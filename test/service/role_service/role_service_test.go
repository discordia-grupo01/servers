package role_service_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/role"
)

func TestCreateRole_NotAdmin_Forbidden(t *testing.T) {
	srv := ownerServer()
	svc := newService(&fakeServers{server: srv}, &fakeRoles{})

	p := validCreateParams(srv.ID)
	p.UserID = "someone-else"

	_, err := svc.CreateRole(context.Background(), p)
	if !errors.Is(err, model.ErrForbiddenNotAdmin) {
		t.Errorf("err = %v, want ErrForbiddenNotAdmin", err)
	}
}

func TestCreateRole_ServerNotFound(t *testing.T) {
	svc := newService(&fakeServers{getErr: model.ErrServerNotFound}, &fakeRoles{})

	_, err := svc.CreateRole(context.Background(), validCreateParams(uuid.New()))
	if !errors.Is(err, model.ErrServerNotFound) {
		t.Errorf("err = %v, want ErrServerNotFound", err)
	}
}

func TestCreateRole_EmptyName_BadRequest(t *testing.T) {
	srv := ownerServer()
	svc := newService(&fakeServers{server: srv}, &fakeRoles{})

	p := validCreateParams(srv.ID)
	p.Name = "   "

	_, err := svc.CreateRole(context.Background(), p)
	if !errors.Is(err, model.ErrRoleNameRequired) {
		t.Errorf("err = %v, want ErrRoleNameRequired", err)
	}
}

func TestCreateRole_NameTooLong_BadRequest(t *testing.T) {
	srv := ownerServer()
	svc := newService(&fakeServers{server: srv}, &fakeRoles{})

	p := validCreateParams(srv.ID)
	p.Name = strings.Repeat("a", 101)

	_, err := svc.CreateRole(context.Background(), p)
	if !errors.Is(err, model.ErrRoleNameTooLong) {
		t.Errorf("err = %v, want ErrRoleNameTooLong", err)
	}
}

func TestCreateRole_InvalidColorFormat_BadRequest(t *testing.T) {
	srv := ownerServer()
	svc := newService(&fakeServers{server: srv}, &fakeRoles{})

	for _, color := range []string{"FF00AA", "#FF00", "#GGGGGG", ""} {
		p := validCreateParams(srv.ID)
		p.Color = color

		_, err := svc.CreateRole(context.Background(), p)
		if !errors.Is(err, model.ErrColorInvalid) {
			t.Errorf("color %q: err = %v, want ErrColorInvalid", color, err)
		}
	}
}

func TestCreateRole_DuplicateName_Allowed(t *testing.T) {
	srv := ownerServer()
	roles := &fakeRoles{}
	svc := newService(&fakeServers{server: srv}, roles)

	p := validCreateParams(srv.ID)
	if _, err := svc.CreateRole(context.Background(), p); err != nil {
		t.Fatalf("first CreateRole: %v", err)
	}
	if _, err := svc.CreateRole(context.Background(), p); err != nil {
		t.Fatalf("second CreateRole with the same name should be allowed: %v", err)
	}
	if roles.createCalls != 2 {
		t.Errorf("repo.Create called %d times, want 2", roles.createCalls)
	}
}

func TestCreateRole_RepoError_InternalServerError(t *testing.T) {
	srv := ownerServer()
	repoErr := errors.New("db down")
	svc := newService(&fakeServers{server: srv}, &fakeRoles{createErr: repoErr})

	_, err := svc.CreateRole(context.Background(), validCreateParams(srv.ID))
	if !errors.Is(err, repoErr) {
		t.Errorf("err = %v, want the repo error propagated as-is", err)
	}
}

func TestCreateRole_PersistsThroughTheRepositoryOnce(t *testing.T) {
	srv := ownerServer()
	roles := &fakeRoles{}
	svc := newService(&fakeServers{server: srv}, roles)

	rl, err := svc.CreateRole(context.Background(), validCreateParams(srv.ID))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if roles.createCalls != 1 {
		t.Fatalf("repo.Create called %d times, want 1", roles.createCalls)
	}
	if roles.created.ID != rl.ID || roles.created.ServerID != srv.ID {
		t.Errorf("repo got role=%+v", roles.created)
	}
	// #21 CA1 (+ #48): a new role starts with VIEW_CHANNELS, SEND_MESSAGES and ADD_REACTIONS.
	if rl.Permissions != role.PermViewChannels|role.PermSendMessages|role.PermAddReactions || roles.created.Permissions != rl.Permissions {
		t.Errorf("permissions = %v (repo got %v), want VIEW_CHANNELS|SEND_MESSAGES|ADD_REACTIONS on creation",
			role.PermissionNames(rl.Permissions), role.PermissionNames(roles.created.Permissions))
	}
}

