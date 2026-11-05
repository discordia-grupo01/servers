package server_moderation_repo_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/discordia-grupo01/servers/internal/repository"
	"github.com/discordia-grupo01/servers/internal/repository/server_moderation_repo"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

var migrations = []string{
	"0001_init", "0002_invitations", "0003_roles", "0004_member_roles", "0005_default_role",
	"0007_categories", "0009_server_banner", "0010_native_categories", "0011_outbox_events",
	"0012_category_name_key", "0013_unassigned_category", "0014_drop_default_kind",
	"0015_role_positions", "0019_everyone_role", "0028_server_moderation", "0031_server_boosts",
}

func newRepo(t *testing.T) (*server_moderation_repo.Repository, *pgxpool.Pool) {
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
		_, _ = pool.Exec(ctx, `DROP TABLE IF EXISTS server_boosts, outbox_events, member_roles, roles, server_bans, invitations, server_members, categories, channels, servers CASCADE`)
	}
	t.Cleanup(drop)
	drop()
	for _, name := range migrations {
		schema, err := os.ReadFile("../../../migrations/" + name + ".up.sql")
		if err != nil {
			t.Fatalf("read migration %s: %v", name, err)
		}
		if _, err := pool.Exec(ctx, string(schema)); err != nil {
			t.Fatalf("apply migration %s: %v", name, err)
		}
	}
	return server_moderation_repo.NewRepository(pool), pool
}

type fixture struct {
	serverID, channelID, categoryID, roleID uuid.UUID
	inviteCode                              string
}

