package role_service_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/service/role_service"
)

func existingRole(serverID uuid.UUID) role.Role {
	return role.Role{ID: uuid.New(), ServerID: serverID, Name: "Moderator", Color: "#FF00AA", Permissions: role.PermSendMessages, Position: 2}
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

func TestUpdateRolePermissions_NotAdmin_Forbidden(t *testing.T) {
	srv := ownerServer()
	rl := existingRole(srv.ID)
	svc := newService(&fakeServers{server: srv}, &fakeRoles{getByIDRole: rl})

	_, err := svc.UpdateRolePermissions(context.Background(), role_service.UpdateRolePermissionsParams{
		RoleID: rl.ID, UserID: "someone-else", Permissions: []string{"SEND_MESSAGES"},
	})
	if !errors.Is(err, model.ErrForbiddenNotAdmin) {
		t.Errorf("err = %v, want ErrForbiddenNotAdmin", err)
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

func TestUpdateRole_NameAtMaxLength_Allowed(t *testing.T) {
	srv := ownerServer()
	rl := existingRole(srv.ID)
	svc := newService(&fakeServers{server: srv}, &fakeRoles{getByIDRole: rl})
	name := strings.Repeat("a", role.NameMaxLen)

	if _, err := svc.UpdateRolePermissions(context.Background(), role_service.UpdateRolePermissionsParams{
		RoleID: rl.ID, UserID: testOwner, Name: &name,
	}); err != nil {
		t.Errorf("a name of exactly %d characters must be accepted: %v", role.NameMaxLen, err)
	}
}

// @everyone keeps its automatic name and color; only its permissions can change.
func TestUpdateRole_Everyone_OnlyPermissionsCanChange(t *testing.T) {
	name, color := "Todos", "#00FF00"
	cases := map[string]struct {
		params  role_service.UpdateRolePermissionsParams
		wantErr error
	}{
		"rename":             {params: role_service.UpdateRolePermissionsParams{Name: &name}, wantErr: model.ErrEveryoneRoleIsAutomatic},
		"recolor":            {params: role_service.UpdateRolePermissionsParams{Color: &color}, wantErr: model.ErrEveryoneRoleIsAutomatic},
		"change permissions": {params: role_service.UpdateRolePermissionsParams{Permissions: []string{"SEND_MESSAGES"}}},
	}
	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			srv := ownerServer()
			everyone := existingRole(srv.ID)
			everyone.IsEveryone = true
			svc := newService(&fakeServers{server: srv}, &fakeRoles{getByIDRole: everyone})

			p := tc.params
			p.RoleID, p.UserID = everyone.ID, testOwner
			_, err := svc.UpdateRolePermissions(context.Background(), p)
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}
