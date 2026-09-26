package member_repo_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/repository/role_repo"
)

func seedRoleWithPermissions(ctx context.Context, t *testing.T, roles *role_repo.RoleRepository, serverID uuid.UUID, name string, perms role.Permission) role.Role {
	t.Helper()
	rl := seedRole(ctx, t, roles, serverID, name)
	rl.Permissions = perms
	if err := roles.Update(ctx, rl); err != nil {
		t.Fatalf("set role permissions: %v", err)
	}
	return rl
}

func TestMemberRepo_MemberStanding_UnionOfRolePermissions(t *testing.T) {
	members, servers, roles, pool := newRepo(t)
	ctx := context.Background()
	srv, member := seedServerWithMember(ctx, t, servers, pool)

	mods := seedRoleWithPermissions(ctx, t, roles, srv.ID, "Moderator", role.PermBanMembers)
	admins := seedRoleWithPermissions(ctx, t, roles, srv.ID, "Admin", role.PermAdministrator|role.PermSendMessages)
	for _, rl := range []role.Role{mods, admins} {
		if err := members.AssignRole(ctx, srv.ID, member, rl.ID); err != nil {
			t.Fatalf("AssignRole: %v", err)
		}
	}

	st, err := members.MemberStanding(ctx, srv.ID, member)
	if err != nil {
		t.Fatalf("MemberStanding: %v", err)
	}
	want := role.PermBanMembers | role.PermAdministrator | role.PermSendMessages
	if !st.IsMember || st.IsOwner || st.Permissions != want {
		t.Errorf("standing = %+v, want member with %v", st, role.PermissionNames(want))
	}
	if !st.IsAdmin() {
		t.Error("a member holding ADMINISTRATOR must be an administrator")
	}
}

func TestMemberRepo_MemberStanding_MemberWithoutRoles(t *testing.T) {
	members, servers, _, pool := newRepo(t)
	ctx := context.Background()
	srv, member := seedServerWithMember(ctx, t, servers, pool)

	st, err := members.MemberStanding(ctx, srv.ID, member)
	if err != nil {
		t.Fatalf("MemberStanding: %v", err)
	}
	if !st.IsMember || st.Permissions != 0 {
		t.Errorf("standing = %+v, want member without permissions", st)
	}
}

func TestMemberRepo_MemberStanding_NonMember(t *testing.T) {
	members, servers, _, pool := newRepo(t)
	ctx := context.Background()
	srv, _ := seedServerWithMember(ctx, t, servers, pool)

	st, err := members.MemberStanding(ctx, srv.ID, "stranger")
	if err != nil {
		t.Fatalf("MemberStanding: %v", err)
	}
	if st.IsMember || st.Permissions != 0 {
		t.Errorf("standing = %+v, want zero standing", st)
	}
}

func TestMemberRepo_HasRole(t *testing.T) {
	members, servers, roles, pool := newRepo(t)
	ctx := context.Background()
	srv, member := seedServerWithMember(ctx, t, servers, pool)
	assigned := seedRole(ctx, t, roles, srv.ID, "Assigned")
	other := seedRole(ctx, t, roles, srv.ID, "Other")
	if err := members.AssignRole(ctx, srv.ID, member, assigned.ID); err != nil {
		t.Fatalf("AssignRole: %v", err)
	}

	if has, err := members.HasRole(ctx, srv.ID, member, assigned.ID); err != nil || !has {
		t.Errorf("HasRole(assigned) = %v, %v; want true, nil", has, err)
	}
	if has, err := members.HasRole(ctx, srv.ID, member, other.ID); err != nil || has {
		t.Errorf("HasRole(other) = %v, %v; want false, nil", has, err)
	}
	if has, err := members.HasRole(ctx, srv.ID, "stranger", assigned.ID); err != nil || has {
		t.Errorf("HasRole(stranger) = %v, %v; want false, nil", has, err)
	}
}
