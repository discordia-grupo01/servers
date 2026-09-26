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

func adminRole(serverID uuid.UUID) role.Role {
	return role.Role{ID: uuid.New(), ServerID: serverID, Name: "Admin", Color: "#FF0000", Permissions: role.PermAdministrator | role.PermSendMessages}
}

// Bug #22: a non-owner holding ADMINISTRATOR administers roles.
func TestAdministrator_CanManageRoles(t *testing.T) {
	srv := ownerServer()
	rl := existingRole(srv.ID)
	svc := newServiceWithMembers(&fakeServers{server: srv}, &fakeRoles{getByIDRole: rl}, &fakeMembers{standing: adminStanding()})
	ctx := context.Background()

	if _, err := svc.CreateRole(ctx, role_service.CreateRoleParams{ServerID: srv.ID, Name: "Mods", Color: "#00FF00", UserID: testAdmin}); err != nil {
		t.Errorf("CreateRole: %v", err)
	}
	if _, err := svc.UpdateRolePermissions(ctx, role_service.UpdateRolePermissionsParams{RoleID: rl.ID, UserID: testAdmin, Permissions: []string{"BAN_MEMBERS"}}); err != nil {
		t.Errorf("UpdateRolePermissions: %v", err)
	}
	if err := svc.SetDefaultRole(ctx, role_service.SetDefaultRoleParams{ServerID: srv.ID, RoleID: rl.ID, UserID: testAdmin}); err != nil {
		t.Errorf("SetDefaultRole: %v", err)
	}
	if err := svc.DeleteRole(ctx, role_service.DeleteRoleParams{RoleID: rl.ID, UserID: testAdmin}); err != nil {
		t.Errorf("DeleteRole: %v", err)
	}
}

// Granular permissions alone don't make someone an administrator.
func TestManageRolesWithoutAdministrator_Forbidden(t *testing.T) {
	srv := ownerServer()
	members := &fakeMembers{standing: role.Standing{IsMember: true, Permissions: role.PermManageRoles}}
	svc := newServiceWithMembers(&fakeServers{server: srv}, &fakeRoles{}, members)

	_, err := svc.CreateRole(context.Background(), role_service.CreateRoleParams{ServerID: srv.ID, Name: "Mods", Color: "#00FF00", UserID: "member-1"})
	if !errors.Is(err, model.ErrForbiddenNotAdmin) {
		t.Errorf("err = %v, want ErrForbiddenNotAdmin", err)
	}
}

func TestManageRoles_StandingLookupError_Propagates(t *testing.T) {
	srv := ownerServer()
	boom := errors.New("db down")
	svc := newServiceWithMembers(&fakeServers{server: srv}, &fakeRoles{}, &fakeMembers{standingErr: boom})

	_, err := svc.CreateRole(context.Background(), role_service.CreateRoleParams{ServerID: srv.ID, Name: "Mods", Color: "#00FF00", UserID: "member-1"})
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want %v", err, boom)
	}
}

// CA2 of #22: an administrator can't remove ADMINISTRATOR from their own role.
func TestUpdateRolePermissions_AdminRemovesOwnAdministrator_Forbidden(t *testing.T) {
	srv := ownerServer()
	rl := adminRole(srv.ID)
	roles := &fakeRoles{getByIDRole: rl}
	svc := newServiceWithMembers(&fakeServers{server: srv}, roles, &fakeMembers{standing: adminStanding(), hasRole: true})

	newName := "Renamed"
	_, err := svc.UpdateRolePermissions(context.Background(), role_service.UpdateRolePermissionsParams{
		RoleID: rl.ID, UserID: testAdmin, Permissions: []string{"SEND_MESSAGES"}, Name: &newName,
	})
	if !errors.Is(err, model.ErrCannotRemoveOwnAdmin) {
		t.Fatalf("err = %v, want ErrCannotRemoveOwnAdmin", err)
	}
	if roles.updated.ID != uuid.Nil {
		t.Errorf("role must not be persisted (not even the rename), got %+v", roles.updated)
	}
}