func TestListRoles_ServerNotFound(t *testing.T) {
	svc := newService(&fakeServers{getErr: model.ErrServerNotFound}, &fakeRoles{})

	_, err := svc.ListRoles(context.Background(), uuid.New(), testOwner)
	if !errors.Is(err, model.ErrServerNotFound) {
		t.Errorf("err = %v, want ErrServerNotFound", err)
	}
}

func TestListRoles_ReturnsRolesFromRepo(t *testing.T) {
	srv := ownerServer()
	roles := &fakeRoles{listRoles: []role.Role{{ID: uuid.New()}, {ID: uuid.New()}}}
	svc := newService(&fakeServers{server: srv}, roles)

	got, err := svc.ListRoles(context.Background(), srv.ID, testOwner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %d roles, want 2", len(got))
	}
}

func TestGetRole_ReturnsRoleFromRepo(t *testing.T) {
	srv := ownerServer()
	roleID := uuid.New()
	roles := &fakeRoles{getByIDRole: role.Role{ID: roleID, ServerID: srv.ID, Name: "Moderator"}}
	svc := newService(&fakeServers{server: srv}, roles)

	got, err := svc.GetRole(context.Background(), roleID, testOwner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != roleID || got.Name != "Moderator" {
		t.Errorf("got role=%+v", got)
	}
}

func TestGetRole_NotFound(t *testing.T) {
	svc := newService(&fakeServers{}, &fakeRoles{getByIDErr: model.ErrRoleNotFound})

	_, err := svc.GetRole(context.Background(), uuid.New(), testOwner)
	if !errors.Is(err, model.ErrRoleNotFound) {
		t.Errorf("err = %v, want ErrRoleNotFound", err)
	}
}

// Any member can read the server's roles, not only administrators.
func TestRoleReads_AnyMemberCanRead(t *testing.T) {
	srv := ownerServer()
	rl := role.Role{ID: uuid.New(), ServerID: srv.ID}
	members := &fakeMembers{standing: role.Standing{IsMember: true}}
	svc := newServiceWithMembers(&fakeServers{server: srv}, &fakeRoles{listRoles: []role.Role{rl}, getByIDRole: rl}, members)

	if got, err := svc.ListRoles(context.Background(), srv.ID, "member-1"); err != nil || len(got) != 1 {
		t.Errorf("ListRoles = %v, %v", got, err)
	}
	if got, err := svc.GetRole(context.Background(), rl.ID, "member-1"); err != nil || got.ID != rl.ID {
		t.Errorf("GetRole = %+v, %v", got, err)
	}
}

// Outsiders can't learn how the server is organized.
func TestRoleReads_NotAMember_Forbidden(t *testing.T) {
	srv := ownerServer()
	rl := role.Role{ID: uuid.New(), ServerID: srv.ID}
	svc := newServiceWithMembers(&fakeServers{server: srv}, &fakeRoles{listRoles: []role.Role{rl}, getByIDRole: rl}, &fakeMembers{})

	if _, err := svc.ListRoles(context.Background(), srv.ID, "stranger"); !errors.Is(err, model.ErrForbiddenNotMember) {
		t.Errorf("ListRoles: err = %v, want ErrForbiddenNotMember", err)
	}
	if _, err := svc.GetRole(context.Background(), rl.ID, "stranger"); !errors.Is(err, model.ErrForbiddenNotMember) {
		t.Errorf("GetRole: err = %v, want ErrForbiddenNotMember", err)
	}
}

func TestRoleReads_LookupErrorsPropagate(t *testing.T) {
	srv := ownerServer()
	rl := role.Role{ID: uuid.New(), ServerID: srv.ID}
	boom := errors.New("db down")

	svc := newServiceWithMembers(&fakeServers{server: srv}, &fakeRoles{getByIDRole: rl}, &fakeMembers{standingErr: boom})
	if _, err := svc.ListRoles(context.Background(), srv.ID, "member-1"); !errors.Is(err, boom) {
		t.Errorf("ListRoles standing: err = %v, want %v", err, boom)
	}
	if _, err := svc.GetRole(context.Background(), rl.ID, "member-1"); !errors.Is(err, boom) {
		t.Errorf("GetRole standing: err = %v, want %v", err, boom)
	}

	svc = newService(&fakeServers{getErr: boom}, &fakeRoles{getByIDRole: rl})
	if _, err := svc.GetRole(context.Background(), rl.ID, testOwner); !errors.Is(err, boom) {
		t.Errorf("GetRole server: err = %v, want %v", err, boom)
	}
}
