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

	"github.com/discordia-grupo01/events-contract/boosts"
	"github.com/discordia-grupo01/events-contract/broker"
	"github.com/discordia-grupo01/events-contract/envelope"
	"github.com/discordia-grupo01/servers/internal/events"
	"github.com/discordia-grupo01/servers/internal/events/billing_consumer"
	"github.com/discordia-grupo01/servers/internal/outbox"
	"github.com/discordia-grupo01/servers/internal/repository"
	"github.com/discordia-grupo01/servers/internal/repository/boost_repo"
)

const billingSecret = "billing-e2e-secret"

// TestBoostSaga runs servers' whole side against a real RabbitMQ: billing's signed
// request goes through the queue and the consumer, and the signed answer comes
// back through the outbox.
func TestBoostSaga_RequestIsAppliedAndAnswered(t *testing.T) {
	rmqURL := os.Getenv("RABBITMQ_URL")
	if os.Getenv("DATABASE_HOST") == "" || rmqURL == "" {
		t.Skip("DATABASE_HOST or RABBITMQ_URL not set: skipping e2e test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	dsn := "host=" + os.Getenv("DATABASE_HOST") +
		" port=" + getenvDefault("DATABASE_PORT", "5432") +
		" dbname=" + getenvDefault("DATABASE_NAME", "discordia_servicio") +
		" user=" + getenvDefault("DATABASE_USER", "discordia") +
		" password=" + getenvDefault("DATABASE_PASSWORD", "discordia") +
		" sslmode=disable"
	pool, err := repository.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	t.Cleanup(pool.Close)
	drop := func() {
		_, _ = pool.Exec(context.Background(), `DROP TABLE IF EXISTS moderation_audit_entries, server_boosts, outbox_events, invitations, server_bans, server_members, channels, servers CASCADE`)
	}
	t.Cleanup(drop)
	drop()
	for _, f := range []string{
		"0001_init.up.sql", "0002_invitations.up.sql", "0011_outbox_events.up.sql",
		"0028_server_moderation.up.sql", "0031_server_boosts.up.sql",
	} {
		schema, err := os.ReadFile("../../migrations/" + f)
		if err != nil {
			t.Fatalf("read migration %s: %v", f, err)
		}
		if _, err := pool.Exec(ctx, string(schema)); err != nil {
			t.Fatalf("apply migration %s: %v", f, err)
		}
	}
	serverID := seedBoostServer(t, pool)

	conn, err := amqp.Dial(rmqURL)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	ch, err := conn.Channel()
	if err != nil {
		t.Fatalf("channel: %v", err)
	}
	if err := ch.ExchangeDeclare(broker.ExchangeName, broker.ExchangeType, true, false, false, false, nil); err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if _, err := ch.QueueDeclare(billing_consumer.QueueName, true, false, false, false, nil); err != nil {
		t.Fatalf("declare: %v", err)
	}
	if err := ch.QueueBind(billing_consumer.QueueName, boosts.RoutingKeyRequested, broker.ExchangeName, false, nil); err != nil {
		t.Fatalf("bind: %v", err)
	}
	if _, err := ch.QueuePurge(billing_consumer.QueueName, false); err != nil {
		t.Fatalf("purge: %v", err)
	}
	answers, err := ch.QueueDeclare("", false, true, true, false, nil)
	if err != nil {
		t.Fatalf("declare answers: %v", err)
	}
	for _, key := range []string{boosts.RoutingKeyApplied, boosts.RoutingKeyUpdated} {
		if err := ch.QueueBind(answers.Name, key, broker.ExchangeName, false, nil); err != nil {
			t.Fatalf("bind: %v", err)
		}
	}
	deliveries, err := ch.Consume(answers.Name, "", true, true, false, false, nil)
	if err != nil {
		t.Fatalf("consume: %v", err)
	}

	go billing_consumer.New(rmqURL, billingSecret, boost_repo.NewRepository(pool, billingSecret)).Start(ctx)
	go outbox.NewPoller(pool, events.NewAMQPPublisher(rmqURL)).Start(ctx)

	request := boosts.SagaEvent{
		Meta:     envelope.Meta{EventID: uuid.NewString(), OccurredAt: time.Now()},
		BoostID:  uuid.NewString(),
		ServerID: serverID.String(),
		UserID:   "owner-1",
	}
	request.Sign(boosts.RoutingKeyRequested, billingSecret)
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := ch.PublishWithContext(ctx, broker.ExchangeName, boosts.RoutingKeyRequested, false, false, amqp.Publishing{
		ContentType: "application/json", Body: body,
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	var applied boosts.SagaEvent
	var updated boosts.ServerBoostUpdated
	timeout := time.After(20 * time.Second)
	for got := 0; got < 2; got++ {
		select {
		case d := <-deliveries:
			var target any = &applied
			if d.RoutingKey == boosts.RoutingKeyUpdated {
				target = &updated
			}
			if err := json.Unmarshal(d.Body, target); err != nil {
				t.Fatal(err)
			}
		case <-timeout:
			t.Fatalf("timed out after %d answers", got)
		}
	}
	if applied.BoostID != request.BoostID || !applied.Verify(boosts.RoutingKeyApplied, billingSecret) {
		t.Errorf("applied = %+v, or billing wouldn't accept its signature", applied)
	}
	if updated.ServerID != serverID.String() || updated.BoostCount != 1 || updated.Version != 1 {
		t.Errorf("updated = %+v", updated)
	}
}

func seedBoostServer(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	id := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO servers (id, name, name_key, owner_id) VALUES ($1, 'Server', $2, 'owner-1')`, id, id.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO server_members (server_id, user_id) VALUES ($1, 'owner-1')`, id); err != nil {
		t.Fatal(err)
	}
	return id
}

func getenvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
