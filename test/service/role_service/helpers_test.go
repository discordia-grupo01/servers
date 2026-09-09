package role_service_test

import (
	"context"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/service/role_service"
)

const testOwner = "owner-1"

type fakeServers struct {
	server        model.Server
	getErr        error
	setDefaultErr error
}

func (f *fakeServers) GetByID(_ context.Context, _ uuid.UUID) (model.Server, error) {
	if f.getErr != nil {
		return model.Server{}, f.getErr
	}
	return f.server, nil
}

func (f *fakeServers) SetDefaultRole(_ context.Context, _, _ uuid.UUID) error {
	return f.setDefaultErr
}

type fakeRoles struct {
	createErr   error
	created     role.Role
	createCalls int

	getByIDRole role.Role
	getByIDErr  error

	listRoles []role.Role
	listErr   error

	updateErr error
	updated   role.Role
}

func (f *fakeRoles) Create(_ context.Context, r role.Role) error {
	f.createCalls++
	if f.createErr != nil {
		return f.createErr
	}
	f.created = r
	return nil
}

func (f *fakeRoles) GetByID(_ context.Context, _ uuid.UUID) (role.Role, error) {
	if f.getByIDErr != nil {
		return role.Role{}, f.getByIDErr
	}
	return f.getByIDRole, nil
}

func (f *fakeRoles) ListByServer(_ context.Context, _ uuid.UUID) ([]role.Role, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listRoles, nil
}

func (f *fakeRoles) Update(_ context.Context, r role.Role) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	f.updated = r
	return nil
}

func ownerServer() model.Server {
	return model.Server{ID: uuid.New(), OwnerID: testOwner, Name: "Test Server"}
}

func newService(servers *fakeServers, roles *fakeRoles) *role_service.RoleService {
	return role_service.New(servers, roles)
}

func validCreateParams(serverID uuid.UUID) role_service.CreateRoleParams {
	return role_service.CreateRoleParams{ServerID: serverID, Name: "Moderator", Color: "#FF00AA", UserID: testOwner}
}
