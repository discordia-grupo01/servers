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

func validAssignParams(srv model.Server, roleID uuid.UUID) member_role_service.AssignRoleParams {
	return member_role_service.AssignRoleParams{
		ServerID: srv.ID, TargetUserID: testUser, RoleID: roleID, ActorUserID: testOwner,
	}
}

func TestAssignRole_RoleServerMismatch_BadRequest(t *testing.T) {
	srv := ownerServer()
	otherServerRole := role.Role{ID: uuid.New(), ServerID: uuid.New()} // belongs to a different server
	svc := newService(&fakeServers{server: srv}, &fakeRoles{role: otherServerRole}, &fakeMembers{isMember: true})

	_, err := svc.AssignRole(context.Background(), validAssignParams(srv, otherServerRole.ID))
	if !errors.Is(err, model.ErrRoleServerMismatch) {
		t.Errorf("err = %v, want ErrRoleServerMismatch", err)
	}
}

func TestAssignRole_MemberNotFound(t *testing.T) {
	srv := ownerServer()
	rl := role.Role{ID: uuid.New(), ServerID: srv.ID}
	svc := newService(&fakeServers{server: srv}, &fakeRoles{role: rl}, &fakeMembers{isMember: false})

	_, err := svc.AssignRole(context.Background(), validAssignParams(srv, rl.ID))
	if !errors.Is(err, model.ErrMemberNotFound) {
		t.Errorf("err = %v, want ErrMemberNotFound", err)
	}
}

func TestAssignRole_RoleNotFound(t *testing.T) {
	srv := ownerServer()
	svc := newService(&fakeServers{server: srv}, &fakeRoles{getErr: model.ErrRoleNotFound}, &fakeMembers{isMember: true})

	_, err := svc.AssignRole(context.Background(), validAssignParams(srv, uuid.New()))
	if !errors.Is(err, model.ErrRoleNotFound) {
		t.Errorf("err = %v, want ErrRoleNotFound", err)
	}
}

func TestAssignRole_AlreadyAssigned_Idempotent(t *testing.T) {
	srv := ownerServer()
	rl := role.Role{ID: uuid.New(), ServerID: srv.ID}
	members := &fakeMembers{isMember: true}
	svc := newService(&fakeServers{server: srv}, &fakeRoles{role: rl}, members)

	if _, err := svc.AssignRole(context.Background(), validAssignParams(srv, rl.ID)); err != nil {
		t.Fatalf("first AssignRole: %v", err)
	}
	if _, err := svc.AssignRole(context.Background(), validAssignParams(srv, rl.ID)); err != nil {
		t.Fatalf("second AssignRole (already assigned) should be idempotent: %v", err)
	}
	if members.assignCalls != 2 {
		t.Errorf("repo.AssignRole called %d times, want 2 (idempotency happens in the repo, not the service)", members.assignCalls)
	}
}

func TestAssignRole_ReturnsAssignedRole(t *testing.T) {
	srv := ownerServer()
	rl := role.Role{ID: uuid.New(), ServerID: srv.ID, Name: "Moderator"}
	svc := newService(&fakeServers{server: srv}, &fakeRoles{role: rl}, &fakeMembers{isMember: true})

	got, err := svc.AssignRole(context.Background(), validAssignParams(srv, rl.ID))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != rl.ID || got.Name != rl.Name {
		t.Errorf("got = %+v, want the assigned role %+v", got, rl)
	}
}

func TestAssignRole_NotOwner_Forbidden(t *testing.T) {
	srv := ownerServer()
	rl := role.Role{ID: uuid.New(), ServerID: srv.ID}
	svc := newService(&fakeServers{server: srv}, &fakeRoles{role: rl}, &fakeMembers{isMember: true})

	p := validAssignParams(srv, rl.ID)
	p.ActorUserID = "someone-else"

	_, err := svc.AssignRole(context.Background(), p)
	if !errors.Is(err, model.ErrForbiddenNotOwner) {
		t.Errorf("err = %v, want ErrForbiddenNotOwner", err)
	}
}

func TestRemoveRole_NotAssigned_NotFound(t *testing.T) {
	srv := ownerServer()
	rl := role.Role{ID: uuid.New(), ServerID: srv.ID}
	svc := newService(&fakeServers{server: srv}, &fakeRoles{role: rl}, &fakeMembers{removeErr: model.ErrMemberRoleNotFound})

	err := svc.RemoveRole(context.Background(), member_role_service.RemoveRoleParams{
		ServerID: srv.ID, TargetUserID: testUser, RoleID: rl.ID, ActorUserID: testOwner,
	})
	if !errors.Is(err, model.ErrMemberRoleNotFound) {
		t.Errorf("err = %v, want ErrMemberRoleNotFound", err)
	}
}

