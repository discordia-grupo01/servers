package channelperm_repo_test

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/repository"
)

func newPool(t *testing.T) *pgxpool.Pool {
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
	t.Cleanup(func() { dropAllTestTables(ctx, pool) })

	dropAllTestTables(ctx, pool)
	for _, file := range []string{
		"../../../migrations/0001_init.up.sql",
		"../../../migrations/0003_roles.up.sql",
		"../../../migrations/0004_member_roles.up.sql",
		"../../../migrations/0007_categories.up.sql",
		"../../../migrations/0015_role_positions.up.sql",
		"../../../migrations/0019_everyone_role.up.sql",
		"../../../migrations/0020_channel_permission_overrides.up.sql",
	} {
		schema, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read migration %s: %v", file, err)
		}
		if _, err := pool.Exec(ctx, string(schema)); err != nil {
			t.Fatalf("apply migration %s: %v", file, err)
		}
	}
	return pool
}

func dropAllTestTables(ctx context.Context, pool *pgxpool.Pool) {
	_, _ = pool.Exec(ctx, `DROP TABLE IF EXISTS channel_permission_overrides, member_roles, roles, categories, server_members, channels, servers CASCADE`)
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func exec(ctx context.Context, t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

func count(ctx context.Context, t *testing.T, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return n
}

// seed creates a server with one channel and one role.
func seed(ctx context.Context, t *testing.T, pool *pgxpool.Pool) (channelID, roleID uuid.UUID) {
	t.Helper()
	serverID, channelID, roleID := uuid.New(), uuid.New(), uuid.New()
	exec(ctx, t, pool, `INSERT INTO servers (id, name, name_key, owner_id) VALUES ($1, 'Server', $2, 'owner-1')`, serverID, "s-"+serverID.String())
	exec(ctx, t, pool, `INSERT INTO channels (id, server_id, name, kind) VALUES ($1, $2, 'general', 'text')`, channelID, serverID)
	exec(ctx, t, pool, `INSERT INTO roles (id, server_id, name, color, position) VALUES ($1, $2, 'R', '#000000', 1)`, roleID, serverID)
	return channelID, roleID
}

const insertOverride = `INSERT INTO channel_permission_overrides (channel_id, role_id, allow, deny) VALUES ($1, $2, $3, $4)`

// Migration 0020 accepts any valid override: disjoint masks with channel-scoped
// permissions only.
func TestMigration0020_AcceptsAValidOverride(t *testing.T) {
	pool := newPool(t)
	ctx := context.Background()
	channelID, roleID := seed(ctx, t, pool)

	exec(ctx, t, pool, insertOverride, channelID, roleID, role.PermSendMessages, role.PermViewChannels|role.PermManageMessages)
	if n := count(ctx, t, pool, `SELECT COUNT(*) FROM channel_permission_overrides`); n != 1 {
		t.Fatalf("got %d overrides, want 1", n)
	}
}

// The CHECKs reject what the service never writes: overlapping masks, empty
// overrides and server-level permissions.
func TestMigration0020_RejectsInvalidOverrides(t *testing.T) {
	pool := newPool(t)
	ctx := context.Background()
	channelID, roleID := seed(ctx, t, pool)

	cases := map[string][2]role.Permission{
		"allow and deny overlap":   {role.PermSendMessages, role.PermSendMessages},
		"empty override":           {0, 0},
		"server-level permission":  {role.PermAdministrator, 0},
		"server-level in the deny": {0, role.PermKickMembers},
	}
	for name, masks := range cases {
		if _, err := pool.Exec(ctx, insertOverride, channelID, roleID, masks[0], masks[1]); err == nil {
			t.Errorf("%s: insert succeeded, want a CHECK violation", name)
		}
	}
	if n := count(ctx, t, pool, `SELECT COUNT(*) FROM channel_permission_overrides`); n != 0 {
		t.Errorf("got %d overrides, want 0", n)
	}
}

// #57 regla 11: deleting the channel or the role deletes its overrides.
func TestMigration0020_DeletingChannelOrRoleCascades(t *testing.T) {
	pool := newPool(t)
	ctx := context.Background()

	channelID, roleID := seed(ctx, t, pool)
	exec(ctx, t, pool, insertOverride, channelID, roleID, role.PermSendMessages, 0)
	exec(ctx, t, pool, `DELETE FROM channels WHERE id = $1`, channelID)
	if n := count(ctx, t, pool, `SELECT COUNT(*) FROM channel_permission_overrides WHERE role_id = $1`, roleID); n != 0 {
		t.Error("the override outlived its channel")
	}

	channelID, roleID = seed(ctx, t, pool)
	exec(ctx, t, pool, insertOverride, channelID, roleID, role.PermSendMessages, 0)
	exec(ctx, t, pool, `DELETE FROM roles WHERE id = $1`, roleID)
	if n := count(ctx, t, pool, `SELECT COUNT(*) FROM channel_permission_overrides WHERE channel_id = $1`, channelID); n != 0 {
		t.Error("the override outlived its role")
	}
}
