package role_service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/service/role_service"
)

func TestSetDefaultRole_RoleServerMismatch_BadRequest(t *testing.T) {
	srv := ownerServer()
	otherServerRole := existingRole(uuid.New()) // belongs to a different server
	svc := newService(&fakeServers{server: srv}, &fakeRoles{getByIDRole: otherServerRole})

	err := svc.SetDefaultRole(context.Background(), role_service.SetDefaultRoleParams{
		ServerID: srv.ID, RoleID: otherServerRole.ID, UserID: testOwner,
	})
	if !errors.Is(err, model.ErrRoleServerMismatch) {
		t.Errorf("err = %v, want ErrRoleServerMismatch", err)
	}
}

func TestSetDefaultRole_NotAdmin_Forbidden(t *testing.T) {
	srv := ownerServer()
	rl := existingRole(srv.ID)
	svc := newService(&fakeServers{server: srv}, &fakeRoles{getByIDRole: rl})

	err := svc.SetDefaultRole(context.Background(), role_service.SetDefaultRoleParams{
		ServerID: srv.ID, RoleID: rl.ID, UserID: "someone-else",
	})
	if !errors.Is(err, model.ErrForbiddenNotAdmin) {
		t.Errorf("err = %v, want ErrForbiddenNotAdmin", err)
	}
}

func TestSetDefaultRole_RoleNotFound(t *testing.T) {
	srv := ownerServer()
	svc := newService(&fakeServers{server: srv}, &fakeRoles{getByIDErr: model.ErrRoleNotFound})

	err := svc.SetDefaultRole(context.Background(), role_service.SetDefaultRoleParams{
		ServerID: srv.ID, RoleID: uuid.New(), UserID: testOwner,
	})
	if !errors.Is(err, model.ErrRoleNotFound) {
		t.Errorf("err = %v, want ErrRoleNotFound", err)
	}
}

func TestSetDefaultRole_ServerNotFound(t *testing.T) {
	svc := newService(&fakeServers{getErr: model.ErrServerNotFound}, &fakeRoles{})

	err := svc.SetDefaultRole(context.Background(), role_service.SetDefaultRoleParams{
		ServerID: uuid.New(), RoleID: uuid.New(), UserID: testOwner,
	})
	if !errors.Is(err, model.ErrServerNotFound) {
		t.Errorf("err = %v, want ErrServerNotFound", err)
	}
}

func TestSetDefaultRole_Valid_CallsRepo(t *testing.T) {
	srv := ownerServer()
	rl := existingRole(srv.ID)
	servers := &fakeServers{server: srv}
	svc := newService(servers, &fakeRoles{getByIDRole: rl})

	err := svc.SetDefaultRole(context.Background(), role_service.SetDefaultRoleParams{
		ServerID: srv.ID, RoleID: rl.ID, UserID: testOwner,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
