package access_events_test

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/discordia-grupo01/events-contract/permissions"
	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/channelperm"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/outbox"
	"github.com/discordia-grupo01/servers/internal/repository"
	"github.com/discordia-grupo01/servers/internal/repository/channelperm_repo"
	"github.com/discordia-grupo01/servers/internal/repository/member_repo"
	"github.com/discordia-grupo01/servers/internal/repository/ownership_transfer_repo"
	"github.com/discordia-grupo01/servers/internal/repository/role_repo"
)

const owner = "owner-1"

type world struct {
	serverID          uuid.UUID
	general           uuid.UUID
	everyoneID, modID uuid.UUID
	overrides         *channelperm_repo.Repository
	roles             *role_repo.RoleRepository
	members           *member_repo.MemberRepository
	transfers         *ownership_transfer_repo.Repository
	invitations       *repository.InvitationRepository
	servers           *repository.ServerRepository
	pool              *pgxpool.Pool
}

func seedWorld(ctx context.Context, t *testing.T) world {
	t.Helper()
	pool := newPool(t)
	w := world{
		serverID: uuid.New(), general: uuid.New(), everyoneID: uuid.New(), modID: uuid.New(),
		overrides:   channelperm_repo.NewRepository(pool),
		roles:       role_repo.NewRoleRepository(pool),
		members:     member_repo.NewMemberRepository(pool),
		transfers:   ownership_transfer_repo.NewRepository(pool),
		invitations: repository.NewInvitationRepository(pool),
		servers:     repository.NewServerRepository(pool),
		pool:        pool,
	}
	exec(ctx, t, pool, `INSERT INTO servers (id, name, name_key, owner_id) VALUES ($1, 'Server', $2, $3)`, w.serverID, "s-"+w.serverID.String(), owner)
	exec(ctx, t, pool, `INSERT INTO channels (id, server_id, name, kind) VALUES ($1, $2, 'general', 'text')`, w.general, w.serverID)
	exec(ctx, t, pool, `INSERT INTO roles (id, server_id, name, color, permissions, position, is_everyone) VALUES ($1, $2, '@everyone', '#9e9e9e', $3, 0, TRUE)`,
		w.everyoneID, w.serverID, role.PermViewChannels|role.PermSendMessages)
	exec(ctx, t, pool, `INSERT INTO roles (id, server_id, name, color, permissions, position) VALUES ($1, $2, 'Mod', '#000000', $3, 1)`, w.modID, w.serverID, role.PermManageRoles|role.PermManageMessages)
	for _, u := range []string{owner, "mod-1", "member-1"} {
		exec(ctx, t, pool, `INSERT INTO server_members (server_id, user_id, is_owner) VALUES ($1, $2, $3)`, w.serverID, u, u == owner)
		exec(ctx, t, pool, `INSERT INTO member_roles (server_id, user_id, role_id) VALUES ($1, $2, $3)`, w.serverID, u, w.everyoneID)
	}
	exec(ctx, t, pool, `INSERT INTO member_roles (server_id, user_id, role_id) VALUES ($1, 'mod-1', $2)`, w.serverID, w.modID)
	return w
}

func allowAll(role.Standing, role.Role) error { return nil }

func (w world) clearOutbox(ctx context.Context, t *testing.T) {
	t.Helper()
	exec(ctx, t, w.pool, `DELETE FROM outbox_events`)
}

func (w world) published(ctx context.Context, t *testing.T, routingKey string) []map[string]any {
	t.Helper()
	rows, err := w.pool.Query(ctx, `SELECT payload FROM outbox_events WHERE routing_key = $1 ORDER BY created_at`, routingKey)
	if err != nil {
		t.Fatalf("read outbox: %v", err)
	}
	defer rows.Close()

	var events []map[string]any
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			t.Fatalf("scan outbox: %v", err)
		}
		var evt map[string]any
		if err := json.Unmarshal(payload, &evt); err != nil {
			t.Fatalf("decode %s: %v", payload, err)
		}
		if evt["event_id"] == "" || evt["occurred_at"] == "" || evt["server_id"] != w.serverID.String() {
			t.Errorf("incomplete event: %s", payload)
		}
		events = append(events, evt)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read outbox: %v", err)
	}
	return events
}

