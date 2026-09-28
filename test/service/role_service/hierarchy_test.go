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

// rankedAdmin is a non-owner administrator whose highest role is at
// position top.
func rankedAdmin(top int) role.Standing {
	return role.Standing{IsMember: true, Permissions: role.PermAdministrator, TopPosition: &top}
}

// A non-owner administrator can't edit, delete or make default a role at or
// above their own highest role: otherwise they could take over roles that
// outrank them and bypass #25 CA2 / #56 CA2.
func TestRoleManagement_AdminCannotActOnRolesAtOrAboveTheirOwn(t *testing.T) {
	for _, pos := range []int{1, 3} { // above and equal to the admin's top (3)
		srv := ownerServer()
		rl := existingRole(srv.ID)
		rl.Position = pos
		roles := &fakeRoles{getByIDRole: rl}
		svc := newServiceWithMembers(&fakeServers{server: srv}, roles, &fakeMembers{standing: rankedAdmin(3)})
		ctx := context.Background()

		name := "Renamed"
		if _, err := svc.UpdateRolePermissions(ctx, role_service.UpdateRolePermissionsParams{RoleID: rl.ID, UserID: testAdmin, Name: &name}); !errors.Is(err, model.ErrRoleAboveOwn) {
			t.Errorf("position %d, update: err = %v, want ErrRoleAboveOwn", pos, err)
		}
		if err := svc.DeleteRole(ctx, role_service.DeleteRoleParams{RoleID: rl.ID, UserID: testAdmin}); !errors.Is(err, model.ErrRoleAboveOwn) {
			t.Errorf("position %d, delete: err = %v, want ErrRoleAboveOwn", pos, err)
		}
		if err := svc.SetDefaultRole(ctx, role_service.SetDefaultRoleParams{ServerID: srv.ID, RoleID: rl.ID, UserID: testAdmin}); !errors.Is(err, model.ErrRoleAboveOwn) {
			t.Errorf("position %d, set default: err = %v, want ErrRoleAboveOwn", pos, err)
		}
		if roles.updated.ID != uuid.Nil || roles.deleteCalled {
			t.Errorf("position %d: the role was written (updated=%v, deleted=%v)", pos, roles.updated.ID, roles.deleteCalled)
		}
	}
}

func TestRoleManagement_AdminActsOnRolesBelowTheirOwn(t *testing.T) {
	srv := ownerServer()
	rl := existingRole(srv.ID)
	rl.Position = 4
	svc := newServiceWithMembers(&fakeServers{server: srv}, &fakeRoles{getByIDRole: rl}, &fakeMembers{standing: rankedAdmin(3)})
	ctx := context.Background()

	name := "Renamed"
	if _, err := svc.UpdateRolePermissions(ctx, role_service.UpdateRolePermissionsParams{RoleID: rl.ID, UserID: testAdmin, Name: &name}); err != nil {
		t.Errorf("update: %v", err)
	}
	if err := svc.SetDefaultRole(ctx, role_service.SetDefaultRoleParams{ServerID: srv.ID, RoleID: rl.ID, UserID: testAdmin}); err != nil {
		t.Errorf("set default: %v", err)
	}
	if err := svc.DeleteRole(ctx, role_service.DeleteRoleParams{RoleID: rl.ID, UserID: testAdmin}); err != nil {
		t.Errorf("delete: %v", err)
	}
}

// The owner is above every role.
func TestRoleManagement_OwnerActsOnAnyRole(t *testing.T) {
	srv := ownerServer()
	rl := existingRole(srv.ID)
	rl.Position = 1
	svc := newService(&fakeServers{server: srv}, &fakeRoles{getByIDRole: rl})
	ctx := context.Background()

	name := "Renamed"
	if _, err := svc.UpdateRolePermissions(ctx, role_service.UpdateRolePermissionsParams{RoleID: rl.ID, UserID: testOwner, Name: &name}); err != nil {
		t.Errorf("update: %v", err)
	}
	if err := svc.DeleteRole(ctx, role_service.DeleteRoleParams{RoleID: rl.ID, UserID: testOwner}); err != nil {
		t.Errorf("delete: %v", err)
	}
}

// Taking ADMINISTRATOR away from your own role keeps its specific error (#22
// CA2), even though that role is also your highest one.
func TestRoleManagement_OwnAdminRoleKeepsItsSpecificError(t *testing.T) {
	srv := ownerServer()
	rl := adminRole(srv.ID)
	rl.Position = 1
	svc := newServiceWithMembers(&fakeServers{server: srv}, &fakeRoles{getByIDRole: rl}, &fakeMembers{standing: rankedAdmin(1), hasRole: true})

	_, err := svc.UpdateRolePermissions(context.Background(), role_service.UpdateRolePermissionsParams{
		RoleID: rl.ID, UserID: testAdmin, Permissions: []string{"SEND_MESSAGES"},
	})
	if !errors.Is(err, model.ErrCannotRemoveOwnAdmin) {
		t.Fatalf("err = %v, want ErrCannotRemoveOwnAdmin", err)
	}
}
