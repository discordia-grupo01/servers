package role_service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/service/role_service"
)

func TestDeleteRole_NotAdmin_Forbidden(t *testing.T) {
	srv := ownerServer()
	rl := existingRole(srv.ID)
	svc := newService(&fakeServers{server: srv}, &fakeRoles{getByIDRole: rl})

	err := svc.DeleteRole(context.Background(), role_service.DeleteRoleParams{
		RoleID: rl.ID, UserID: "someone-else",
	})
	if !errors.Is(err, model.ErrForbiddenNotAdmin) {
		t.Errorf("err = %v, want ErrForbiddenNotAdmin", err)
	}
}

func TestDeleteRole_RoleNotFound(t *testing.T) {
	svc := newService(&fakeServers{}, &fakeRoles{getByIDErr: model.ErrRoleNotFound})

	err := svc.DeleteRole(context.Background(), role_service.DeleteRoleParams{
		RoleID: uuid.New(), UserID: testOwner,
	})
	if !errors.Is(err, model.ErrRoleNotFound) {
		t.Errorf("err = %v, want ErrRoleNotFound", err)
	}
}

func TestDeleteRole_ServerNotFound(t *testing.T) {
	rl := existingRole(uuid.New())
	svc := newService(&fakeServers{getErr: model.ErrServerNotFound}, &fakeRoles{getByIDRole: rl})

	err := svc.DeleteRole(context.Background(), role_service.DeleteRoleParams{
		RoleID: rl.ID, UserID: testOwner,
	})
	if !errors.Is(err, model.ErrServerNotFound) {
		t.Errorf("err = %v, want ErrServerNotFound", err)
	}
}

func TestDeleteRole_IsServerDefault_Conflict(t *testing.T) {
	srv := ownerServer()
	rl := existingRole(srv.ID)
	srv.DefaultRoleID = &rl.ID
	roles := &fakeRoles{getByIDRole: rl}
	svc := newService(&fakeServers{server: srv}, roles)

	err := svc.DeleteRole(context.Background(), role_service.DeleteRoleParams{
		RoleID: rl.ID, UserID: testOwner,
	})
	if !errors.Is(err, model.ErrRoleIsServerDefault) {
		t.Errorf("err = %v, want ErrRoleIsServerDefault", err)
	}
	if roles.deleteCalled {
		t.Error("repo.Delete should not be called when the role is the server's default")
	}
}

func TestDeleteRole_Valid_CallsRepo(t *testing.T) {
	srv := ownerServer()
	rl := existingRole(srv.ID)
	roles := &fakeRoles{getByIDRole: rl}
	svc := newService(&fakeServers{server: srv}, roles)

	err := svc.DeleteRole(context.Background(), role_service.DeleteRoleParams{
		RoleID: rl.ID, UserID: testOwner,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !roles.deleteCalled || roles.deletedID != rl.ID {
		t.Errorf("Delete not called with %v (called=%v, got=%v)", rl.ID, roles.deleteCalled, roles.deletedID)
	}
}
