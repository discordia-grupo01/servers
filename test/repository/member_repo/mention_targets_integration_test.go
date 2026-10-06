package member_repo_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/repository/member_repo"
	"github.com/discordia-grupo01/servers/internal/repository/role_repo"
)

type mentionWorld struct {
	srv                             model.Server
	viewer, admin, talker, roleless string
	viewRole, adminRole, talkRole   role.Role
	everyone                        uuid.UUID
}

func addMember(ctx context.Context, t *testing.T, pool *pgxpool.Pool, serverID uuid.UUID, userID string) {
	t.Helper()
	if _, err := pool.Exec(ctx,
		`INSERT INTO server_members (server_id, user_id, is_owner, joined_at) VALUES ($1, $2, FALSE, $3)`,
		serverID, userID, time.Now().UTC(),
	); err != nil {
		t.Fatalf("add member %s: %v", userID, err)
	}
}

func roleWith(ctx context.Context, t *testing.T, roles *role_repo.RoleRepository, pool *pgxpool.Pool, serverID uuid.UUID, name string, perms role.Permission) role.Role {
	t.Helper()
	rl := seedRole(ctx, t, roles, serverID, name)
	if _, err := pool.Exec(ctx, `UPDATE roles SET permissions = $1 WHERE id = $2`, perms, rl.ID); err != nil {
		t.Fatalf("set permissions of %s: %v", name, err)
	}
	rl.Permissions = perms
	return rl
}

