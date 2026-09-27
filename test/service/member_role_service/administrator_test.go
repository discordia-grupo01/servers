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

func adminRoleIn(serverID uuid.UUID) role.Role {
	return role.Role{ID: uuid.New(), ServerID: serverID, Name: "Admin", Permissions: role.PermAdministrator, Position: 2}
}

// Bug #22: a non-owner holding ADMINISTRATOR can assign and remove roles.
func TestAdministrator_CanAssignAndRemoveRoles(t *testing.T) {
	srv := ownerServer()
	rl := role.Role{ID: uuid.New(), ServerID: srv.ID, Name: "Mods", Position: 2}
	members := &fakeMembers{isMember: true, standing: adminStanding()}
	svc := newService(&fakeServers{server: srv}, &fakeRoles{role: rl}, members)
	ctx := context.Background()

	if _, err := svc.AssignRole(ctx, member_role_service.AssignRoleParams{ServerID: srv.ID, TargetUserID: testUser, RoleID: rl.ID, ActorUserID: testAdmin}); err != nil {
		t.Errorf("AssignRole: %v", err)
	}
	if err := svc.RemoveRole(ctx, member_role_service.RemoveRoleParams{ServerID: srv.ID, TargetUserID: testUser, RoleID: rl.ID, ActorUserID: testAdmin}); err != nil {
		t.Errorf("RemoveRole: %v", err)
	}
}

// CA2 of #22 (same rule): an administrator can't unassign their own admin role.
func TestRemoveRole_AdminUnassignsOwnAdminRole_Forbidden(t *testing.T) {
	srv := ownerServer()
	rl := adminRoleIn(srv.ID)
	members := &fakeMembers{standing: adminStanding(), hasRole: true}
	svc := newService(&fakeServers{server: srv}, &fakeRoles{role: rl}, members)

	err := svc.RemoveRole(context.Background(), member_role_service.RemoveRoleParams{
		ServerID: srv.ID, TargetUserID: testAdmin, RoleID: rl.ID, ActorUserID: testAdmin,
	})
	if !errors.Is(err, model.ErrCannotRemoveOwnAdmin) {
		t.Fatalf("err = %v, want ErrCannotRemoveOwnAdmin", err)
	}
	if members.removeCalls != 0 {
		t.Errorf("RemoveRole repo calls = %d, want 0", members.removeCalls)
	}
}

func TestRemoveRole_AdminUnassignsOwnNonAdminRole_Allowed(t *testing.T) {
	srv := ownerServer()
	rl := role.Role{ID: uuid.New(), ServerID: srv.ID, Name: "VIP", Permissions: role.PermSendMessages, Position: 2}
	members := &fakeMembers{standing: adminStanding(), hasRole: true}
	svc := newService(&fakeServers{server: srv}, &fakeRoles{role: rl}, members)

	if err := svc.RemoveRole(context.Background(), member_role_service.RemoveRoleParams{
		ServerID: srv.ID, TargetUserID: testAdmin, RoleID: rl.ID, ActorUserID: testAdmin,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if members.hasRoleCalls != 0 {
		t.Errorf("HasRole calls = %d, want 0 for a role without ADMINISTRATOR", members.hasRoleCalls)
	}
}

func TestRemoveRole_AdminUnassignsSomeoneElsesAdminRole_Allowed(t *testing.T) {
	srv := ownerServer()
	rl := adminRoleIn(srv.ID)
	members := &fakeMembers{standing: adminStanding(), hasRole: true}
	svc := newService(&fakeServers{server: srv}, &fakeRoles{role: rl}, members)

	if err := svc.RemoveRole(context.Background(), member_role_service.RemoveRoleParams{
		ServerID: srv.ID, TargetUserID: testUser, RoleID: rl.ID, ActorUserID: testAdmin,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if members.removeCalls != 1 {
		t.Errorf("RemoveRole repo calls = %d, want 1", members.removeCalls)
	}
}

func TestRemoveRole_OwnerUnassignsOwnAdminRole_Allowed(t *testing.T) {
	srv := ownerServer()
	rl := adminRoleIn(srv.ID)
	members := &fakeMembers{hasRole: true}
	svc := newService(&fakeServers{server: srv}, &fakeRoles{role: rl}, members)

	if err := svc.RemoveRole(context.Background(), member_role_service.RemoveRoleParams{
		ServerID: srv.ID, TargetUserID: testOwner, RoleID: rl.ID, ActorUserID: testOwner,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRemoveRole_AdminUnassignsAdminRoleTheyDontHold_FallsThroughToNotFound(t *testing.T) {
	srv := ownerServer()
	rl := adminRoleIn(srv.ID)
	members := &fakeMembers{standing: adminStanding(), hasRole: false, removeErr: model.ErrMemberRoleNotFound}
	svc := newService(&fakeServers{server: srv}, &fakeRoles{role: rl}, members)

	err := svc.RemoveRole(context.Background(), member_role_service.RemoveRoleParams{
		ServerID: srv.ID, TargetUserID: testAdmin, RoleID: rl.ID, ActorUserID: testAdmin,
	})
	if !errors.Is(err, model.ErrMemberRoleNotFound) {
		t.Errorf("err = %v, want ErrMemberRoleNotFound", err)
	}
}

func TestRemoveRole_HasRoleError_Propagates(t *testing.T) {
	srv := ownerServer()
	rl := adminRoleIn(srv.ID)
	boom := errors.New("db down")
	svc := newService(&fakeServers{server: srv}, &fakeRoles{role: rl}, &fakeMembers{standing: adminStanding(), hasRoleErr: boom})

	err := svc.RemoveRole(context.Background(), member_role_service.RemoveRoleParams{
		ServerID: srv.ID, TargetUserID: testAdmin, RoleID: rl.ID, ActorUserID: testAdmin,
	})
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want %v", err, boom)
	}
}

func TestAssignRole_StandingLookupError_Propagates(t *testing.T) {
	srv := ownerServer()
	boom := errors.New("db down")
	svc := newService(&fakeServers{server: srv}, &fakeRoles{}, &fakeMembers{standingErr: boom})

	_, err := svc.AssignRole(context.Background(), member_role_service.AssignRoleParams{
		ServerID: srv.ID, TargetUserID: testUser, RoleID: uuid.New(), ActorUserID: testAdmin,
	})
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want %v", err, boom)
	}
}
