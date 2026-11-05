package boost_repo_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/discordia-grupo01/events-contract/boosts"
	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/repository"
	"github.com/discordia-grupo01/servers/internal/repository/boost_repo"
)

const (
	secret = "test-secret"
	member = "member-1"
)

type outboxRow struct {
	routingKey string
	payload    []byte
}

func setup(t *testing.T) (*boost_repo.Repository, *pgxpool.Pool) {
	t.Helper()
	host := os.Getenv("DATABASE_HOST")
	if host == "" {
		t.Skip("DATABASE_HOST not set: skipping integration test")
	}
	dsn := "host=" + host +
		" port=" + env("DATABASE_PORT", "5432") +
		" dbname=" + env("DATABASE_NAME", "discordia_servicio") +
		" user=" + env("DATABASE_USER", "discordia") +
		" password=" + env("DATABASE_PASSWORD", "discordia") +
		" sslmode=disable"

	ctx := context.Background()
	pool, err := repository.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	t.Cleanup(pool.Close)
	drop := func() {
		_, _ = pool.Exec(ctx, `DROP TABLE IF EXISTS server_boosts, outbox_events, invitations, server_bans, server_members, channels, servers CASCADE`)
	}
	t.Cleanup(drop)
	drop()
	for _, file := range []string{
		"0001_init.up.sql", "0002_invitations.up.sql", "0011_outbox_events.up.sql",
		"0028_server_moderation.up.sql", "0031_server_boosts.up.sql",
	} {
		schema, err := os.ReadFile("../../../migrations/" + file)
		if err != nil {
			t.Fatalf("read migration %s: %v", file, err)
		}
		if _, err := pool.Exec(ctx, string(schema)); err != nil {
			t.Fatalf("apply migration %s: %v", file, err)
		}
	}
	return boost_repo.NewRepository(pool, secret), pool
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func seedServer(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	id := uuid.New()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO servers (id, name, name_key, owner_id) VALUES ($1, 'Server', $2, 'owner-1')`, id, id.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO server_members (server_id, user_id) VALUES ($1, $2)`, id, member); err != nil {
		t.Fatal(err)
	}
	return id
}

func newBoost(serverID uuid.UUID) model.Boost {
	return model.Boost{ID: uuid.New(), ServerID: serverID, UserID: member}
}

func outboxRows(t *testing.T, pool *pgxpool.Pool) []outboxRow {
	t.Helper()
	rows, err := pool.Query(context.Background(), `SELECT routing_key, payload FROM outbox_events ORDER BY created_at, routing_key`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []outboxRow
	for rows.Next() {
		var row outboxRow
		if err := rows.Scan(&row.routingKey, &row.payload); err != nil {
			t.Fatal(err)
		}
		out = append(out, row)
	}
	return out
}

func clearOutbox(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `DELETE FROM outbox_events`); err != nil {
		t.Fatal(err)
	}
}

// assertAnswer checks the only saga event in the outbox, including that billing
// would accept its signature.
func assertAnswer(t *testing.T, pool *pgxpool.Pool, routingKey, reason string, b model.Boost) {
	t.Helper()
	var found int
	for _, row := range outboxRows(t, pool) {
		if row.routingKey == boosts.RoutingKeyUpdated {
			continue
		}
		found++
		if row.routingKey != routingKey {
			t.Errorf("routing key = %s, want %s", row.routingKey, routingKey)
		}
		var evt boosts.SagaEvent
		if err := json.Unmarshal(row.payload, &evt); err != nil {
			t.Fatal(err)
		}
		if !evt.Verify(routingKey, secret) {
			t.Error("billing wouldn't accept this signature")
		}
		if evt.Reason != reason || evt.BoostID != b.ID.String() || evt.ServerID != b.ServerID.String() || evt.UserID != b.UserID {
			t.Errorf("event = %+v, want reason %q for boost %+v", evt, reason, b)
		}
	}
	if found != 1 {
		t.Errorf("%d answers in the outbox, want 1", found)
	}
}

func assertBoosts(t *testing.T, pool *pgxpool.Pool, serverID uuid.UUID, wantCount, wantLevel int) {
	t.Helper()
	var count, level int
	if err := pool.QueryRow(context.Background(),
		`SELECT boost_count, boost_level FROM servers WHERE id = $1`, serverID,
	).Scan(&count, &level); err != nil {
		t.Fatal(err)
	}
	if count != wantCount || level != wantLevel {
		t.Errorf("count=%d level=%d, want %d and %d", count, level, wantCount, wantLevel)
	}
}

func boostStatus(t *testing.T, pool *pgxpool.Pool, id uuid.UUID) string {
	t.Helper()
	var status string
	if err := pool.QueryRow(context.Background(), `SELECT status FROM server_boosts WHERE boost_id = $1`, id).Scan(&status); err != nil {
		t.Fatal(err)
	}
	return status
}

