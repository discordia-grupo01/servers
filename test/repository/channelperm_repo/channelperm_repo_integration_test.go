package channelperm_repo_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/channelperm"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/repository/channelperm_repo"
)

const owner = "owner-1"

// world is a server owned by owner with one text channel, @everyone and two
// roles, and a member holding @everyone plus "Mod".
type world struct {
	serverID, channelID      uuid.UUID
	everyoneID, modID, lowID uuid.UUID
}

func seedWorld(ctx context.Context, t *testing.T, pool *pgxpool.Pool) world {
	t.Helper()
	w := world{serverID: uuid.New(), channelID: uuid.New(), everyoneID: uuid.New(), modID: uuid.New(), lowID: uuid.New()}
	exec(ctx, t, pool, `INSERT INTO servers (id, name, name_key, owner_id) VALUES ($1, 'Server', $2, $3)`, w.serverID, "s-"+w.serverID.String(), owner)
	exec(ctx, t, pool, `INSERT INTO channels (id, server_id, name, kind) VALUES ($1, $2, 'anuncios', 'text')`, w.channelID, w.serverID)
	exec(ctx, t, pool, `INSERT INTO roles (id, server_id, name, color, permissions, position, is_everyone) VALUES ($1, $2, '@everyone', '#9e9e9e', 3, 0, TRUE)`, w.everyoneID, w.serverID)
	exec(ctx, t, pool, `INSERT INTO roles (id, server_id, name, color, permissions, position) VALUES ($1, $2, 'Mod', '#000000', $3, 1)`, w.modID, w.serverID, role.PermManageRoles)
	exec(ctx, t, pool, `INSERT INTO roles (id, server_id, name, color, permissions, position) VALUES ($1, $2, 'Low', '#000000', 0, 2)`, w.lowID, w.serverID)
	for _, u := range []string{owner, "mod-1", "member-1"} {
		exec(ctx, t, pool, `INSERT INTO server_members (server_id, user_id, is_owner) VALUES ($1, $2, $3)`, w.serverID, u, u == owner)
		exec(ctx, t, pool, `INSERT INTO member_roles (server_id, user_id, role_id) VALUES ($1, $2, $3)`, w.serverID, u, w.everyoneID)
	}
	exec(ctx, t, pool, `INSERT INTO member_roles (server_id, user_id, role_id) VALUES ($1, 'mod-1', $2)`, w.serverID, w.modID)
	return w
}

func allowAll(role.Standing, role.Role) error { return nil }

func newOverride(channelID, roleID uuid.UUID, allow, deny role.Permission) channelperm.Override {
	return channelperm.Override{ChannelID: channelID, RoleID: roleID, Allow: allow, Deny: deny, UpdatedAt: time.Now().UTC().Truncate(time.Millisecond)}
}

// #57 CA1: the override is stored for that channel and role, and replaced on
// a second PUT; the role's server-wide permissions are untouched.
func TestUpsert_CreatesThenReplaces(t *testing.T) {
	pool := newPool(t)
	ctx := context.Background()
	w := seedWorld(ctx, t, pool)
	repo := channelperm_repo.NewRepository(pool)

	o, err := repo.Upsert(ctx, w.serverID, "mod-1", newOverride(w.channelID, w.everyoneID, 0, role.PermSendMessages), allowAll)
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if o.ChannelID != w.channelID || o.RoleID != w.everyoneID || o.Deny != role.PermSendMessages || o.UpdatedAt.IsZero() {
		t.Errorf("saved = %+v", o)
	}

	o, err = repo.Upsert(ctx, w.serverID, "mod-1", newOverride(w.channelID, w.everyoneID, role.PermManageMessages, role.PermViewChannels), allowAll)
	if err != nil {
		t.Fatalf("second Upsert: %v", err)
	}
	if o.Allow != role.PermManageMessages || o.Deny != role.PermViewChannels {
		t.Errorf("replaced = %+v", o)
	}
	if n := count(ctx, t, pool, `SELECT COUNT(*) FROM channel_permission_overrides`); n != 1 {
		t.Errorf("got %d rows, want 1", n)
	}
	if n := count(ctx, t, pool, `SELECT permissions FROM roles WHERE id = $1`, w.everyoneID); n != 3 {
		t.Errorf("@everyone's server-wide permissions changed to %d", n)
	}
}