func TestRemoveRole_RoleServerMismatch_BadRequest(t *testing.T) {
	srv := ownerServer()
	otherServerRole := role.Role{ID: uuid.New(), ServerID: uuid.New()} // belongs to a different server
	svc := newService(&fakeServers{server: srv}, &fakeRoles{role: otherServerRole}, &fakeMembers{})

	err := svc.RemoveRole(context.Background(), member_role_service.RemoveRoleParams{
		ServerID: srv.ID, TargetUserID: testUser, RoleID: otherServerRole.ID, ActorUserID: testOwner,
	})
	if !errors.Is(err, model.ErrRoleServerMismatch) {
		t.Errorf("err = %v, want ErrRoleServerMismatch", err)
	}
}

func TestRemoveRole_RoleNotFound(t *testing.T) {
	srv := ownerServer()
	svc := newService(&fakeServers{server: srv}, &fakeRoles{getErr: model.ErrRoleNotFound}, &fakeMembers{})

	err := svc.RemoveRole(context.Background(), member_role_service.RemoveRoleParams{
		ServerID: srv.ID, TargetUserID: testUser, RoleID: uuid.New(), ActorUserID: testOwner,
	})
	if !errors.Is(err, model.ErrRoleNotFound) {
		t.Errorf("err = %v, want ErrRoleNotFound", err)
	}
}

func TestRemoveRole_NotOwner_Forbidden(t *testing.T) {
	srv := ownerServer()
	members := &fakeMembers{}
	svc := newService(&fakeServers{server: srv}, &fakeRoles{}, members)

	err := svc.RemoveRole(context.Background(), member_role_service.RemoveRoleParams{
		ServerID: srv.ID, TargetUserID: testUser, RoleID: uuid.New(), ActorUserID: "someone-else",
	})
	if !errors.Is(err, model.ErrForbiddenNotOwner) {
		t.Errorf("err = %v, want ErrForbiddenNotOwner", err)
	}
	if members.removeCalls != 0 {
		t.Error("repo.RemoveRole should not be called when the actor isn't the owner")
	}
}

func TestListMemberRoles_ServerNotFound(t *testing.T) {
	svc := newService(&fakeServers{getErr: model.ErrServerNotFound}, &fakeRoles{}, &fakeMembers{})

	_, err := svc.ListMemberRoles(context.Background(), uuid.New(), testUser)
	if !errors.Is(err, model.ErrServerNotFound) {
		t.Errorf("err = %v, want ErrServerNotFound", err)
	}
}

func TestListMemberRoles_ReturnsRolesFromRepo(t *testing.T) {
	srv := ownerServer()
	members := &fakeMembers{listRoles: []role.Role{{ID: uuid.New()}, {ID: uuid.New()}}}
	svc := newService(&fakeServers{server: srv}, &fakeRoles{}, members)

	got, err := svc.ListMemberRoles(context.Background(), srv.ID, testUser)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %d roles, want 2", len(got))
	}
}

func TestGetMemberPermissions_ServerNotFound(t *testing.T) {
	svc := newService(&fakeServers{getErr: model.ErrServerNotFound}, &fakeRoles{}, &fakeMembers{})

	_, err := svc.GetMemberPermissions(context.Background(), uuid.New(), testUser)
	if !errors.Is(err, model.ErrServerNotFound) {
		t.Errorf("err = %v, want ErrServerNotFound", err)
	}
}

func TestGetMemberPermissions_ReturnsUnionOfRolePermissions(t *testing.T) {
	srv := ownerServer()
	members := &fakeMembers{listRoles: []role.Role{
		{ID: uuid.New(), Permissions: role.PermBanMembers | role.PermKickMembers | role.PermViewChannels},
		{ID: uuid.New(), Permissions: role.PermSendMessages | role.PermViewChannels},
	}}
	svc := newService(&fakeServers{server: srv}, &fakeRoles{}, members)

	got, err := svc.GetMemberPermissions(context.Background(), srv.ID, testUser)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"BAN_MEMBERS", "KICK_MEMBERS", "SEND_MESSAGES", "VIEW_CHANNELS"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i, name := range want {
		if got[i] != name {
			t.Errorf("got %v, want %v", got, want)
			break
		}
	}
}

func TestGetMemberPermissions_NoRoles_ReturnsEmpty(t *testing.T) {
	srv := ownerServer()
	svc := newService(&fakeServers{server: srv}, &fakeRoles{}, &fakeMembers{})

	got, err := svc.GetMemberPermissions(context.Background(), srv.ID, testUser)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}