// seedServer inserts a server with one channel, category, role and invitation.
func seedServer(t *testing.T, pool *pgxpool.Pool) fixture {
	t.Helper()
	ctx := context.Background()
	f := fixture{serverID: uuid.New(), channelID: uuid.New(), categoryID: uuid.New(), roleID: uuid.New(), inviteCode: "code-" + uuid.NewString()}
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO servers (id, name, name_key, owner_id) VALUES ($1, 'Mi Comunidad', $2, 'owner-1')`, []any{f.serverID, f.serverID.String()}},
		{`INSERT INTO categories (id, server_id, name, name_key, position) VALUES ($1, $2, 'Texto', 'texto', 0)`, []any{f.categoryID, f.serverID}},
		{`INSERT INTO channels (id, server_id, name, kind, position, category_id) VALUES ($1, $2, 'general', 'text', 0, $3)`, []any{f.channelID, f.serverID, f.categoryID}},
		{`INSERT INTO roles (id, server_id, name, color, permissions, position) VALUES ($1, $2, 'Miembro', '#5865F2', 3, 1)`, []any{f.roleID, f.serverID}},
		{`INSERT INTO invitations (id, code, server_id, created_by) VALUES ($1, $2, $3, 'owner-1')`, []any{uuid.New(), f.inviteCode, f.serverID}},
	} {
		if _, err := pool.Exec(ctx, q.sql, q.args...); err != nil {
			t.Fatalf("seed %q: %v", q.sql, err)
		}
	}
	return f
}

func state(t *testing.T, pool *pgxpool.Pool, serverID uuid.UUID) (suspendedAt *time.Time, version int64) {
	t.Helper()
	err := pool.QueryRow(context.Background(), `SELECT suspended_at, moderation_version FROM servers WHERE id = $1`, serverID).Scan(&suspendedAt, &version)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	return suspendedAt, version
}

func mustApply(t *testing.T, repo *server_moderation_repo.Repository, id uuid.UUID, at *time.Time, version int64, want bool) {
	t.Helper()
	got, err := repo.ApplyServerModeration(context.Background(), id, at, version)
	if err != nil {
		t.Fatalf("ApplyServerModeration(v%d): %v", version, err)
	}
	if got != want {
		t.Errorf("ApplyServerModeration(v%d) applied = %v, want %v", version, got, want)
	}
}

func TestNewServerStartsActiveAtVersionZero(t *testing.T) {
	_, pool := newRepo(t)
	f := seedServer(t, pool)

	at, v := state(t, pool, f.serverID)
	if at != nil || v != 0 {
		t.Errorf("state = (%v, %d), want (nil, 0)", at, v)
	}
}

func TestApplyServerModeration_SuspendsAndReactivates(t *testing.T) {
	repo, pool := newRepo(t)
	f := seedServer(t, pool)
	when := time.Date(2026, 11, 4, 12, 30, 0, 0, time.UTC)

	mustApply(t, repo, f.serverID, &when, 1, true)
	at, v := state(t, pool, f.serverID)
	if at == nil || !at.Equal(when) || v != 1 {
		t.Fatalf("after suspending: state = (%v, %d), want (%v, 1)", at, v, when)
	}

	mustApply(t, repo, f.serverID, nil, 2, true)
	at, v = state(t, pool, f.serverID)
	if at != nil || v != 2 {
		t.Fatalf("after reactivating: state = (%v, %d), want (nil, 2)", at, v)
	}
}

// A redelivered or out-of-order event never changes the state.
func TestApplyServerModeration_StaleOrDuplicateVersionIsIgnored(t *testing.T) {
	repo, pool := newRepo(t)
	f := seedServer(t, pool)
	when := time.Now().UTC().Truncate(time.Millisecond)

	mustApply(t, repo, f.serverID, &when, 3, true)
	mustApply(t, repo, f.serverID, &when, 3, false) // duplicate
	mustApply(t, repo, f.serverID, nil, 2, false)   // older reactivation arriving late
	mustApply(t, repo, f.serverID, nil, 1, false)

	at, v := state(t, pool, f.serverID)
	if at == nil || v != 3 {
		t.Errorf("state = (%v, %d), want still suspended at version 3", at, v)
	}
}

// An event for a server that no longer exists changes nothing and is not an error.
func TestApplyServerModeration_UnknownServerIsANoOp(t *testing.T) {
	repo, _ := newRepo(t)
	when := time.Now().UTC()
	mustApply(t, repo, uuid.New(), &when, 1, false)
}

func TestSuspendedLookups_ResolveTheOwningServer(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	f := seedServer(t, pool)

	lookups := map[string]func() (bool, bool, error){
		"server":   func() (bool, bool, error) { return repo.SuspendedByServer(ctx, f.serverID) },
		"channel":  func() (bool, bool, error) { return repo.SuspendedByChannel(ctx, f.channelID) },
		"category": func() (bool, bool, error) { return repo.SuspendedByCategory(ctx, f.categoryID) },
		"role":     func() (bool, bool, error) { return repo.SuspendedByRole(ctx, f.roleID) },
		"invite":   func() (bool, bool, error) { return repo.SuspendedByInvite(ctx, f.inviteCode) },
	}
	check := func(wantSuspended bool) {
		t.Helper()
		for name, lookup := range lookups {
			suspended, found, err := lookup()
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if !found || suspended != wantSuspended {
				t.Errorf("%s: suspended=%v found=%v, want suspended=%v found=true", name, suspended, found, wantSuspended)
			}
		}
	}

	check(false)
	when := time.Now().UTC()
	mustApply(t, repo, f.serverID, &when, 1, true)
	check(true)
	mustApply(t, repo, f.serverID, nil, 2, true)
	check(false)
}

// Suspending one server doesn't touch another one's channels, roles or invitations.
func TestSuspendedLookups_OtherServersAreUnaffected(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	suspended, other := seedServer(t, pool), seedServer(t, pool)
	when := time.Now().UTC()
	mustApply(t, repo, suspended.serverID, &when, 1, true)

	got, found, err := repo.SuspendedByChannel(ctx, other.channelID)
	if err != nil || !found || got {
		t.Errorf("other server's channel: suspended=%v found=%v err=%v, want false/true/nil", got, found, err)
	}
	got, found, err = repo.SuspendedByInvite(ctx, other.inviteCode)
	if err != nil || !found || got {
		t.Errorf("other server's invite: suspended=%v found=%v err=%v, want false/true/nil", got, found, err)
	}
}

func TestSuspendedLookups_UnknownResourcesAreNotFound(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()

	for name, lookup := range map[string]func() (bool, bool, error){
		"server":   func() (bool, bool, error) { return repo.SuspendedByServer(ctx, uuid.New()) },
		"channel":  func() (bool, bool, error) { return repo.SuspendedByChannel(ctx, uuid.New()) },
		"category": func() (bool, bool, error) { return repo.SuspendedByCategory(ctx, uuid.New()) },
		"role":     func() (bool, bool, error) { return repo.SuspendedByRole(ctx, uuid.New()) },
		"invite":   func() (bool, bool, error) { return repo.SuspendedByInvite(ctx, "no-such-code") },
	} {
		suspended, found, err := lookup()
		if err != nil || found || suspended {
			t.Errorf("%s: suspended=%v found=%v err=%v, want false/false/nil", name, suspended, found, err)
		}
	}
}

func TestSuspendedLookups_DatabaseErrorsAreReturned(t *testing.T) {
	repo, pool := newRepo(t)
	pool.Close()
	if _, _, err := repo.SuspendedByServer(context.Background(), uuid.New()); err == nil {
		t.Error("err = nil, want an error from a closed pool")
	}
	when := time.Now()
	if _, err := repo.ApplyServerModeration(context.Background(), uuid.New(), &when, 1); err == nil {
		t.Error("ApplyServerModeration err = nil, want an error from a closed pool")
	}
}

func TestSuspendedByInvite_AlsoFindsVanityCodes(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	f := seedServer(t, pool)
	if _, err := pool.Exec(ctx, `UPDATE servers SET vanity_code = 'mi-servidor', suspended_at = now() WHERE id = $1`, f.serverID); err != nil {
		t.Fatal(err)
	}

	suspended, found, err := repo.SuspendedByInvite(ctx, "Mi-Servidor")
	if err != nil || !found || !suspended {
		t.Errorf("suspended=%v found=%v err=%v, want true/true/nil", suspended, found, err)
	}
}
