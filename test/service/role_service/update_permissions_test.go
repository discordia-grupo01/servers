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

func existingRole(serverID uuid.UUID) role.Role {
	return role.Role{ID: uuid.New(), ServerID: serverID, Name: "Moderator", Color: "#FF00AA", Permissions: role.PermSendMessages}
}

func TestUpdateRolePermissions_UnknownPermission_BadRequest(t *testing.T) {
	srv := ownerServer()
	rl := existingRole(srv.ID)
	svc := newService(&fakeServers{server: srv}, &fakeRoles{getByIDRole: rl})

	var ipe *model.InvalidPermissionError
	_, err := svc.UpdateRolePermissions(context.Background(), role_service.UpdateRolePermissionsParams{
		RoleID: rl.ID, UserID: testOwner, Permissions: []string{"SEND_MESSAGES", "FLY_TO_MOON"},
	})
	if !errors.As(err, &ipe) || ipe.Value != "FLY_TO_MOON" {
		t.Errorf("err = %v, want InvalidPermissionError{Value: FLY_TO_MOON}", err)
	}
	if !errors.Is(err, model.ErrInvalidPermission) {
		t.Errorf("err = %v, want it to also match ErrInvalidPermission", err)
	}
}

func TestUpdateRolePermissions_NotOwner_Forbidden(t *testing.T) {
	srv := ownerServer()
	rl := existingRole(srv.ID)
	svc := newService(&fakeServers{server: srv}, &fakeRoles{getByIDRole: rl})

	_, err := svc.UpdateRolePermissions(context.Background(), role_service.UpdateRolePermissionsParams{
		RoleID: rl.ID, UserID: "someone-else", Permissions: []string{"SEND_MESSAGES"},
	})
	if !errors.Is(err, model.ErrForbiddenNotOwner) {
		t.Errorf("err = %v, want ErrForbiddenNotOwner", err)
	}
}

func TestUpdateRolePermissions_RoleNotFound(t *testing.T) {
	svc := newService(&fakeServers{}, &fakeRoles{getByIDErr: model.ErrRoleNotFound})

	_, err := svc.UpdateRolePermissions(context.Background(), role_service.UpdateRolePermissionsParams{
		RoleID: uuid.New(), UserID: testOwner, Permissions: []string{"SEND_MESSAGES"},
	})
	if !errors.Is(err, model.ErrRoleNotFound) {
		t.Errorf("err = %v, want ErrRoleNotFound", err)
	}
}

func TestUpdateRolePermissions_PartialUpdate_KeepsExistingPermissions(t *testing.T) {
	srv := ownerServer()
	rl := existingRole(srv.ID)
	roles := &fakeRoles{getByIDRole: rl}
	svc := newService(&fakeServers{server: srv}, roles)

	newName := "Senior Moderator"
	got, err := svc.UpdateRolePermissions(context.Background(), role_service.UpdateRolePermissionsParams{
		RoleID: rl.ID, UserID: testOwner, Name: &newName,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != newName {
		t.Errorf("name = %q, want %q", got.Name, newName)
	}
	if got.Permissions != role.PermSendMessages {
		t.Errorf("permissions = %d, want unchanged %d", got.Permissions, role.PermSendMessages)
	}
	if roles.updated.Name != newName || roles.updated.Permissions != role.PermSendMessages {
		t.Errorf("repo got role=%+v", roles.updated)
	}
}

func TestUpdateRolePermissions_ValidPermissions_ReplacesMask(t *testing.T) {
	srv := ownerServer()
	rl := existingRole(srv.ID)
	svc := newService(&fakeServers{server: srv}, &fakeRoles{getByIDRole: rl})

	got, err := svc.UpdateRolePermissions(context.Background(), role_service.UpdateRolePermissionsParams{
		RoleID: rl.ID, UserID: testOwner, Permissions: []string{"MANAGE_CHANNELS", "BAN_MEMBERS"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Permissions&role.PermManageChannels == 0 || got.Permissions&role.PermBanMembers == 0 {
		t.Errorf("permissions = %d, missing expected bits", got.Permissions)
	}
	if got.Permissions&role.PermSendMessages != 0 {
		t.Errorf("permissions = %d, old permission should have been replaced, not merged", got.Permissions)
	}
}
