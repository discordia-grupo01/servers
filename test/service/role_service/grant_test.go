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

// rankedManager holds MANAGE_ROLES (not ADMINISTRATOR) and SEND_MESSAGES, with
// their highest role at position top.
func rankedManager(top int) role.Standing {
	return role.Standing{IsMember: true, Permissions: role.PermManageRoles | role.PermSendMessages, TopPosition: &top}
}

// The escalation: a manager edits a role below theirs to carry ADMINISTRATOR
// (or any permission they lack). Rejected, and nothing is written.
func TestUpdateRolePermissions_CannotGrantPermissionsNotHeld(t *testing.T) {
	for _, perms := range [][]string{{"ADMINISTRATOR"}, {"SEND_MESSAGES", "BAN_MEMBERS"}} {
		srv := ownerServer()
		rl := existingRole(srv.ID) // SEND_MESSAGES, position 2
		roles := &fakeRoles{getByIDRole: rl}
		svc := newServiceWithMembers(&fakeServers{server: srv}, roles, &fakeMembers{standing: rankedManager(1)})

		_, err := svc.UpdateRolePermissions(context.Background(), role_service.UpdateRolePermissionsParams{
			RoleID: rl.ID, UserID: "manager-1", Permissions: perms,
		})
		if !errors.Is(err, model.ErrPermissionNotHeld) {
			t.Errorf("%v: err = %v, want ErrPermissionNotHeld", perms, err)
		}
		if roles.updated.ID != uuid.Nil {
			t.Errorf("%v: the role was written", perms)
		}
	}
}

// Granting what they hold works, and so does keeping or removing permissions
// the role already had even if the manager lacks them: only additions count.
func TestUpdateRolePermissions_GrantsHeldAndKeepsOrRemovesExisting(t *testing.T) {
	srv := ownerServer()
	rl := existingRole(srv.ID)
	rl.Permissions = role.PermSendMessages | role.PermBanMembers // BAN_MEMBERS: not held by the manager
	cases := map[string][]string{
		"adds a held permission, keeps BAN_MEMBERS": {"SEND_MESSAGES", "MANAGE_ROLES", "BAN_MEMBERS"},
		"removes BAN_MEMBERS":                       {"SEND_MESSAGES"},
		"removes everything":                        {},
	}
	for name, perms := range cases {
		roles := &fakeRoles{getByIDRole: rl}
		svc := newServiceWithMembers(&fakeServers{server: srv}, roles, &fakeMembers{standing: rankedManager(1)})

		if _, err := svc.UpdateRolePermissions(context.Background(), role_service.UpdateRolePermissionsParams{
			RoleID: rl.ID, UserID: "manager-1", Permissions: perms,
		}); err != nil {
			t.Errorf("%s: unexpected error: %v", name, err)
		}
	}
}

// Administrators and the owner hold every permission, so they grant anything.
func TestUpdateRolePermissions_AdminAndOwnerGrantAnything(t *testing.T) {
	srv := ownerServer()
	rl := existingRole(srv.ID)

	admin := newServiceWithMembers(&fakeServers{server: srv}, &fakeRoles{getByIDRole: rl}, &fakeMembers{standing: rankedAdmin(1)})
	if _, err := admin.UpdateRolePermissions(context.Background(), role_service.UpdateRolePermissionsParams{
		RoleID: rl.ID, UserID: testAdmin, Permissions: []string{"ADMINISTRATOR", "BAN_MEMBERS"},
	}); err != nil {
		t.Errorf("admin: %v", err)
	}

	owner := newService(&fakeServers{server: srv}, &fakeRoles{getByIDRole: rl})
	if _, err := owner.UpdateRolePermissions(context.Background(), role_service.UpdateRolePermissionsParams{
		RoleID: rl.ID, UserID: testOwner, Permissions: []string{"ADMINISTRATOR"},
	}); err != nil {
		t.Errorf("owner: %v", err)
	}
}
