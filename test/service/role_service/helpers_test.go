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

	deleteErr    error
	deletedID    uuid.UUID
	deleteCalled bool

	// Reorder runs check against listRoles (the "current" roles) and, if it
	// passes, records the written order.
	reorderErr     error
	reorderCalls   int
	reorderWritten []uuid.UUID
}

// Create mimics the repo: the new role goes at the bottom of the hierarchy,
// after the roles in listRoles.
func (f *fakeRoles) Create(_ context.Context, r role.Role) (role.Role, error) {
	f.createCalls++
	if f.createErr != nil {
		return role.Role{}, f.createErr
	}
	r.Position = len(f.listRoles) + 1
	f.created = r
	return r, nil
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

func (f *fakeRoles) Reorder(_ context.Context, _ uuid.UUID, orderedIDs []uuid.UUID, check func([]role.Role) error) ([]role.Role, error) {
	f.reorderCalls++
	if f.reorderErr != nil {
		return nil, f.reorderErr
	}
	if err := check(f.listRoles); err != nil {
		return nil, err
	}
	f.reorderWritten = orderedIDs
	byID := make(map[uuid.UUID]role.Role, len(f.listRoles))
	for _, rl := range f.listRoles {
		byID[rl.ID] = rl
	}
	out := make([]role.Role, 0, len(orderedIDs))
	for i, id := range orderedIDs {
		rl := byID[id]
		rl.Position = i + 1
		out = append(out, rl)
	}
	return out, nil
}

func (f *fakeRoles) Delete(_ context.Context, id uuid.UUID) error {
	f.deleteCalled = true
	f.deletedID = id
	return f.deleteErr
}

func ownerServer() model.Server {
	return model.Server{ID: uuid.New(), OwnerID: testOwner, Name: "Test Server"}
}

// fakeMembers covers the actor's standing (same semantics as the other
// packages' fakeStandings) and HasRole for the "can't remove your own
// ADMINISTRATOR" rule.
type fakeMembers struct {
	standing    role.Standing
	standingErr error

	hasRole      bool
	hasRoleErr   error
	hasRoleCalls int
}

func (f *fakeMembers) MemberStanding(_ context.Context, _ uuid.UUID, _ string) (role.Standing, error) {
	if f.standingErr != nil {
		return role.Standing{}, f.standingErr
	}
	return f.standing, nil
}

func (f *fakeMembers) HasRole(_ context.Context, _ uuid.UUID, _ string, _ uuid.UUID) (bool, error) {
	f.hasRoleCalls++
	if f.hasRoleErr != nil {
		return false, f.hasRoleErr
	}
	return f.hasRole, nil
}

// adminStanding is a non-owner member holding ADMINISTRATOR through a role.
func adminStanding() role.Standing {
	return role.Standing{IsMember: true, Permissions: role.PermAdministrator}
}

const testAdmin = "admin-1"

func newService(servers *fakeServers, roles *fakeRoles) *role_service.RoleService {
	return newServiceWithMembers(servers, roles, &fakeMembers{})
}

func newServiceWithMembers(servers *fakeServers, roles *fakeRoles, members *fakeMembers) *role_service.RoleService {
	return role_service.New(servers, roles, members)
}

func validCreateParams(serverID uuid.UUID) role_service.CreateRoleParams {
	return role_service.CreateRoleParams{ServerID: serverID, Name: "Moderator", Color: "#FF00AA", UserID: testOwner}
}