// The check gets the actor's standing and the role read under the server
// lock; if it rejects, nothing is written.
func TestUpsertAndDelete_CheckGetsStandingAndRole(t *testing.T) {
	pool := newPool(t)
	ctx := context.Background()
	w := seedWorld(ctx, t, pool)
	repo := channelperm_repo.NewRepository(pool)

	var gotActor role.Standing
	var gotRole role.Role
	_, err := repo.Upsert(ctx, w.serverID, "mod-1", newOverride(w.channelID, w.lowID, 0, role.PermSendMessages), func(a role.Standing, rl role.Role) error {
		gotActor, gotRole = a, rl
		return nil
	})
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if !gotActor.IsMember || gotActor.Permissions&role.PermManageRoles == 0 || gotActor.TopPosition == nil || *gotActor.TopPosition != 1 {
		t.Errorf("actor = %+v", gotActor)
	}
	if gotRole.ID != w.lowID || gotRole.Position != 2 || gotRole.IsEveryone {
		t.Errorf("role = %+v", gotRole)
	}

	_, err = repo.Upsert(ctx, w.serverID, owner, newOverride(w.channelID, w.everyoneID, 0, role.PermSendMessages), func(a role.Standing, rl role.Role) error {
		if !a.IsOwner || !rl.IsEveryone {
			t.Errorf("owner = %+v, role = %+v", a, rl)
		}
		return model.ErrRoleAboveOwn
	})
	if !errors.Is(err, model.ErrRoleAboveOwn) {
		t.Fatalf("err = %v, want the check's error", err)
	}
	if n := count(ctx, t, pool, `SELECT COUNT(*) FROM channel_permission_overrides WHERE role_id = $1`, w.everyoneID); n != 0 {
		t.Error("an override was written although the check rejected it")
	}

	deleted, err := repo.Delete(ctx, w.serverID, "mod-1", w.channelID, w.lowID, func(role.Standing, role.Role) error { return model.ErrForbiddenNotAdmin })
	if !errors.Is(err, model.ErrForbiddenNotAdmin) || deleted {
		t.Fatalf("delete with a rejecting check: deleted=%v err=%v", deleted, err)
	}
	if n := count(ctx, t, pool, `SELECT COUNT(*) FROM channel_permission_overrides WHERE role_id = $1`, w.lowID); n != 1 {
		t.Error("the override was deleted although the check rejected it")
	}
}

func TestDelete(t *testing.T) {
	pool := newPool(t)
	ctx := context.Background()
	w := seedWorld(ctx, t, pool)
	repo := channelperm_repo.NewRepository(pool)
	exec(ctx, t, pool, insertOverride, w.channelID, w.lowID, role.PermSendMessages, 0)

	deleted, err := repo.Delete(ctx, w.serverID, "mod-1", w.channelID, w.lowID, allowAll)
	if err != nil || !deleted {
		t.Fatalf("deleted=%v err=%v", deleted, err)
	}
	deleted, err = repo.Delete(ctx, w.serverID, "mod-1", w.channelID, w.lowID, allowAll)
	if err != nil || deleted {
		t.Fatalf("second delete: deleted=%v err=%v, want false, nil", deleted, err)
	}
}

// Channel and role are validated against the server under the lock.
func TestUpsert_LookupErrors(t *testing.T) {
	pool := newPool(t)
	ctx := context.Background()
	w := seedWorld(ctx, t, pool)
	other := seedWorld(ctx, t, pool)
	repo := channelperm_repo.NewRepository(pool)

	cases := map[string]struct {
		serverID, channelID, roleID uuid.UUID
		want                        error
	}{
		"unknown server":            {uuid.New(), w.channelID, w.lowID, model.ErrServerNotFound},
		"unknown channel":           {w.serverID, uuid.New(), w.lowID, model.ErrChannelNotFound},
		"channel of another server": {w.serverID, other.channelID, w.lowID, model.ErrChannelServerMismatch},
		"unknown role":              {w.serverID, w.channelID, uuid.New(), model.ErrRoleNotFound},
		"role of another server":    {w.serverID, w.channelID, other.lowID, model.ErrRoleServerMismatch},
	}
	for name, tc := range cases {
		_, err := repo.Upsert(ctx, tc.serverID, owner, newOverride(tc.channelID, tc.roleID, role.PermSendMessages, 0), allowAll)
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", name, err, tc.want)
		}
	}
	if n := count(ctx, t, pool, `SELECT COUNT(*) FROM channel_permission_overrides`); n != 0 {
		t.Errorf("got %d rows, want 0", n)
	}
}