func TestRequest_MemberIsAppliedAndCounted(t *testing.T) {
	repo, pool := setup(t)
	ctx := context.Background()
	b := newBoost(seedServer(t, pool))

	if err := repo.Request(ctx, b); err != nil {
		t.Fatal(err)
	}

	assertAnswer(t, pool, boosts.RoutingKeyApplied, "", b)
	assertBoosts(t, pool, b.ServerID, 1, 0)
	var updated boosts.ServerBoostUpdated
	for _, row := range outboxRows(t, pool) {
		if row.routingKey == boosts.RoutingKeyUpdated {
			if err := json.Unmarshal(row.payload, &updated); err != nil {
				t.Fatal(err)
			}
		}
	}
	if updated.BoostCount != 1 || updated.Level != 0 || updated.Version != 1 || updated.ServerID != b.ServerID.String() {
		t.Errorf("server_boost_updated = %+v", updated)
	}
}

func TestRequest_RepeatedAnswersTheSameAndCountsOnce(t *testing.T) {
	repo, pool := setup(t)
	ctx := context.Background()
	b := newBoost(seedServer(t, pool))

	for i := 0; i < 3; i++ {
		if err := repo.Request(ctx, b); err != nil {
			t.Fatal(err)
		}
	}

	assertBoosts(t, pool, b.ServerID, 1, 0)
	var applied, updated int
	for _, row := range outboxRows(t, pool) {
		switch row.routingKey {
		case boosts.RoutingKeyApplied:
			applied++
		case boosts.RoutingKeyUpdated:
			updated++
		}
	}
	if applied != 3 || updated != 1 {
		t.Errorf("applied=%d updated=%d, want 3 and 1", applied, updated)
	}
}

func TestRequest_Rejections(t *testing.T) {
	repo, pool := setup(t)
	ctx := context.Background()

	t.Run("server not found", func(t *testing.T) {
		clearOutbox(t, pool)
		b := newBoost(uuid.New())
		if err := repo.Request(ctx, b); err != nil {
			t.Fatal(err)
		}
		assertAnswer(t, pool, boosts.RoutingKeyRejected, boosts.ReasonServerNotFound, b)
	})

	t.Run("not a member", func(t *testing.T) {
		clearOutbox(t, pool)
		b := newBoost(seedServer(t, pool))
		b.UserID = "stranger"
		if err := repo.Request(ctx, b); err != nil {
			t.Fatal(err)
		}
		assertAnswer(t, pool, boosts.RoutingKeyRejected, boosts.ReasonNotAMember, b)
		assertBoosts(t, pool, b.ServerID, 0, 0)
	})

	t.Run("suspended, and the answer doesn't change after reactivating", func(t *testing.T) {
		clearOutbox(t, pool)
		b := newBoost(seedServer(t, pool))
		if _, err := pool.Exec(ctx, `UPDATE servers SET suspended_at = now() WHERE id = $1`, b.ServerID); err != nil {
			t.Fatal(err)
		}
		if err := repo.Request(ctx, b); err != nil {
			t.Fatal(err)
		}
		assertAnswer(t, pool, boosts.RoutingKeyRejected, boosts.ReasonServerSuspended, b)

		clearOutbox(t, pool)
		if _, err := pool.Exec(ctx, `UPDATE servers SET suspended_at = NULL WHERE id = $1`, b.ServerID); err != nil {
			t.Fatal(err)
		}
		if err := repo.Request(ctx, b); err != nil {
			t.Fatal(err)
		}
		assertAnswer(t, pool, boosts.RoutingKeyRejected, boosts.ReasonServerSuspended, b)
		assertBoosts(t, pool, b.ServerID, 0, 0)
	})
}

func TestCancel_BeforeTheRequestRejectsItWhenItArrives(t *testing.T) {
	repo, pool := setup(t)
	ctx := context.Background()
	b := newBoost(seedServer(t, pool))

	if err := repo.Cancel(ctx, b); err != nil {
		t.Fatal(err)
	}
	if rows := outboxRows(t, pool); len(rows) != 0 {
		t.Errorf("cancelling published %d events", len(rows))
	}
	if err := repo.Request(ctx, b); err != nil {
		t.Fatal(err)
	}

	assertAnswer(t, pool, boosts.RoutingKeyRejected, boosts.ReasonCancelled, b)
	assertBoosts(t, pool, b.ServerID, 0, 0)
}

func TestCancel_AfterApplyingStopsCounting(t *testing.T) {
	repo, pool := setup(t)
	ctx := context.Background()
	b := newBoost(seedServer(t, pool))
	if err := repo.Request(ctx, b); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 2; i++ {
		if err := repo.Cancel(ctx, b); err != nil {
			t.Fatal(err)
		}
	}

	if got := boostStatus(t, pool, b.ID); got != model.BoostStatusCancelled {
		t.Errorf("status = %s", got)
	}
	assertBoosts(t, pool, b.ServerID, 0, 0)
	var version int64
	if err := pool.QueryRow(ctx, `SELECT boost_version FROM servers WHERE id = $1`, b.ServerID).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 2 {
		t.Errorf("version = %d: a repeated cancel must not count as a change", version)
	}
}

