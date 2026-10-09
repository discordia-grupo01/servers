// Package channel_access_test checks that every write that can hide a channel
// from someone publishes servers.channel_access_revoked with exactly who lost
// it, and that writes that hide nothing publish nothing.
package channel_access_test

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/discordia-grupo01/events-contract/channels"
	"github.com/discordia-grupo01/events-contract/membership"
	moderationevents "github.com/discordia-grupo01/events-contract/moderation"
	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/channelperm"
	"github.com/discordia-grupo01/servers/internal/model/moderation"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/outbox"
	"github.com/discordia-grupo01/servers/internal/repository"
	"github.com/discordia-grupo01/servers/internal/repository/channelperm_repo"
	"github.com/discordia-grupo01/servers/internal/repository/member_repo"
	"github.com/discordia-grupo01/servers/internal/repository/moderation_repo"
	"github.com/discordia-grupo01/servers/internal/repository/ownership_transfer_repo"
	"github.com/discordia-grupo01/servers/internal/repository/role_repo"
)

const owner = "owner-1"

// world is a server owned by owner with a #general and a #staff channel,
// @everyone (VIEW_CHANNELS | SEND_MESSAGES) and a Mod role, and two members
// besides the owner: mod-1 (Mod) and member-1.
type world struct {
	serverID          uuid.UUID
	general, staff    uuid.UUID
	everyoneID, modID uuid.UUID
	overrides         *channelperm_repo.Repository
	roles             *role_repo.RoleRepository
	members           *member_repo.MemberRepository
	transfers         *ownership_transfer_repo.Repository
	moderation        *moderation_repo.Repository
	pool              *pgxpool.Pool
}

func seedWorld(ctx context.Context, t *testing.T) world {
	t.Helper()
	pool := newPool(t)
	w := world{
		serverID: uuid.New(), general: uuid.New(), staff: uuid.New(), everyoneID: uuid.New(), modID: uuid.New(),
		overrides:  channelperm_repo.NewRepository(pool),
		roles:      role_repo.NewRoleRepository(pool),
		members:    member_repo.NewMemberRepository(pool),
		transfers:  ownership_transfer_repo.NewRepository(pool),
		moderation: moderation_repo.NewRepository(pool),
		pool:       pool,
	}
	exec(ctx, t, pool, `INSERT INTO servers (id, name, name_key, owner_id) VALUES ($1, 'Server', $2, $3)`, w.serverID, "s-"+w.serverID.String(), owner)
	exec(ctx, t, pool, `INSERT INTO channels (id, server_id, name, kind) VALUES ($1, $2, 'general', 'text'), ($3, $2, 'staff', 'voice')`, w.general, w.serverID, w.staff)
	exec(ctx, t, pool, `INSERT INTO roles (id, server_id, name, color, permissions, position, is_everyone) VALUES ($1, $2, '@everyone', '#9e9e9e', $3, 0, TRUE)`,
		w.everyoneID, w.serverID, role.PermViewChannels|role.PermSendMessages)
	exec(ctx, t, pool, `INSERT INTO roles (id, server_id, name, color, permissions, position) VALUES ($1, $2, 'Mod', '#000000', $3, 1)`, w.modID, w.serverID, role.PermManageRoles)
	for _, u := range []string{owner, "mod-1", "member-1"} {
		exec(ctx, t, pool, `INSERT INTO server_members (server_id, user_id, is_owner) VALUES ($1, $2, $3)`, w.serverID, u, u == owner)
		exec(ctx, t, pool, `INSERT INTO member_roles (server_id, user_id, role_id) VALUES ($1, $2, $3)`, w.serverID, u, w.everyoneID)
	}
	exec(ctx, t, pool, `INSERT INTO member_roles (server_id, user_id, role_id) VALUES ($1, 'mod-1', $2)`, w.serverID, w.modID)
	return w
}

func allowAll(role.Standing, role.Role) error { return nil }

func (w world) override(ctx context.Context, t *testing.T, roleID uuid.UUID, allow, deny role.Permission) {
	t.Helper()
	o := channelperm.Override{ChannelID: w.staff, RoleID: roleID, Allow: allow, Deny: deny, UpdatedAt: time.Now().UTC()}
	if _, err := w.overrides.Upsert(ctx, w.serverID, owner, o, allowAll); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
}

