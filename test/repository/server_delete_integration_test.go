package repository_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/outbox"
	"github.com/discordia-grupo01/servers/internal/repository"
)

func applyDeletionSchema(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	_, _ = pool.Exec(ctx, `DROP TABLE IF EXISTS ownership_transfers CASCADE`)
	for _, path := range []string{
		"../../migrations/0002_invitations.up.sql",
		"../../migrations/0006_ownership_transfers.up.sql",
	} {
		schema, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read migration %s: %v", path, err)
		}
		if _, err := pool.Exec(ctx, string(schema)); err != nil {
			t.Fatalf("apply schema %s: %v", path, err)
		}
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DROP TABLE IF EXISTS ownership_transfers CASCADE`) })
}

type seeded struct {
	srv   model.Server
	chans []model.Channel
	owner string
	guest string
}

func seedFullServer(t *testing.T, repo *repository.ServerRepository, pool *pgxpool.Pool, name string) seeded {
	t.Helper()
	ctx := context.Background()
	owner, guest := "owner-"+uuid.NewString(), "guest-"+uuid.NewString()
	srv, chans := serverFixture(name, owner)
	if err := repo.CreateServer(ctx, srv, nil, chans, []outbox.Event{testOutboxEvent()}); err != nil {
		t.Fatalf("CreateServer: %v", err)
	}

	roleID := uuid.New()
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO server_members (server_id, user_id, is_owner, joined_at) VALUES ($1, $2, FALSE, now())`, []any{srv.ID, guest}},
		{`INSERT INTO roles (id, server_id, name, color, permissions, position) VALUES ($1, $2, 'Miembro', '#5865F2', 3, 1)`, []any{roleID, srv.ID}},
		{`INSERT INTO member_roles (server_id, user_id, role_id) VALUES ($1, $2, $3)`, []any{srv.ID, guest, roleID}},
		{`UPDATE servers SET default_role_id = $2 WHERE id = $1`, []any{srv.ID, roleID}},
		{`INSERT INTO invitations (id, code, server_id, created_by) VALUES ($1, $2, $3, $4)`, []any{uuid.New(), "code-" + uuid.NewString(), srv.ID, owner}},
		{`INSERT INTO server_bans (server_id, user_id, reason) VALUES ($1, $2, 'spam')`, []any{srv.ID, "banned-" + uuid.NewString()}},
		{`INSERT INTO ownership_transfers (id, server_id, from_user_id, to_user_id) VALUES ($1, $2, $3, $4)`, []any{uuid.New(), srv.ID, owner, guest}},
	} {
		if _, err := pool.Exec(ctx, q.sql, q.args...); err != nil {
			t.Fatalf("seed %q: %v", q.sql, err)
		}
	}
	return seeded{srv: srv, chans: chans, owner: owner, guest: guest}
}

var serverScopedTables = []string{
	"channels", "server_members", "roles", "member_roles", "invitations", "server_bans", "ownership_transfers",
}