func TestCancel_DoesNotChangeARejection(t *testing.T) {
	repo, pool := setup(t)
	ctx := context.Background()
	b := newBoost(seedServer(t, pool))
	b.UserID = "stranger"
	if err := repo.Request(ctx, b); err != nil {
		t.Fatal(err)
	}
	if err := repo.Cancel(ctx, b); err != nil {
		t.Fatal(err)
	}
	if got := boostStatus(t, pool, b.ID); got != model.BoostStatusRejected {
		t.Errorf("status = %s", got)
	}
}

func TestCancelAndRemove_ServerGoneIsNotAnError(t *testing.T) {
	repo, _ := setup(t)
	b := newBoost(uuid.New())
	if err := repo.Cancel(context.Background(), b); err != nil {
		t.Errorf("cancel: %v", err)
	}
	if err := repo.Remove(context.Background(), b); err != nil {
		t.Errorf("remove: %v", err)
	}
}

func TestRemove_DropsALevelAndARepeatChangesNothing(t *testing.T) {
	repo, pool := setup(t)
	ctx := context.Background()
	serverID := seedServer(t, pool)
	var last model.Boost
	for i := 0; i < 5; i++ {
		last = newBoost(serverID)
		if err := repo.Request(ctx, last); err != nil {
			t.Fatal(err)
		}
	}
	assertBoosts(t, pool, serverID, 5, 1)

	for i := 0; i < 2; i++ {
		if err := repo.Remove(ctx, last); err != nil {
			t.Fatal(err)
		}
	}

	if got := boostStatus(t, pool, last.ID); got != model.BoostStatusRemoved {
		t.Errorf("status = %s", got)
	}
	assertBoosts(t, pool, serverID, 4, 0)

	clearOutbox(t, pool)
	if err := repo.Request(ctx, last); err != nil {
		t.Fatal(err)
	}
	if rows := outboxRows(t, pool); len(rows) != 0 {
		t.Errorf("a late request for a removed boost published %d events", len(rows))
	}
}

func TestRequest_ConcurrentBoostsAllCount(t *testing.T) {
	repo, pool := setup(t)
	ctx := context.Background()
	serverID := seedServer(t, pool)

	var wg sync.WaitGroup
	errs := make(chan error, 15)
	for i := 0; i < 15; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- repo.Request(ctx, newBoost(serverID))
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	assertBoosts(t, pool, serverID, 15, 2)
}

func TestLeavingKeepsTheBoost(t *testing.T) {
	repo, pool := setup(t)
	ctx := context.Background()
	b := newBoost(seedServer(t, pool))
	if err := repo.Request(ctx, b); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM server_members WHERE server_id = $1 AND user_id = $2`, b.ServerID, b.UserID); err != nil {
		t.Fatal(err)
	}
	if err := repo.Remove(ctx, b); err != nil {
		t.Fatal(err)
	}
	assertBoosts(t, pool, b.ServerID, 0, 0)
}

func TestDeletingTheServerDeletesItsBoosts(t *testing.T) {
	repo, pool := setup(t)
	ctx := context.Background()
	b := newBoost(seedServer(t, pool))
	if err := repo.Request(ctx, b); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM servers WHERE id = $1`, b.ServerID); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM server_boosts`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("%d boosts left", count)
	}
}

func TestSetVanity(t *testing.T) {
	repo, pool := setup(t)
	ctx := context.Background()
	first, second := seedServer(t, pool), seedServer(t, pool)
	code := "mi-servidor"

	if err := repo.SetVanity(ctx, first, &code); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetVanity(ctx, second, &code); !errors.Is(err, model.ErrVanityCodeTaken) {
		t.Errorf("taken code: err = %v", err)
	}
	if err := repo.SetVanity(ctx, first, nil); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetVanity(ctx, second, &code); err != nil {
		t.Errorf("a cleared code must be free again: %v", err)
	}
	other := "otro-codigo"
	if err := repo.SetVanity(ctx, uuid.New(), &other); !errors.Is(err, model.ErrServerNotFound) {
		t.Errorf("missing server: err = %v", err)
	}
}

func TestSetVanity_CantMatchAnInvitation(t *testing.T) {
	repo, pool := setup(t)
	ctx := context.Background()
	serverID := seedServer(t, pool)
	code := "abcd1234"
	if _, err := pool.Exec(ctx, `
		INSERT INTO invitations (id, code, server_id, created_by, created_at)
		VALUES ($1, $2, $3, 'owner-1', $4)`, uuid.New(), code, serverID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetVanity(ctx, serverID, &code); !errors.Is(err, model.ErrVanityCodeTaken) {
		t.Errorf("err = %v", err)
	}
}