func seedMentionWorld(ctx context.Context, t *testing.T, members *member_repo.MemberRepository, roles *role_repo.RoleRepository, pool *pgxpool.Pool, srv model.Server) mentionWorld {
	t.Helper()
	w := mentionWorld{srv: srv, viewer: "viewer", admin: "admin", talker: "talker", roleless: "roleless"}
	for _, id := range []string{w.viewer, w.admin, w.talker, w.roleless} {
		addMember(ctx, t, pool, srv.ID, id)
	}
	w.viewRole = roleWith(ctx, t, roles, pool, srv.ID, "Viewers", role.PermViewChannels)
	w.adminRole = roleWith(ctx, t, roles, pool, srv.ID, "Admins", role.PermAdministrator)
	w.talkRole = roleWith(ctx, t, roles, pool, srv.ID, "Talkers", role.PermSendMessages)
	for user, rl := range map[string]role.Role{w.viewer: w.viewRole, w.admin: w.adminRole, w.talker: w.talkRole} {
		if err := members.AssignRole(ctx, srv.ID, user, rl.ID); err != nil {
			t.Fatalf("assign %s: %v", rl.Name, err)
		}
	}
	if _, err := pool.Exec(ctx, `DELETE FROM member_roles WHERE server_id = $1 AND user_id = $2`, srv.ID, srv.OwnerID); err != nil {
		t.Fatalf("strip owner roles: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT id FROM roles WHERE server_id = $1 AND is_everyone`, srv.ID).Scan(&w.everyone); err != nil {
		t.Fatalf("find @everyone: %v", err)
	}
	return w
}

func sorted(ids ...string) []string {
	slices.Sort(ids)
	return ids
}

func TestMemberRepo_FilterMembers_KeepsOnlyMembers(t *testing.T) {
	members, servers, _, pool := newRepo(t)
	ctx := context.Background()
	srv, member := seedServerWithMember(ctx, t, servers, pool)
	_, otherMember := seedServerWithMember(ctx, t, servers, pool)

	got, err := members.FilterMembers(ctx, srv.ID, []string{"stranger", member, srv.OwnerID, otherMember})
	if err != nil {
		t.Fatalf("FilterMembers: %v", err)
	}
	if want := sorted(member, srv.OwnerID); !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestMemberRepo_FilterMembers_NoneMatch_ReturnsEmpty(t *testing.T) {
	members, servers, _, pool := newRepo(t)
	ctx := context.Background()
	srv, _ := seedServerWithMember(ctx, t, servers, pool)

	got, err := members.FilterMembers(ctx, srv.ID, []string{"nobody"})
	if err != nil || len(got) != 0 {
		t.Errorf("got %v, %v; want empty, nil", got, err)
	}
}

func TestMemberRepo_MemberIDsWithRoles_UnionWithoutDuplicates(t *testing.T) {
	members, servers, roles, pool := newRepo(t)
	ctx := context.Background()
	srv, _ := seedServerWithMember(ctx, t, servers, pool)
	w := seedMentionWorld(ctx, t, members, roles, pool, srv)
	// viewer also holds the talkers role: must appear once.
	if err := members.AssignRole(ctx, srv.ID, w.viewer, w.talkRole.ID); err != nil {
		t.Fatalf("assign: %v", err)
	}

	got, err := members.MemberIDsWithRoles(ctx, srv.ID, []uuid.UUID{w.viewRole.ID, w.talkRole.ID})
	if err != nil {
		t.Fatalf("MemberIDsWithRoles: %v", err)
	}
	if want := sorted(w.viewer, w.talker); !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestMemberRepo_ViewersAmong_FollowsTheViewChannelsRule(t *testing.T) {
	members, servers, roles, pool := newRepo(t)
	ctx := context.Background()
	srv, _ := seedServerWithMember(ctx, t, servers, pool)
	w := seedMentionWorld(ctx, t, members, roles, pool, srv)

	got, err := members.ViewersAmong(ctx, srv.ID, srv.OwnerID,
		[]string{w.viewer, w.admin, w.talker, w.roleless, srv.OwnerID, "stranger"})
	if err != nil {
		t.Fatalf("ViewersAmong: %v", err)
	}
	if want := sorted(w.viewer, w.admin, srv.OwnerID); !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestMemberRepo_ViewersAmong_OnlyLooksAtTheGivenUsers(t *testing.T) {
	members, servers, roles, pool := newRepo(t)
	ctx := context.Background()
	srv, _ := seedServerWithMember(ctx, t, servers, pool)
	w := seedMentionWorld(ctx, t, members, roles, pool, srv)

	got, err := members.ViewersAmong(ctx, srv.ID, srv.OwnerID, []string{w.viewer})
	if err != nil {
		t.Fatalf("ViewersAmong: %v", err)
	}
	if !slices.Equal(got, []string{w.viewer}) {
		t.Errorf("got %v, want [%s]", got, w.viewer)
	}
}

func TestMemberRepo_AllViewers_EveryMemberWhoCanSee_OnlyOfThatServer(t *testing.T) {
	members, servers, roles, pool := newRepo(t)
	ctx := context.Background()
	srv, _ := seedServerWithMember(ctx, t, servers, pool)
	w := seedMentionWorld(ctx, t, members, roles, pool, srv)
	other, otherMember := seedServerWithMember(ctx, t, servers, pool)
	otherAdmins := roleWith(ctx, t, roles, pool, other.ID, "Admins", role.PermAdministrator)
	if err := members.AssignRole(ctx, other.ID, otherMember, otherAdmins.ID); err != nil {
		t.Fatalf("assign: %v", err)
	}

	got, err := members.AllViewers(ctx, srv.ID, srv.OwnerID)
	if err != nil {
		t.Fatalf("AllViewers: %v", err)
	}
	if want := sorted(w.viewer, w.admin, srv.OwnerID); !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// @everyone carries VIEW_CHANNELS by default, so in a real server every member
// who holds it is a viewer.
func TestMemberRepo_AllViewers_EveryoneRoleGrantsView(t *testing.T) {
	members, servers, roles, pool := newRepo(t)
	ctx := context.Background()
	srv, _ := seedServerWithMember(ctx, t, servers, pool)
	w := seedMentionWorld(ctx, t, members, roles, pool, srv)
	if err := members.AssignRole(ctx, srv.ID, w.roleless, w.everyone); err != nil {
		t.Fatalf("assign @everyone: %v", err)
	}

	got, err := members.AllViewers(ctx, srv.ID, srv.OwnerID)
	if err != nil {
		t.Fatalf("AllViewers: %v", err)
	}
	if !slices.Contains(got, w.roleless) {
		t.Errorf("got %v, want it to include %s (holds @everyone)", got, w.roleless)
	}
}

func TestRoleRepo_FilterMentionable_DropsForeignRolesAndEveryone(t *testing.T) {
	members, servers, roles, pool := newRepo(t)
	ctx := context.Background()
	srv, _ := seedServerWithMember(ctx, t, servers, pool)
	w := seedMentionWorld(ctx, t, members, roles, pool, srv)
	other, _ := seedServerWithMember(ctx, t, servers, pool)
	foreign := seedRole(ctx, t, roles, other.ID, "Foreign")

	got, err := roles.FilterMentionable(ctx, srv.ID,
		[]uuid.UUID{w.talkRole.ID, w.viewRole.ID, w.everyone, foreign.ID, uuid.New()})
	if err != nil {
		t.Fatalf("FilterMentionable: %v", err)
	}
	if want := []uuid.UUID{w.viewRole.ID, w.talkRole.ID}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
