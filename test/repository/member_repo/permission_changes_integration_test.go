package member_repo_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/repository/member_repo"
)

func permissionsOf(ctx context.Context, t *testing.T, members *member_repo.MemberRepository, serverID uuid.UUID, userID string) role.Permission {
	t.Helper()
	st, err := members.MemberStanding(ctx, serverID, userID)
	if err != nil {
		t.Fatalf("MemberStanding: %v", err)
	}
	return st.Permissions
}

// #22 CA1: editing a role's permissions applies right away to every member
// holding it -- the next permission check already sees the change, in both
// directions.
func TestMemberRepo_RolePermissionChangesApplyImmediately(t *testing.T) {
	members, servers, roles, pool := newRepo(t)
	ctx := context.Background()
	srv, member := seedServerWithMember(ctx, t, servers, pool)

	rl := seedRoleWithPermissions(ctx, t, roles, srv.ID, "Helpers", role.PermSendMessages)
	if err := members.AssignRole(ctx, srv.ID, member, rl.ID); err != nil {
		t.Fatalf("AssignRole: %v", err)
	}
	if permissionsOf(ctx, t, members, srv.ID, member)&role.PermBanMembers != 0 {
		t.Fatal("member already holds BAN_MEMBERS before the edit")
	}

	rl.Permissions |= role.PermBanMembers
	if err := roles.Update(ctx, rl); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got := permissionsOf(ctx, t, members, srv.ID, member); got&role.PermBanMembers == 0 {
		t.Errorf("after granting: permissions = %v, want BAN_MEMBERS included", role.PermissionNames(got))
	}

	rl.Permissions &^= role.PermBanMembers
	if err := roles.Update(ctx, rl); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got := permissionsOf(ctx, t, members, srv.ID, member); got != role.PermSendMessages {
		t.Errorf("after revoking: permissions = %v, want only SEND_MESSAGES", role.PermissionNames(got))
	}
}

// #23 CA2: removing a role takes away its permissions, unless another role
// the member still holds grants them too.
func TestMemberRepo_RemovingARoleKeepsPermissionsGrantedByAnotherRole(t *testing.T) {
	members, servers, roles, pool := newRepo(t)
	ctx := context.Background()
	srv, member := seedServerWithMember(ctx, t, servers, pool)

	a := seedRoleWithPermissions(ctx, t, roles, srv.ID, "A", role.PermSendMessages|role.PermBanMembers)
	b := seedRoleWithPermissions(ctx, t, roles, srv.ID, "B", role.PermSendMessages)
	for _, rl := range []role.Role{a, b} {
		if err := members.AssignRole(ctx, srv.ID, member, rl.ID); err != nil {
			t.Fatalf("AssignRole: %v", err)
		}
	}

	if err := members.RemoveRole(ctx, srv.ID, member, a.ID); err != nil {
		t.Fatalf("RemoveRole(A): %v", err)
	}
	if got := permissionsOf(ctx, t, members, srv.ID, member); got != role.PermSendMessages {
		t.Errorf("without A: permissions = %v, want SEND_MESSAGES kept through B and BAN_MEMBERS gone", role.PermissionNames(got))
	}

	if err := members.RemoveRole(ctx, srv.ID, member, b.ID); err != nil {
		t.Fatalf("RemoveRole(B): %v", err)
	}
	if got := permissionsOf(ctx, t, members, srv.ID, member); got != 0 {
		t.Errorf("without A and B: permissions = %v, want none", role.PermissionNames(got))
	}
}