// Two managers setting the same override at once: both succeed, one row.
func TestUpsert_Concurrent_OneRow(t *testing.T) {
	pool := newPool(t)
	ctx := context.Background()
	w := seedWorld(ctx, t, pool)
	repo := channelperm_repo.NewRepository(pool)

	var wg sync.WaitGroup
	errs := make([]error, 6)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			deny := role.PermSendMessages
			if i%2 == 1 {
				deny = role.PermViewChannels
			}
			_, errs[i] = repo.Upsert(ctx, w.serverID, "mod-1", newOverride(w.channelID, w.lowID, 0, deny), allowAll)
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	}
	if n := count(ctx, t, pool, `SELECT COUNT(*) FROM channel_permission_overrides`); n != 1 {
		t.Errorf("got %d rows, want 1", n)
	}
}

// @everyone first, then the hierarchy; other channels are not included.
func TestListByChannel_OrderedByHierarchy(t *testing.T) {
	pool := newPool(t)
	ctx := context.Background()
	w := seedWorld(ctx, t, pool)
	repo := channelperm_repo.NewRepository(pool)
	otherChannel := uuid.New()
	exec(ctx, t, pool, `INSERT INTO channels (id, server_id, name, kind) VALUES ($1, $2, 'otro', 'text')`, otherChannel, w.serverID)

	exec(ctx, t, pool, insertOverride, w.channelID, w.lowID, 0, role.PermSendMessages)
	exec(ctx, t, pool, insertOverride, w.channelID, w.everyoneID, 0, role.PermViewChannels)
	exec(ctx, t, pool, insertOverride, w.channelID, w.modID, role.PermViewChannels, 0)
	exec(ctx, t, pool, insertOverride, otherChannel, w.lowID, role.PermSendMessages, 0)

	got, err := repo.ListByChannel(ctx, w.channelID)
	if err != nil {
		t.Fatalf("ListByChannel: %v", err)
	}
	if len(got) != 3 || got[0].RoleID != w.everyoneID || got[1].RoleID != w.modID || got[2].RoleID != w.lowID {
		t.Errorf("got %+v, want @everyone, Mod, Low", got)
	}

	empty, err := repo.ListByChannel(ctx, uuid.New())
	if err != nil || empty == nil || len(empty) != 0 {
		t.Errorf("no overrides: got %v (nil=%v), err %v; want an empty, non-nil slice", empty, empty == nil, err)
	}
}

// ForMember returns only the overrides of the roles the user holds, with
// @everyone's split out.
func TestForMember_SplitsEveryoneAndKeepsOnlyTheUsersRoles(t *testing.T) {
	pool := newPool(t)
	ctx := context.Background()
	w := seedWorld(ctx, t, pool)
	repo := channelperm_repo.NewRepository(pool)

	exec(ctx, t, pool, insertOverride, w.channelID, w.everyoneID, 0, role.PermViewChannels)
	exec(ctx, t, pool, insertOverride, w.channelID, w.modID, role.PermViewChannels, 0)
	exec(ctx, t, pool, insertOverride, w.channelID, w.lowID, 0, role.PermSendMessages) // nobody holds Low

	everyone, roles, err := repo.ForMember(ctx, w.serverID, w.channelID, "mod-1")
	if err != nil {
		t.Fatalf("ForMember: %v", err)
	}
	if everyone.RoleID != w.everyoneID || everyone.Deny != role.PermViewChannels {
		t.Errorf("@everyone = %+v", everyone)
	}
	if len(roles) != 1 || roles[0].RoleID != w.modID {
		t.Errorf("roles = %+v, want only Mod", roles)
	}

	everyone, roles, err = repo.ForMember(ctx, w.serverID, w.channelID, "member-1")
	if err != nil || everyone.RoleID != w.everyoneID || len(roles) != 0 {
		t.Errorf("member-1: everyone=%+v roles=%+v err=%v", everyone, roles, err)
	}

	everyone, roles, err = repo.ForMember(ctx, w.serverID, w.channelID, "stranger")
	if err != nil || !everyone.IsEmpty() || len(roles) != 0 {
		t.Errorf("stranger: everyone=%+v roles=%+v err=%v", everyone, roles, err)
	}

	// Combined with the pure rule: member-1 can't see the channel, mod-1 can.
	st := role.Standing{IsMember: true, Permissions: role.PermViewChannels | role.PermSendMessages}
	e, r, _ := repo.ForMember(ctx, w.serverID, w.channelID, "member-1")
	if channelperm.CanView(st, e, r) {
		t.Error("member-1 should not see the channel")
	}
	e, r, _ = repo.ForMember(ctx, w.serverID, w.channelID, "mod-1")
	if !channelperm.CanView(st, e, r) {
		t.Error("mod-1 should see the channel")
	}
}

