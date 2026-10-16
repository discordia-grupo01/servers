package e2e_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/discordia-grupo01/events-contract/admin"
	"github.com/discordia-grupo01/events-contract/envelope"
	"github.com/discordia-grupo01/servers/internal/events/admin_consumer"
	"github.com/discordia-grupo01/servers/internal/repository"
	"github.com/discordia-grupo01/servers/internal/repository/suspension_repo"
)

const adminSecret = "e2e-admin-secret"

// setupAdminReplica starts the real admin consumer against Postgres and
// RabbitMQ, and returns a channel to publish on discordia.events plus the
// pool. The consumer's queue is declared, bound and purged here first, so
// nothing published by the test can be dropped for lack of a bound queue and
// nothing left over from an earlier run is consumed.
func setupAdminReplica(t *testing.T) (*amqp.Channel, *pgxpool.Pool) {
	t.Helper()
	if os.Getenv("DATABASE_HOST") == "" || os.Getenv("RABBITMQ_URL") == "" {
		t.Skip("DATABASE_HOST and RABBITMQ_URL both required: skipping end-to-end test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	pool, err := repository.NewPool(ctx, "host="+os.Getenv("DATABASE_HOST")+
		" port="+env("DATABASE_PORT", "5432")+
		" dbname="+env("DATABASE_NAME", "discordia_servicio")+
		" user="+env("DATABASE_USER", "discordia")+
		" password="+env("DATABASE_PASSWORD", "discordia")+
		" sslmode=disable")
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	t.Cleanup(pool.Close)
	drop := func() { _, _ = pool.Exec(context.Background(), `DROP TABLE IF EXISTS suspended_users`) }
	t.Cleanup(drop)
	drop()
	schema, err := os.ReadFile("../../migrations/0027_suspended_users.up.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	if _, err := pool.Exec(ctx, string(schema)); err != nil {
		t.Fatalf("apply migration: %v", err)
	}

	conn, err := amqp.Dial(os.Getenv("RABBITMQ_URL"))
	if err != nil {
		t.Fatalf("amqp.Dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	ch, err := conn.Channel()
	if err != nil {
		t.Fatalf("conn.Channel: %v", err)
	}
	if err := ch.ExchangeDeclare("discordia.events", "topic", true, false, false, false, nil); err != nil {
		t.Fatalf("ExchangeDeclare: %v", err)
	}
	if _, err := ch.QueueDeclare(admin_consumer.QueueName, true, false, false, false, nil); err != nil {
		t.Fatalf("QueueDeclare: %v", err)
	}
	for _, key := range []string{admin.RoutingKeyUserSuspended, admin.RoutingKeyUserReactivated} {
		if err := ch.QueueBind(admin_consumer.QueueName, key, "discordia.events", false, nil); err != nil {
			t.Fatalf("QueueBind: %v", err)
		}
	}
	if _, err := ch.QueuePurge(admin_consumer.QueueName, false); err != nil {
		t.Fatalf("QueuePurge: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		_, _ = ch.QueueDelete(admin_consumer.QueueName, false, false, false)
		ch.Close()
	})

	go admin_consumer.New(os.Getenv("RABBITMQ_URL"), adminSecret, suspension_repo.NewSuspensionRepository(pool)).Start(ctx)
	return ch, pool
}

func publishAdmin(t *testing.T, ch *amqp.Channel, routingKey string, event any) {
	t.Helper()
	body, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := ch.PublishWithContext(context.Background(), "discordia.events", routingKey, false, false,
		amqp.Publishing{ContentType: "application/json", DeliveryMode: amqp.Persistent, Body: body},
	); err != nil {
		t.Fatalf("publish: %v", err)
	}
}

func suspend(secret, user string, version int64) admin.UserSuspended {
	e := admin.UserSuspended{Meta: envelope.Meta{EventID: uuid.NewString(), OccurredAt: time.Now().UTC()}, UserID: user, Version: version}
	e.Sign(secret)
	return e
}

func reactivate(secret, user string, version int64) admin.UserReactivated {
	e := admin.UserReactivated{Meta: envelope.Meta{EventID: uuid.NewString(), OccurredAt: time.Now().UTC()}, UserID: user, Version: version}
	e.Sign(secret)
	return e
}

// waitForSuspension waits until user's replicated state is (suspended, version).
func waitForSuspension(t *testing.T, pool *pgxpool.Pool, user string, suspended bool, version int64) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var gotSuspended bool
		var gotVersion int64
		err := pool.QueryRow(context.Background(), `SELECT suspended, version FROM suspended_users WHERE user_id = $1`, user).Scan(&gotSuspended, &gotVersion)
		if err == nil && gotSuspended == suspended && gotVersion == version {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s to be suspended=%v at version %d", user, suspended, version)
}

func hasSuspensionRow(t *testing.T, pool *pgxpool.Pool, user string) bool {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM suspended_users WHERE user_id = $1`, user).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n > 0
}

func TestAdminEvents_SuspendAndReactivateAreReplicated(t *testing.T) {
	ch, pool := setupAdminReplica(t)
	user := uuid.NewString() // servers has never seen this user: the row is created anyway

	publishAdmin(t, ch, admin.RoutingKeyUserSuspended, suspend(adminSecret, user, 1))
	waitForSuspension(t, pool, user, true, 1)

	publishAdmin(t, ch, admin.RoutingKeyUserReactivated, reactivate(adminSecret, user, 2))
	waitForSuspension(t, pool, user, false, 2)

	// A late duplicate of the suspension (older version) doesn't re-suspend.
	// The probe event published after it is handled in order by the same
	// consumer, so once the probe lands the stale one has been processed.
	probe := uuid.NewString()
	publishAdmin(t, ch, admin.RoutingKeyUserSuspended, suspend(adminSecret, user, 1))
	publishAdmin(t, ch, admin.RoutingKeyUserSuspended, suspend(adminSecret, probe, 1))
	waitForSuspension(t, pool, probe, true, 1)
	waitForSuspension(t, pool, user, false, 2)
}

// Events with a missing, forged or foreign-secret signature, or published under
// another routing key than the one signed, change nothing. The valid event at
// the end is processed after them (same queue, same consumer), which proves the
// invalid ones were already handled and rejected.
func TestAdminEvents_UnverifiableEventsChangeNothing(t *testing.T) {
	ch, pool := setupAdminReplica(t)
	victim := uuid.NewString()
	probe := uuid.NewString()

	unsigned := suspend(adminSecret, victim, 1)
	unsigned.Signature = ""
	publishAdmin(t, ch, admin.RoutingKeyUserSuspended, unsigned)
	publishAdmin(t, ch, admin.RoutingKeyUserSuspended, suspend("not-the-shared-secret", victim, 2))
	forged := suspend(adminSecret, victim, 3)
	forged.Signature = "00" + forged.Signature[2:]
	publishAdmin(t, ch, admin.RoutingKeyUserSuspended, forged)
	// A valid reactivation signature can't pass as a suspension and vice versa.
	r := reactivate(adminSecret, victim, 4)
	publishAdmin(t, ch, admin.RoutingKeyUserSuspended, admin.UserSuspended{Meta: r.Meta, UserID: r.UserID, Version: r.Version, Signature: r.Signature})
	publishAdmin(t, ch, admin.RoutingKeyUserSuspended, suspend(adminSecret, probe, 1))

	waitForSuspension(t, pool, probe, true, 1)
	if hasSuspensionRow(t, pool, victim) {
		t.Errorf("victim has a suspended_users row after only unverifiable events")
	}
}
