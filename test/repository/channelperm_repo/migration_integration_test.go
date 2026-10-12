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
		"../../../migrations/0011_outbox_events.up.sql",
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
	_, _ = pool.Exec(ctx, `DROP TABLE IF EXISTS outbox_events, channel_permission_overrides, member_roles, roles, categories, server_members, channels, servers CASCADE`)
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

func applyMigration(ctx context.Context, t *testing.T, pool *pgxpool.Pool, file string) {
	t.Helper()
	schema, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read migration %s: %v", file, err)
	}
	if _, err := pool.Exec(ctx, string(schema)); err != nil {
		t.Fatalf("apply migration %s: %v", file, err)
	}
}

func TestMigration0025_GrantsConnectToWhoeverCouldSeeChannels(t *testing.T) {
	pool := newPool(t)
	ctx := context.Background()
	channelID, viewerID := seed(ctx, t, pool)
	var serverID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT server_id FROM channels WHERE id = $1`, channelID).Scan(&serverID); err != nil {
		t.Fatalf("read server: %v", err)
	}
	blindID := uuid.New()
	exec(ctx, t, pool, `UPDATE roles SET permissions = $2 WHERE id = $1`, viewerID, role.PermViewChannels|role.PermSendMessages)
	exec(ctx, t, pool, `INSERT INTO roles (id, server_id, name, color, position, permissions) VALUES ($1, $2, 'Blind', '#000000', 2, $3)`,
		blindID, serverID, role.PermSendMessages)
	exec(ctx, t, pool, insertOverride, channelID, blindID, role.PermViewChannels, 0)

	applyMigration(ctx, t, pool, "../../../migrations/0023_add_reactions_permission.up.sql")
	applyMigration(ctx, t, pool, "../../../migrations/0025_connect_permission.up.sql")

	var viewerPerms, blindPerms role.Permission
	if err := pool.QueryRow(ctx, `SELECT permissions FROM roles WHERE id = $1`, viewerID).Scan(&viewerPerms); err != nil {
		t.Fatalf("read viewer: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT permissions FROM roles WHERE id = $1`, blindID).Scan(&blindPerms); err != nil {
		t.Fatalf("read blind: %v", err)
	}
	if viewerPerms&role.PermConnect == 0 {
		t.Errorf("a role with VIEW_CHANNELS must get CONNECT, got %v", role.PermissionNames(viewerPerms))
	}
	if blindPerms&role.PermConnect != 0 {
		t.Errorf("a role without VIEW_CHANNELS must not get CONNECT, got %v", role.PermissionNames(blindPerms))
	}

	var allow role.Permission
	if err := pool.QueryRow(ctx, `SELECT allow FROM channel_permission_overrides WHERE role_id = $1`, blindID).Scan(&allow); err != nil {
		t.Fatalf("read override: %v", err)
	}
	if allow != role.PermViewChannels|role.PermConnect {
		t.Errorf("override allow = %v, want [CONNECT VIEW_CHANNELS]", role.PermissionNames(allow))
	}

	exec(ctx, t, pool, insertOverride, channelID, viewerID, 0, role.PermConnect)
	if _, err := pool.Exec(ctx, `UPDATE channel_permission_overrides SET allow = allow | $2 WHERE role_id = $1`, blindID, role.PermKickMembers); err == nil {
		t.Error("a server-level permission in an override must still violate the CHECK")
	}

	if n := count(ctx, t, pool, `SELECT COUNT(*) FROM outbox_events WHERE routing_key = 'servers.role_updated' AND payload->'permissions' ? 'CONNECT'`); n != 1 {
		t.Errorf("role_updated events with CONNECT = %d, want 1", n)
	}
	if n := count(ctx, t, pool, `SELECT COUNT(*) FROM outbox_events WHERE routing_key = 'servers.channel_overrides_updated' AND payload::text LIKE '%CONNECT%'`); n != 1 {
		t.Errorf("channel_overrides_updated events with CONNECT = %d, want 1", n)
	}
}
