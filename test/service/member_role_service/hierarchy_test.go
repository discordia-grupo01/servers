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

func rankedAdmin(top int) role.Standing {
	return role.Standing{IsMember: true, Permissions: role.PermAdministrator, TopPosition: &top}
}

// The bypass the hierarchy rule closes: an administrator ranked 3 can't give
// themselves the role at position 1 (to then ban or reorder above their rank),
// nor take it away from whoever holds it.
func TestMemberRoles_AdminCannotAssignOrRemoveRolesAtOrAboveTheirOwn(t *testing.T) {
	for _, pos := range []int{1, 3} {
		srv := ownerServer()
		rl := role.Role{ID: uuid.New(), ServerID: srv.ID, Name: "Top", Permissions: role.PermAdministrator, Position: pos}
		members := &fakeMembers{standing: rankedAdmin(3), isMember: true}
		svc := newService(&fakeServers{server: srv}, &fakeRoles{role: rl}, members)
		ctx := context.Background()

		_, err := svc.AssignRole(ctx, member_role_service.AssignRoleParams{ServerID: srv.ID, TargetUserID: testAdmin, RoleID: rl.ID, ActorUserID: testAdmin})
		if !errors.Is(err, model.ErrRoleAboveOwn) {
			t.Errorf("position %d, assign: err = %v, want ErrRoleAboveOwn", pos, err)
		}
		err = svc.RemoveRole(ctx, member_role_service.RemoveRoleParams{ServerID: srv.ID, TargetUserID: "other-admin", RoleID: rl.ID, ActorUserID: testAdmin})
		if !errors.Is(err, model.ErrRoleAboveOwn) {
			t.Errorf("position %d, remove: err = %v, want ErrRoleAboveOwn", pos, err)
		}
		if members.assignCalls != 0 || members.removeCalls != 0 {
			t.Errorf("position %d: repo called (assign=%d, remove=%d)", pos, members.assignCalls, members.removeCalls)
		}
	}
}

func TestMemberRoles_AdminAssignsAndRemovesRolesBelowTheirOwn(t *testing.T) {
	srv := ownerServer()
	rl := role.Role{ID: uuid.New(), ServerID: srv.ID, Name: "Low", Position: 4}
	svc := newService(&fakeServers{server: srv}, &fakeRoles{role: rl}, &fakeMembers{standing: rankedAdmin(3), isMember: true})
	ctx := context.Background()

	if _, err := svc.AssignRole(ctx, member_role_service.AssignRoleParams{ServerID: srv.ID, TargetUserID: testUser, RoleID: rl.ID, ActorUserID: testAdmin}); err != nil {
		t.Errorf("assign: %v", err)
	}
	if err := svc.RemoveRole(ctx, member_role_service.RemoveRoleParams{ServerID: srv.ID, TargetUserID: testUser, RoleID: rl.ID, ActorUserID: testAdmin}); err != nil {
		t.Errorf("remove: %v", err)
	}
}

func TestMemberRoles_OwnerAssignsAnyRole(t *testing.T) {
	srv := ownerServer()
	rl := role.Role{ID: uuid.New(), ServerID: srv.ID, Name: "Top", Position: 1}
	svc := newService(&fakeServers{server: srv}, &fakeRoles{role: rl}, &fakeMembers{isMember: true})

	if _, err := svc.AssignRole(context.Background(), member_role_service.AssignRoleParams{ServerID: srv.ID, TargetUserID: testUser, RoleID: rl.ID, ActorUserID: testOwner}); err != nil {
		t.Errorf("assign: %v", err)
	}
}