// One query for several servers: membership, permissions and rank per
// server, like repository.MemberStanding.
func TestMemberStandings_SeveralServersAtOnce(t *testing.T) {
	pool := newPool(t)
	ctx := context.Background()
	a := seedWorld(ctx, t, pool)
	b := seedWorld(ctx, t, pool)
	exec(ctx, t, pool, `DELETE FROM member_roles WHERE server_id = $1 AND user_id = 'mod-1'`, b.serverID)
	exec(ctx, t, pool, `DELETE FROM server_members WHERE server_id = $1 AND user_id = 'mod-1'`, b.serverID)
	repo := channelperm_repo.NewRepository(pool)

	got, err := repo.MemberStandings(ctx, "mod-1", []uuid.UUID{a.serverID, b.serverID, uuid.New()})
	if err != nil {
		t.Fatalf("MemberStandings: %v", err)
	}
	inA := got[a.serverID]
	if !inA.IsMember || inA.Permissions != role.PermManageRoles|3 || inA.TopPosition == nil || *inA.TopPosition != 1 {
		t.Errorf("server A = %+v", inA)
	}
	if inB := got[b.serverID]; inB.IsMember || inB.Permissions != 0 || inB.TopPosition != nil {
		t.Errorf("server B (not a member) = %+v", inB)
	}
	if len(got) != 2 {
		t.Errorf("got %d servers, want 2 (unknown ones are absent)", len(got))
	}
}

// One query for every channel of several servers: only the overrides of the
// user's roles, grouped by channel and split into @everyone's and the rest.
func TestChannelLayers_GroupsPerChannel(t *testing.T) {
	pool := newPool(t)
	ctx := context.Background()
	a := seedWorld(ctx, t, pool)
	b := seedWorld(ctx, t, pool)
	repo := channelperm_repo.NewRepository(pool)

	exec(ctx, t, pool, insertOverride, a.channelID, a.everyoneID, 0, role.PermViewChannels)
	exec(ctx, t, pool, insertOverride, a.channelID, a.modID, role.PermViewChannels, 0)
	exec(ctx, t, pool, insertOverride, a.channelID, a.lowID, 0, role.PermSendMessages) // nobody holds Low
	exec(ctx, t, pool, insertOverride, b.channelID, b.everyoneID, 0, role.PermSendMessages)

	got, err := repo.ChannelLayers(ctx, "mod-1", []uuid.UUID{a.serverID, b.serverID})
	if err != nil {
		t.Fatalf("ChannelLayers: %v", err)
	}
	la := got[a.channelID]
	if la.Everyone.Deny != role.PermViewChannels || len(la.Roles) != 1 || la.Roles[0].RoleID != a.modID {
		t.Errorf("channel A = %+v", la)
	}
	if lb := got[b.channelID]; lb.Everyone.Deny != role.PermSendMessages || len(lb.Roles) != 0 {
		t.Errorf("channel B = %+v", lb)
	}

	only, err := repo.ChannelLayers(ctx, "member-1", []uuid.UUID{a.serverID})
	if err != nil || len(only) != 1 || len(only[a.channelID].Roles) != 0 {
		t.Errorf("member-1 in A: %+v, err %v", only, err)
	}
	none, err := repo.ChannelLayers(ctx, "stranger", []uuid.UUID{a.serverID, b.serverID})
	if err != nil || len(none) != 0 {
		t.Errorf("stranger: %+v, err %v", none, err)
	}
}
