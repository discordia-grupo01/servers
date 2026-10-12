package role_service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/service/role_service"
)

func roleMembersFixture(actor role.Standing) (*role_service.RoleService, *fakeMembers, role.Role) {
	serverID := uuid.New()
	rl := role.Role{ID: uuid.New(), ServerID: serverID, Name: "Mod"}
	members := &fakeMembers{
		standing:         actor,
		roleMembers:      []model.Member{{UserID: "u1"}, {UserID: "u2"}},
		roleMembersTotal: 2,
	}
	svc := newServiceWithMembers(
		&fakeServers{server: model.Server{ID: serverID, OwnerID: testOwner}},
		&fakeRoles{getByIDRole: rl},
		members,
	)
	return svc, members, rl
}

func TestListRoleMembers_CA1_ListsWhoHoldsTheRole(t *testing.T) {
	svc, members, rl := roleMembersFixture(role.Standing{IsMember: true, Permissions: role.PermManageRoles})

	got, total, err := svc.ListRoleMembers(context.Background(), role_service.ListRoleMembersParams{
		RoleID: rl.ID, UserID: "mod-1", Limit: 500, Offset: -3,
	})

	if err != nil || total != 2 || len(got) != 2 {
		t.Fatalf("got %v, %d, %v", got, total, err)
	}
	if members.gotRoleMembers.serverID != rl.ServerID || members.gotRoleMembers.roleID != rl.ID {
		t.Errorf("listed %+v, want the role's server and id", members.gotRoleMembers)
	}
	if members.gotRoleMembers.limit != model.MemberListMaxLimit || members.gotRoleMembers.offset != 0 {
		t.Errorf("page = %+v, want clamped", members.gotRoleMembers)
	}
}

func TestListRoleMembers_OwnerAndAdministratorsCanWithoutTheGrant(t *testing.T) {
	svc, _, rl := roleMembersFixture(role.Standing{IsMember: true, Permissions: role.PermAdministrator})
	for _, actor := range []string{testOwner, "admin-1"} {
		if _, _, err := svc.ListRoleMembers(context.Background(), role_service.ListRoleMembersParams{RoleID: rl.ID, UserID: actor}); err != nil {
			t.Errorf("%s: %v", actor, err)
		}
	}
}

func TestListRoleMembers_RequiresManageRoles(t *testing.T) {
	svc, members, rl := roleMembersFixture(role.Standing{IsMember: true, Permissions: role.DefaultPermissions})

	_, _, err := svc.ListRoleMembers(context.Background(), role_service.ListRoleMembersParams{RoleID: rl.ID, UserID: "member-1"})

	if !errors.Is(err, model.ErrForbiddenNotAdmin) || members.roleMembersCalls != 0 {
		t.Errorf("err = %v, calls = %d", err, members.roleMembersCalls)
	}
}

func TestListRoleMembers_UnknownRoleAndNoUser(t *testing.T) {
	svc := newServiceWithMembers(&fakeServers{}, &fakeRoles{getByIDErr: model.ErrRoleNotFound}, &fakeMembers{})
	if _, _, err := svc.ListRoleMembers(context.Background(), role_service.ListRoleMembersParams{RoleID: uuid.New(), UserID: "x"}); !errors.Is(err, model.ErrRoleNotFound) {
		t.Errorf("unknown role: err = %v", err)
	}
	if _, _, err := svc.ListRoleMembers(context.Background(), role_service.ListRoleMembersParams{RoleID: uuid.New()}); !errors.Is(err, model.ErrUnauthorized) {
		t.Errorf("no user: err = %v", err)
	}
}
