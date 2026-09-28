package member_role_service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/service/member_role_service"
)

func rankedManager(top int) role.Standing {
	return role.Standing{IsMember: true, Permissions: role.PermManageRoles | role.PermSendMessages, TopPosition: &top}
}

// A manager can't assign (to themselves or anyone) a role below theirs that
// carries a permission they lack -- e.g. an ADMINISTRATOR role the owner left
// low in the hierarchy.
func TestAssignRole_CannotAssignRoleWithPermissionsNotHeld(t *testing.T) {
	srv := ownerServer()
	rl := role.Role{ID: uuid.New(), ServerID: srv.ID, Name: "Admins", Permissions: role.PermAdministrator, Position: 2}
	members := &fakeMembers{standing: rankedManager(1), isMember: true}
	svc := newService(&fakeServers{server: srv}, &fakeRoles{role: rl}, members)

	for _, target := range []string{"manager-1", testUser} {
		_, err := svc.AssignRole(context.Background(), member_role_service.AssignRoleParams{
			ServerID: srv.ID, TargetUserID: target, RoleID: rl.ID, ActorUserID: "manager-1",
		})
		if !errors.Is(err, model.ErrPermissionNotHeld) {
			t.Errorf("target %s: err = %v, want ErrPermissionNotHeld", target, err)
		}
	}
	if members.assignCalls != 0 {
		t.Errorf("repo AssignRole called %d times", members.assignCalls)
	}
}

func TestAssignRole_AssignsRoleWithHeldPermissions(t *testing.T) {
	srv := ownerServer()
	rl := role.Role{ID: uuid.New(), ServerID: srv.ID, Name: "Talkers", Permissions: role.PermSendMessages, Position: 2}
	members := &fakeMembers{standing: rankedManager(1), isMember: true}
	svc := newService(&fakeServers{server: srv}, &fakeRoles{role: rl}, members)

	if _, err := svc.AssignRole(context.Background(), member_role_service.AssignRoleParams{
		ServerID: srv.ID, TargetUserID: testUser, RoleID: rl.ID, ActorUserID: "manager-1",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if members.assignCalls != 1 {
		t.Errorf("repo AssignRole called %d times, want 1", members.assignCalls)
	}
}

// Unassigning never grants anything, so it isn't restricted by permissions.
func TestRemoveRole_NotRestrictedByPermissionsNotHeld(t *testing.T) {
	srv := ownerServer()
	rl := role.Role{ID: uuid.New(), ServerID: srv.ID, Name: "Banners", Permissions: role.PermBanMembers, Position: 2}
	svc := newService(&fakeServers{server: srv}, &fakeRoles{role: rl}, &fakeMembers{standing: rankedManager(1), isMember: true})

	if err := svc.RemoveRole(context.Background(), member_role_service.RemoveRoleParams{
		ServerID: srv.ID, TargetUserID: testUser, RoleID: rl.ID, ActorUserID: "manager-1",
	}); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