// makeStaffPrivate hides #staff from @everyone and shows it to Mod, then
// forgets the events that took.
func (w world) makeStaffPrivate(ctx context.Context, t *testing.T) {
	t.Helper()
	w.override(ctx, t, w.modID, role.PermViewChannels, 0)
	w.override(ctx, t, w.everyoneID, 0, role.PermViewChannels)
	exec(ctx, t, w.pool, `DELETE FROM outbox_events`)
}

// revoked returns the channel_access_revoked events in the outbox, keyed by
// channel, checking their server and that nothing else was published besides
// the routing keys in also (the events a kick, ban or leave publishes anyway).
func (w world) revoked(ctx context.Context, t *testing.T, also ...string) map[uuid.UUID][]string {
	t.Helper()
	rows, err := w.pool.Query(ctx, `SELECT routing_key, payload FROM outbox_events WHERE NOT routing_key = ANY($1)`, append([]string{}, also...))
	if err != nil {
		t.Fatalf("read outbox: %v", err)
	}
	defer rows.Close()

	got := make(map[uuid.UUID][]string)
	for rows.Next() {
		var key string
		var payload []byte
		if err := rows.Scan(&key, &payload); err != nil {
			t.Fatalf("scan outbox: %v", err)
		}
		if key != channels.RoutingKeyChannelAccessRevoked {
			t.Fatalf("unexpected event %q: %s", key, payload)
		}
		var evt channels.ChannelAccessRevoked
		if err := json.Unmarshal(payload, &evt); err != nil {
			t.Fatalf("decode %s: %v", payload, err)
		}
		if evt.ServerID != w.serverID.String() || evt.EventID == "" || evt.OccurredAt.IsZero() {
			t.Errorf("event = %+v", evt)
		}
		channelID := uuid.MustParse(evt.ChannelID)
		if _, dup := got[channelID]; dup {
			t.Errorf("two events for channel %s", channelID)
		}
		got[channelID] = evt.UserIDs
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read outbox: %v", err)
	}
	return got
}

func (w world) expectRevoked(ctx context.Context, t *testing.T, want map[uuid.UUID][]string, also ...string) {
	t.Helper()
	got := w.revoked(ctx, t, also...)
	if len(want) == 0 && len(got) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("revoked = %v, want %v", got, want)
	}
}

func TestOverride_HidingChannel_PublishesWhoLostIt(t *testing.T) {
	ctx := context.Background()
	w := seedWorld(ctx, t)

	w.override(ctx, t, w.everyoneID, 0, role.PermViewChannels)

	// The owner sees everything; mod-1 has no allow yet.
	w.expectRevoked(ctx, t, map[uuid.UUID][]string{w.staff: {"member-1", "mod-1"}})
}

func TestOverride_NotTouchingView_PublishesNothing(t *testing.T) {
	ctx := context.Background()
	w := seedWorld(ctx, t)

	w.override(ctx, t, w.everyoneID, 0, role.PermSendMessages)

	w.expectRevoked(ctx, t, nil)
}

func TestOverride_GrantingAccess_PublishesNothing(t *testing.T) {
	ctx := context.Background()
	w := seedWorld(ctx, t)
	w.makeStaffPrivate(ctx, t)

	if _, err := w.overrides.Delete(ctx, w.serverID, owner, w.staff, w.everyoneID, allowAll); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	w.expectRevoked(ctx, t, nil)
}

func TestOverride_DeletingTheAllow_PublishesWhoLostIt(t *testing.T) {
	ctx := context.Background()
	w := seedWorld(ctx, t)
	w.makeStaffPrivate(ctx, t)

	if _, err := w.overrides.Delete(ctx, w.serverID, owner, w.staff, w.modID, allowAll); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	w.expectRevoked(ctx, t, map[uuid.UUID][]string{w.staff: {"mod-1"}})
}