func countFor(t *testing.T, pool *pgxpool.Pool, table string, serverID uuid.UUID) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM `+table+` WHERE server_id = $1`, serverID).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func eventBuilder(routingKey string) (func([]string, []uuid.UUID) ([]outbox.Event, error), *struct {
	members  []string
	channels []uuid.UUID
}) {
	seen := &struct {
		members  []string
		channels []uuid.UUID
	}{}
	return func(members []string, channels []uuid.UUID) ([]outbox.Event, error) {
		seen.members, seen.channels = members, channels
		return []outbox.Event{{RoutingKey: routingKey, Payload: []byte(`{"ok":true}`)}}, nil
	}, seen
}

func TestRepo_DeleteServer_RemovesEverythingOfThatServerAndNothingElse(t *testing.T) {
	repo, pool := newRepo(t)
	applyDeletionSchema(t, pool)
	ctx := context.Background()
	doomed := seedFullServer(t, repo, pool, "Doomed")
	survivor := seedFullServer(t, repo, pool, "Survivor")

	for _, table := range serverScopedTables {
		if countFor(t, pool, table, doomed.srv.ID) == 0 {
			t.Fatalf("fixture: %s has no rows for the server to delete", table)
		}
	}
	before := map[string]int{}
	for _, table := range serverScopedTables {
		before[table] = countFor(t, pool, table, survivor.srv.ID)
	}

	build, _ := eventBuilder("test.deleted")
	if err := repo.DeleteServer(ctx, doomed.srv.ID, build); err != nil {
		t.Fatalf("DeleteServer: %v", err)
	}

	if _, err := repo.GetByID(ctx, doomed.srv.ID); !errors.Is(err, model.ErrServerNotFound) {
		t.Errorf("GetByID after delete: err = %v, want ErrServerNotFound", err)
	}
	for _, table := range serverScopedTables {
		if n := countFor(t, pool, table, doomed.srv.ID); n != 0 {
			t.Errorf("%s still has %d rows of the deleted server", table, n)
		}
		if n := countFor(t, pool, table, survivor.srv.ID); n != before[table] {
			t.Errorf("%s rows of the other server: %d, want %d untouched", table, n, before[table])
		}
	}
	if _, err := repo.GetByID(ctx, survivor.srv.ID); err != nil {
		t.Errorf("the other server must survive: %v", err)
	}
}

func TestRepo_DeleteServer_HandsTheEventBuilderEveryMemberAndChannel(t *testing.T) {
	repo, pool := newRepo(t)
	applyDeletionSchema(t, pool)
	s := seedFullServer(t, repo, pool, "Builder Input")

	build, seen := eventBuilder("test.deleted")
	if err := repo.DeleteServer(context.Background(), s.srv.ID, build); err != nil {
		t.Fatalf("DeleteServer: %v", err)
	}

	members := map[string]bool{}
	for _, m := range seen.members {
		members[m] = true
	}
	if len(seen.members) != 2 || !members[s.owner] || !members[s.guest] {
		t.Errorf("members = %v, want owner and guest", seen.members)
	}
	channels := map[uuid.UUID]bool{}
	for _, c := range seen.channels {
		channels[c] = true
	}
	if len(seen.channels) != len(s.chans) {
		t.Fatalf("channels = %d, want %d", len(seen.channels), len(s.chans))
	}
	for _, c := range s.chans {
		if !channels[c.ID] {
			t.Errorf("channel %s missing from the builder input", c.ID)
		}
	}
}

func TestRepo_DeleteServer_StoresTheEventsInTheSameTransaction(t *testing.T) {
	repo, pool := newRepo(t)
	applyDeletionSchema(t, pool)
	s := seedFullServer(t, repo, pool, "Outbox Atomic")
	key := "test.deleted." + uuid.NewString()

	build, _ := eventBuilder(key)
	if err := repo.DeleteServer(context.Background(), s.srv.ID, build); err != nil {
		t.Fatalf("DeleteServer: %v", err)
	}

	var n int
	_ = pool.QueryRow(context.Background(), `SELECT count(*) FROM outbox_events WHERE routing_key = $1 AND published_at IS NULL`, key).Scan(&n)
	if n != 1 {
		t.Errorf("pending outbox events = %d, want 1", n)
	}
}

func TestRepo_DeleteServer_WhenBuildingEventsFails_KeepsTheServer(t *testing.T) {
	repo, pool := newRepo(t)
	applyDeletionSchema(t, pool)
	ctx := context.Background()
	s := seedFullServer(t, repo, pool, "Rollback")
	boom := errors.New("cannot marshal")

	err := repo.DeleteServer(ctx, s.srv.ID, func([]string, []uuid.UUID) ([]outbox.Event, error) { return nil, boom })
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the builder error", err)
	}
	if _, err := repo.GetByID(ctx, s.srv.ID); err != nil {
		t.Errorf("the server must still exist after a failed deletion: %v", err)
	}
	for _, table := range serverScopedTables {
		if countFor(t, pool, table, s.srv.ID) == 0 {
			t.Errorf("%s lost its rows although the deletion failed", table)
		}
	}
}

func TestRepo_DeleteServer_WhenEventInsertFails_KeepsTheServer(t *testing.T) {
	repo, pool := newRepo(t)
	applyDeletionSchema(t, pool)
	ctx := context.Background()
	s := seedFullServer(t, repo, pool, "Rollback Insert")

	err := repo.DeleteServer(ctx, s.srv.ID, func([]string, []uuid.UUID) ([]outbox.Event, error) {
		return []outbox.Event{{RoutingKey: "test.bad", Payload: []byte("not json")}}, nil
	})
	if err == nil {
		t.Fatal("DeleteServer succeeded although the outbox payload is invalid")
	}
	if _, err := repo.GetByID(ctx, s.srv.ID); err != nil {
		t.Errorf("the server must survive when its events cannot be stored: %v", err)
	}
}

func TestRepo_DeleteServer_UnknownServer(t *testing.T) {
	repo, pool := newRepo(t)
	applyDeletionSchema(t, pool)
	key := "test.deleted." + uuid.NewString()

	build, _ := eventBuilder(key)
	err := repo.DeleteServer(context.Background(), uuid.New(), build)
	if !errors.Is(err, model.ErrServerNotFound) {
		t.Fatalf("err = %v, want ErrServerNotFound", err)
	}
	var n int
	_ = pool.QueryRow(context.Background(), `SELECT count(*) FROM outbox_events WHERE routing_key = $1`, key).Scan(&n)
	if n != 0 {
		t.Errorf("outbox events = %d, want none for a server that does not exist", n)
	}
}

func TestRepo_DeleteServer_IsSafeUnderConcurrentDeletes(t *testing.T) {
	repo, pool := newRepo(t)
	applyDeletionSchema(t, pool)
	s := seedFullServer(t, repo, pool, "Race")
	key := "test.deleted." + uuid.NewString()

	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			build, _ := eventBuilder(key)
			results <- repo.DeleteServer(context.Background(), s.srv.ID, build)
		}()
	}
	var ok, notFound int
	deadline := time.After(15 * time.Second)
	for i := 0; i < 2; i++ {
		select {
		case err := <-results:
			switch {
			case err == nil:
				ok++
			case errors.Is(err, model.ErrServerNotFound):
				notFound++
			default:
				t.Fatalf("unexpected error: %v", err)
			}
		case <-deadline:
			t.Fatal("concurrent deletes did not finish")
		}
	}
	if ok != 1 || notFound != 1 {
		t.Errorf("ok=%d notFound=%d, want exactly one winner and one ErrServerNotFound", ok, notFound)
	}
	var n int
	_ = pool.QueryRow(context.Background(), `SELECT count(*) FROM outbox_events WHERE routing_key = $1`, key).Scan(&n)
	if n != 1 {
		t.Errorf("outbox events = %d, want exactly 1 (no duplicate server_deleted)", n)
	}
}