func (w world) expectOne(ctx context.Context, t *testing.T, routingKey string, want map[string]any) {
	t.Helper()
	events := w.published(ctx, t, routingKey)
	if len(events) != 1 {
		t.Fatalf("%s: %d events, want 1: %v", routingKey, len(events), events)
	}
	for key, value := range want {
		if !reflect.DeepEqual(events[0][key], value) {
			t.Errorf("%s.%s = %v, want %v", routingKey, key, events[0][key], value)
		}
	}
}

func (w world) expectNone(ctx context.Context, t *testing.T, routingKey string) {
	t.Helper()
	if events := w.published(ctx, t, routingKey); len(events) != 0 {
		t.Errorf("%s: %d events, want none: %v", routingKey, len(events), events)
	}
}

func sortedIDs(ids ...uuid.UUID) []any {
	s := make([]string, len(ids))
	for i, id := range ids {
		s[i] = id.String()
	}
	sort.Strings(s)
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}

func TestCreateRole_PublishesItsSnapshot(t *testing.T) {
	ctx := context.Background()
	w := seedWorld(ctx, t)
	w.clearOutbox(ctx, t)

	now := time.Now().UTC()
	created, err := w.roles.Create(ctx, role.Role{
		ID: uuid.New(), ServerID: w.serverID, Name: "VIP", Color: "#ffffff",
		Permissions: role.PermSendMessages | role.PermViewChannels, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	w.expectOne(ctx, t, permissions.RoutingKeyRoleUpdated, map[string]any{
		"role_id":     created.ID.String(),
		"permissions": []any{"SEND_MESSAGES", "VIEW_CHANNELS"},
		"is_everyone": false,
	})
}

func TestUpdateRole_PublishesItsNewSnapshot(t *testing.T) {
	ctx := context.Background()
	w := seedWorld(ctx, t)
	w.clearOutbox(ctx, t)

	mod, err := w.roles.GetByID(ctx, w.modID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	mod.Permissions = role.PermMentionEveryone
	mod.UpdatedAt = time.Now().UTC()
	if err := w.roles.Update(ctx, mod); err != nil {
		t.Fatalf("Update: %v", err)
	}

	w.expectOne(ctx, t, permissions.RoutingKeyRoleUpdated, map[string]any{
		"role_id":     w.modID.String(),
		"permissions": []any{"MENTION_EVERYONE"},
		"is_everyone": false,
	})
}

func TestUpdateRole_NotFound_PublishesNothing(t *testing.T) {
	ctx := context.Background()
	w := seedWorld(ctx, t)
	w.clearOutbox(ctx, t)

	err := w.roles.Update(ctx, role.Role{ID: uuid.New(), ServerID: w.serverID, Name: "ghost", Color: "#000000"})
	if err == nil {
		t.Fatal("Update of a missing role succeeded")
	}

	w.expectNone(ctx, t, permissions.RoutingKeyRoleUpdated)
}

func TestDeleteRole_PublishesRoleDeleted(t *testing.T) {
	ctx := context.Background()
	w := seedWorld(ctx, t)
	w.clearOutbox(ctx, t)

	if err := w.roles.Delete(ctx, w.modID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	w.expectOne(ctx, t, permissions.RoutingKeyRoleDeleted, map[string]any{"role_id": w.modID.String()})
}

func TestDeleteRole_Everyone_PublishesNothing(t *testing.T) {
	ctx := context.Background()
	w := seedWorld(ctx, t)
	w.clearOutbox(ctx, t)

	if err := w.roles.Delete(ctx, w.everyoneID); err == nil {
		t.Fatal("deleting @everyone succeeded")
	}

	w.expectNone(ctx, t, permissions.RoutingKeyRoleDeleted)
}

func TestAssignRole_PublishesTheFullSetOfRoles(t *testing.T) {
	ctx := context.Background()
	w := seedWorld(ctx, t)
	w.clearOutbox(ctx, t)

	if err := w.members.AssignRole(ctx, w.serverID, "member-1", w.modID); err != nil {
		t.Fatalf("AssignRole: %v", err)
	}

	w.expectOne(ctx, t, permissions.RoutingKeyMemberRolesUpdated, map[string]any{
		"user_id":  "member-1",
		"role_ids": sortedIDs(w.everyoneID, w.modID),
	})
}

func TestRemoveRole_PublishesTheRemainingRoles(t *testing.T) {
	ctx := context.Background()
	w := seedWorld(ctx, t)
	w.clearOutbox(ctx, t)

	if err := w.members.RemoveRole(ctx, w.serverID, "mod-1", w.modID); err != nil {
		t.Fatalf("RemoveRole: %v", err)
	}

	w.expectOne(ctx, t, permissions.RoutingKeyMemberRolesUpdated, map[string]any{
		"user_id":  "mod-1",
		"role_ids": sortedIDs(w.everyoneID),
	})
}

func TestRemoveRole_NotHeld_PublishesNothing(t *testing.T) {
	ctx := context.Background()
	w := seedWorld(ctx, t)
	w.clearOutbox(ctx, t)

	if err := w.members.RemoveRole(ctx, w.serverID, "member-1", w.modID); err == nil {
		t.Fatal("RemoveRole of a role not held succeeded")
	}

	w.expectNone(ctx, t, permissions.RoutingKeyMemberRolesUpdated)
}

func TestOverride_PublishesEveryOverrideOfTheChannel(t *testing.T) {
	ctx := context.Background()
	w := seedWorld(ctx, t)
	set := func(roleID uuid.UUID, allow, deny role.Permission) {
		t.Helper()
		o := channelperm.Override{ChannelID: w.general, RoleID: roleID, Allow: allow, Deny: deny, UpdatedAt: time.Now().UTC()}
		if _, err := w.overrides.Upsert(ctx, w.serverID, owner, o, allowAll); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
	}
	set(w.everyoneID, 0, role.PermSendMessages)
	w.clearOutbox(ctx, t)

	set(w.modID, role.PermManageMessages|role.PermSendMessages, 0)

	events := w.published(ctx, t, permissions.RoutingKeyChannelOverridesUpdated)
	if len(events) != 1 {
		t.Fatalf("%d events, want 1", len(events))
	}
	wantOverrides := map[string]map[string]any{
		w.everyoneID.String(): {"allow": []any{}, "deny": []any{"SEND_MESSAGES"}},
		w.modID.String():      {"allow": []any{"MANAGE_MESSAGES", "SEND_MESSAGES"}, "deny": []any{}},
	}
	got := map[string]map[string]any{}
	for _, raw := range events[0]["overrides"].([]any) {
		entry := raw.(map[string]any)
		got[entry["role_id"].(string)] = map[string]any{"allow": entry["allow"], "deny": entry["deny"]}
	}
	if !reflect.DeepEqual(got, wantOverrides) {
		t.Errorf("overrides = %v, want %v", got, wantOverrides)
	}
	if events[0]["channel_id"] != w.general.String() {
		t.Errorf("channel_id = %v", events[0]["channel_id"])
	}
}

func TestDeleteOverride_PublishesAnEmptyListWhenNoneLeft(t *testing.T) {
	ctx := context.Background()
	w := seedWorld(ctx, t)
	o := channelperm.Override{ChannelID: w.general, RoleID: w.modID, Deny: role.PermSendMessages, UpdatedAt: time.Now().UTC()}
	if _, err := w.overrides.Upsert(ctx, w.serverID, owner, o, allowAll); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	w.clearOutbox(ctx, t)

	deleted, err := w.overrides.Delete(ctx, w.serverID, owner, w.general, w.modID, allowAll)
	if err != nil || !deleted {
		t.Fatalf("Delete = %v, %v", deleted, err)
	}

	w.expectOne(ctx, t, permissions.RoutingKeyChannelOverridesUpdated, map[string]any{
		"channel_id": w.general.String(),
		"overrides":  []any{},
	})
}

func TestDeleteOverride_NoneThere_PublishesNothing(t *testing.T) {
	ctx := context.Background()
	w := seedWorld(ctx, t)
	w.clearOutbox(ctx, t)

	deleted, err := w.overrides.Delete(ctx, w.serverID, owner, w.general, w.modID, allowAll)
	if err != nil || deleted {
		t.Fatalf("Delete = %v, %v", deleted, err)
	}

	w.expectNone(ctx, t, permissions.RoutingKeyChannelOverridesUpdated)
}

func TestAcceptTransfer_PublishesTheNewOwner(t *testing.T) {
	ctx := context.Background()
	w := seedWorld(ctx, t)
	transfer := model.OwnershipTransfer{
		ID: uuid.New(), ServerID: w.serverID, FromUserID: owner, ToUserID: "member-1",
		Status: model.OwnershipTransferPending, CreatedAt: time.Now().UTC(),
	}
	if err := w.transfers.Create(ctx, transfer); err != nil {
		t.Fatalf("Create: %v", err)
	}
	w.clearOutbox(ctx, t)

	if _, err := w.transfers.Accept(ctx, transfer.ID, time.Now().UTC()); err != nil {
		t.Fatalf("Accept: %v", err)
	}

	w.expectOne(ctx, t, permissions.RoutingKeyOwnerUpdated, map[string]any{"owner_id": "member-1"})
}

func TestRedeemInvitation_PublishesTheRolesOfTheNewMember(t *testing.T) {
	ctx := context.Background()
	w := seedWorld(ctx, t)
	exec(ctx, t, w.pool, `UPDATE servers SET default_role_id = $1 WHERE id = $2`, w.modID, w.serverID)
	inv := model.Invitation{
		ID: uuid.New(), Code: "code-" + uuid.NewString(), ServerID: w.serverID, CreatedBy: owner,
		CreatedAt: time.Now().UTC(),
	}
	if err := w.invitations.CreateInvitation(ctx, inv); err != nil {
		t.Fatalf("CreateInvitation: %v", err)
	}
	w.clearOutbox(ctx, t)

	_, err := w.invitations.RedeemInvitation(ctx, inv.Code, "newcomer", time.Now().UTC(), func(serverID uuid.UUID) (outbox.Event, error) {
		return outbox.Event{RoutingKey: "servers.member_joined", Payload: []byte(`{}`)}, nil
	})
	if err != nil {
		t.Fatalf("RedeemInvitation: %v", err)
	}

	w.expectOne(ctx, t, permissions.RoutingKeyMemberRolesUpdated, map[string]any{
		"user_id":  "newcomer",
		"role_ids": sortedIDs(w.everyoneID, w.modID),
	})
}

func TestCreateServer_PublishesOwnerEveryoneRoleAndOwnerRoles(t *testing.T) {
	ctx := context.Background()
	w := seedWorld(ctx, t)
	w.clearOutbox(ctx, t)

	srv := model.Server{
		ID: uuid.New(), Name: "Fresh", NameKey: "fresh-" + uuid.NewString(), OwnerID: "creator",
		CreatedAt: time.Now().UTC(),
	}
	if err := w.servers.CreateServer(ctx, srv, nil, nil, nil); err != nil {
		t.Fatalf("CreateServer: %v", err)
	}

	for _, key := range []string{
		permissions.RoutingKeyOwnerUpdated,
		permissions.RoutingKeyRoleUpdated,
		permissions.RoutingKeyMemberRolesUpdated,
	} {
		var payload []byte
		err := w.pool.QueryRow(ctx, `SELECT payload FROM outbox_events WHERE routing_key = $1`, key).Scan(&payload)
		if err != nil {
			t.Fatalf("%s not published: %v", key, err)
		}
		var evt map[string]any
		_ = json.Unmarshal(payload, &evt)
		if evt["server_id"] != srv.ID.String() {
			t.Errorf("%s server_id = %v", key, evt["server_id"])
		}
		switch key {
		case permissions.RoutingKeyOwnerUpdated:
			if evt["owner_id"] != "creator" {
				t.Errorf("owner_id = %v", evt["owner_id"])
			}
		case permissions.RoutingKeyRoleUpdated:
			if evt["is_everyone"] != true || !reflect.DeepEqual(evt["permissions"], []any{"ADD_REACTIONS", "CREATE_INVITE", "SEND_MESSAGES", "VIEW_CHANNELS"}) {
				t.Errorf("role = %v", evt)
			}
		case permissions.RoutingKeyMemberRolesUpdated:
			if evt["user_id"] != "creator" || len(evt["role_ids"].([]any)) != 1 {
				t.Errorf("member roles = %v", evt)
			}
		}
	}
}

func TestBackfill_AnnouncesEverythingAlreadyStored(t *testing.T) {
	ctx := context.Background()
	w := seedWorld(ctx, t)
	o := channelperm.Override{ChannelID: w.general, RoleID: w.everyoneID, Allow: role.PermViewChannels, Deny: role.PermSendMessages, UpdatedAt: time.Now().UTC()}
	if _, err := w.overrides.Upsert(ctx, w.serverID, owner, o, allowAll); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	w.clearOutbox(ctx, t)

	backfill, err := os.ReadFile("../../../migrations/0022_backfill_access_events.up.sql")
	if err != nil {
		t.Fatalf("read backfill: %v", err)
	}
	if _, err := w.pool.Exec(ctx, string(backfill)); err != nil {
		t.Fatalf("apply backfill: %v", err)
	}

	w.expectOne(ctx, t, permissions.RoutingKeyOwnerUpdated, map[string]any{"owner_id": owner})

	roles := map[string]any{}
	for _, evt := range w.published(ctx, t, permissions.RoutingKeyRoleUpdated) {
		roles[evt["role_id"].(string)] = evt["permissions"]
	}
	wantRoles := map[string]any{
		w.everyoneID.String(): []any{"SEND_MESSAGES", "VIEW_CHANNELS"},
		w.modID.String():      []any{"MANAGE_MESSAGES", "MANAGE_ROLES"},
	}
	if !reflect.DeepEqual(roles, wantRoles) {
		t.Errorf("roles = %v, want %v", roles, wantRoles)
	}

	members := map[string]any{}
	for _, evt := range w.published(ctx, t, permissions.RoutingKeyMemberRolesUpdated) {
		members[evt["user_id"].(string)] = evt["role_ids"]
	}
	wantMembers := map[string]any{
		owner:      sortedIDs(w.everyoneID),
		"member-1": sortedIDs(w.everyoneID),
		"mod-1":    sortedIDs(w.everyoneID, w.modID),
	}
	if !reflect.DeepEqual(members, wantMembers) {
		t.Errorf("members = %v, want %v", members, wantMembers)
	}

	w.expectOne(ctx, t, permissions.RoutingKeyChannelOverridesUpdated, map[string]any{
		"channel_id": w.general.String(),
		"overrides": []any{map[string]any{
			"role_id": w.everyoneID.String(),
			"allow":   []any{"VIEW_CHANNELS"},
			"deny":    []any{"SEND_MESSAGES"},
		}},
	})
}

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
		"../../../migrations/0002_invitations.up.sql",
		"../../../migrations/0003_roles.up.sql",
		"../../../migrations/0004_member_roles.up.sql",
		"../../../migrations/0005_default_role.up.sql",
		"../../../migrations/0006_ownership_transfers.up.sql",
		"../../../migrations/0007_categories.up.sql",
		"../../../migrations/0008_ownership_transfer_cancelled_status.up.sql",
		"../../../migrations/0011_outbox_events.up.sql",
		"../../../migrations/0015_role_positions.up.sql",
		"../../../migrations/0016_server_bans_banned_by.up.sql",
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
	_, _ = pool.Exec(ctx, `DROP TABLE IF EXISTS channel_permission_overrides, outbox_events, ownership_transfers, member_roles, roles, categories, server_bans, invitations, server_members, channels, servers CASCADE`)
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