func TestOverride_RejectedWrite_PublishesNothing(t *testing.T) {
	ctx := context.Background()
	w := seedWorld(ctx, t)

	o := channelperm.Override{ChannelID: w.staff, RoleID: w.everyoneID, Deny: role.PermViewChannels, UpdatedAt: time.Now().UTC()}
	_, err := w.overrides.Upsert(ctx, w.serverID, "member-1", o, func(role.Standing, role.Role) error { return model.ErrForbiddenMissingPermission })
	if err == nil {
		t.Fatal("Upsert succeeded, want the check's error")
	}

	w.expectRevoked(ctx, t, nil)
}

func TestRoleUpdate_TakingViewAway_PublishesEveryChannel(t *testing.T) {
	ctx := context.Background()
	w := seedWorld(ctx, t)

	rl, err := w.roles.GetByID(ctx, w.everyoneID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	rl.Permissions = role.PermSendMessages
	rl.UpdatedAt = time.Now().UTC()
	if err := w.roles.Update(ctx, rl); err != nil {
		t.Fatalf("Update: %v", err)
	}

	w.expectRevoked(ctx, t, map[uuid.UUID][]string{
		w.general: {"member-1", "mod-1"},
		w.staff:   {"member-1", "mod-1"},
	})
}

func TestRoleUpdate_RenameOnly_PublishesNothing(t *testing.T) {
	ctx := context.Background()
	w := seedWorld(ctx, t)

	rl, err := w.roles.GetByID(ctx, w.modID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	rl.Name = "Moderadores"
	rl.UpdatedAt = time.Now().UTC()
	if err := w.roles.Update(ctx, rl); err != nil {
		t.Fatalf("Update: %v", err)
	}

	w.expectRevoked(ctx, t, nil)
}

func TestRoleDelete_RoleThatGaveAccess_PublishesWhoLostIt(t *testing.T) {
	ctx := context.Background()
	w := seedWorld(ctx, t)
	w.makeStaffPrivate(ctx, t)

	if err := w.roles.Delete(ctx, w.modID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	w.expectRevoked(ctx, t, map[uuid.UUID][]string{w.staff: {"mod-1"}})
}

func TestRemoveRole_RoleThatGaveAccess_PublishesWhoLostIt(t *testing.T) {
	ctx := context.Background()
	w := seedWorld(ctx, t)
	w.makeStaffPrivate(ctx, t)

	if err := w.members.RemoveRole(ctx, w.serverID, "mod-1", w.modID); err != nil {
		t.Fatalf("RemoveRole: %v", err)
	}

	w.expectRevoked(ctx, t, map[uuid.UUID][]string{w.staff: {"mod-1"}})
}

func TestAssignRole_RoleWithDenyOverride_PublishesWhoLostIt(t *testing.T) {
	ctx := context.Background()
	w := seedWorld(ctx, t)
	w.override(ctx, t, w.modID, 0, role.PermViewChannels)
	exec(ctx, t, w.pool, `DELETE FROM outbox_events`)

	if err := w.members.AssignRole(ctx, w.serverID, "member-1", w.modID); err != nil {
		t.Fatalf("AssignRole: %v", err)
	}

	w.expectRevoked(ctx, t, map[uuid.UUID][]string{w.staff: {"member-1"}})
}

func TestAssignRole_AlreadyHeld_PublishesNothing(t *testing.T) {
	ctx := context.Background()
	w := seedWorld(ctx, t)
	w.makeStaffPrivate(ctx, t)

	if err := w.members.AssignRole(ctx, w.serverID, "mod-1", w.modID); err != nil {
		t.Fatalf("AssignRole: %v", err)
	}

	w.expectRevoked(ctx, t, nil)
}

func TestAcceptTransfer_PreviousOwnerLosesPrivateChannels(t *testing.T) {
	ctx := context.Background()
	w := seedWorld(ctx, t)
	w.makeStaffPrivate(ctx, t)
	transfer := model.OwnershipTransfer{
		ID: uuid.New(), ServerID: w.serverID, FromUserID: owner, ToUserID: "member-1",
		Status: model.OwnershipTransferPending, CreatedAt: time.Now().UTC(),
	}
	if err := w.transfers.Create(ctx, transfer); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := w.transfers.Accept(ctx, transfer.ID, time.Now().UTC()); err != nil {
		t.Fatalf("Accept: %v", err)
	}

	// The new owner gains #staff, which isn't published.
	w.expectRevoked(ctx, t, map[uuid.UUID][]string{w.staff: {owner}})
}

// event is a stand-in for the member_left / member_kicked / member_banned that
// a kick, ban or leave publishes alongside.
func event(routingKey string) outbox.Event {
	return outbox.Event{RoutingKey: routingKey, Payload: []byte(`{}`)}
}

func allowModeration(_, _ role.Standing) error { return nil }

func TestKick_PublishesEveryChannelTheMemberSaw(t *testing.T) {
	ctx := context.Background()
	w := seedWorld(ctx, t)
	w.makeStaffPrivate(ctx, t)

	kick := moderation.Kick{ServerID: w.serverID, UserID: "mod-1", KickedBy: owner, KickedAt: time.Now().UTC()}
	if err := w.moderation.KickMember(ctx, kick, allowModeration,
		event(membership.RoutingKeyMemberLeft), event(moderationevents.RoutingKeyMemberKicked)); err != nil {
		t.Fatalf("KickMember: %v", err)
	}

	w.expectRevoked(ctx, t, map[uuid.UUID][]string{
		w.general: {"mod-1"},
		w.staff:   {"mod-1"},
	}, membership.RoutingKeyMemberLeft, moderationevents.RoutingKeyMemberKicked)
}

func TestKick_Rejected_PublishesNothing(t *testing.T) {
	ctx := context.Background()
	w := seedWorld(ctx, t)

	kick := moderation.Kick{ServerID: w.serverID, UserID: "mod-1", KickedBy: "member-1", KickedAt: time.Now().UTC()}
	err := w.moderation.KickMember(ctx, kick, func(_, _ role.Standing) error { return model.ErrForbiddenMissingPermission },
		event(membership.RoutingKeyMemberLeft), event(moderationevents.RoutingKeyMemberKicked))
	if err == nil {
		t.Fatal("KickMember succeeded, want the check's error")
	}

	w.expectRevoked(ctx, t, nil)
}

func TestBan_PublishesEveryChannelTheMemberSaw(t *testing.T) {
	ctx := context.Background()
	w := seedWorld(ctx, t)
	w.makeStaffPrivate(ctx, t)

	by := owner
	ban := moderation.Ban{ServerID: w.serverID, UserID: "member-1", BannedBy: &by, BannedAt: time.Now().UTC()}
	if err := w.moderation.BanMember(ctx, ban, allowModeration,
		event(membership.RoutingKeyMemberLeft), event(moderationevents.RoutingKeyMemberBanned)); err != nil {
		t.Fatalf("BanMember: %v", err)
	}

	// member-1 never saw the private #staff.
	w.expectRevoked(ctx, t, map[uuid.UUID][]string{w.general: {"member-1"}},
		membership.RoutingKeyMemberLeft, moderationevents.RoutingKeyMemberBanned)
}

func TestLeave_PublishesEveryChannelTheMemberSaw(t *testing.T) {
	ctx := context.Background()
	w := seedWorld(ctx, t)
	w.makeStaffPrivate(ctx, t)

	removed, err := w.members.RemoveMember(ctx, w.serverID, "mod-1", event(membership.RoutingKeyMemberLeft))
	if err != nil || !removed {
		t.Fatalf("RemoveMember = %v, %v", removed, err)
	}

	w.expectRevoked(ctx, t, map[uuid.UUID][]string{
		w.general: {"mod-1"},
		w.staff:   {"mod-1"},
	}, membership.RoutingKeyMemberLeft)
}

func TestLeave_NotAMember_PublishesNothing(t *testing.T) {
	ctx := context.Background()
	w := seedWorld(ctx, t)

	removed, err := w.members.RemoveMember(ctx, w.serverID, "stranger", event(membership.RoutingKeyMemberLeft))
	if err != nil || removed {
		t.Fatalf("RemoveMember = %v, %v", removed, err)
	}

	w.expectRevoked(ctx, t, nil)
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