func TestUpdateRolePermissions_AdminRemovesAdministratorFromRoleTheyDontHold_Allowed(t *testing.T) {
	srv := ownerServer()
	rl := adminRole(srv.ID)
	roles := &fakeRoles{getByIDRole: rl}
	svc := newServiceWithMembers(&fakeServers{server: srv}, roles, &fakeMembers{standing: adminStanding(), hasRole: false})

	got, err := svc.UpdateRolePermissions(context.Background(), role_service.UpdateRolePermissionsParams{
		RoleID: rl.ID, UserID: testAdmin, Permissions: []string{"SEND_MESSAGES"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Permissions != role.PermSendMessages {
		t.Errorf("permissions = %v", role.PermissionNames(got.Permissions))
	}
}

func TestUpdateRolePermissions_AdminKeepsAdministratorOnOwnRole_NoHasRoleLookup(t *testing.T) {
	srv := ownerServer()
	rl := adminRole(srv.ID)
	members := &fakeMembers{standing: adminStanding(), hasRole: true}
	svc := newServiceWithMembers(&fakeServers{server: srv}, &fakeRoles{getByIDRole: rl}, members)

	_, err := svc.UpdateRolePermissions(context.Background(), role_service.UpdateRolePermissionsParams{
		RoleID: rl.ID, UserID: testAdmin, Permissions: []string{"ADMINISTRATOR", "BAN_MEMBERS"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if members.hasRoleCalls != 0 {
		t.Errorf("HasRole calls = %d, want 0 when ADMINISTRATOR is kept", members.hasRoleCalls)
	}
}

func TestUpdateRolePermissions_OwnerRemovesAdministratorFromOwnRole_Allowed(t *testing.T) {
	srv := ownerServer()
	rl := adminRole(srv.ID)
	members := &fakeMembers{hasRole: true}
	svc := newServiceWithMembers(&fakeServers{server: srv}, &fakeRoles{getByIDRole: rl}, members)

	if _, err := svc.UpdateRolePermissions(context.Background(), role_service.UpdateRolePermissionsParams{
		RoleID: rl.ID, UserID: testOwner, Permissions: []string{"SEND_MESSAGES"},
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if members.hasRoleCalls != 0 {
		t.Errorf("HasRole calls = %d, want 0 for the owner", members.hasRoleCalls)
	}
}

func TestUpdateRolePermissions_HasRoleError_Propagates(t *testing.T) {
	srv := ownerServer()
	rl := adminRole(srv.ID)
	boom := errors.New("db down")
	svc := newServiceWithMembers(&fakeServers{server: srv}, &fakeRoles{getByIDRole: rl}, &fakeMembers{standing: adminStanding(), hasRoleErr: boom})

	_, err := svc.UpdateRolePermissions(context.Background(), role_service.UpdateRolePermissionsParams{
		RoleID: rl.ID, UserID: testAdmin, Permissions: []string{"SEND_MESSAGES"},
	})
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want %v", err, boom)
	}
}

func TestDeleteRole_AdminDeletesOwnAdminRole_Forbidden(t *testing.T) {
	srv := ownerServer()
	rl := adminRole(srv.ID)
	roles := &fakeRoles{getByIDRole: rl}
	svc := newServiceWithMembers(&fakeServers{server: srv}, roles, &fakeMembers{standing: adminStanding(), hasRole: true})

	err := svc.DeleteRole(context.Background(), role_service.DeleteRoleParams{RoleID: rl.ID, UserID: testAdmin})
	if !errors.Is(err, model.ErrCannotRemoveOwnAdmin) {
		t.Fatalf("err = %v, want ErrCannotRemoveOwnAdmin", err)
	}
	if roles.deleteCalled {
		t.Error("repo.Delete must not be called")
	}
}

func TestDeleteRole_AdminDeletesAdminRoleTheyDontHold_Allowed(t *testing.T) {
	srv := ownerServer()
	rl := adminRole(srv.ID)
	roles := &fakeRoles{getByIDRole: rl}
	svc := newServiceWithMembers(&fakeServers{server: srv}, roles, &fakeMembers{standing: adminStanding(), hasRole: false})

	if err := svc.DeleteRole(context.Background(), role_service.DeleteRoleParams{RoleID: rl.ID, UserID: testAdmin}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !roles.deleteCalled {
		t.Error("repo.Delete should be called")
	}
}
