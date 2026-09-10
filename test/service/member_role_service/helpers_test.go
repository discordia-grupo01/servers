package member_role_service_test

import (
	"context"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/service/member_role_service"
)

const (
	testOwner = "owner-1"
	testUser  = "member-1"
)

type fakeServers struct {
	server model.Server
	getErr error
}

func (f *fakeServers) GetByID(_ context.Context, _ uuid.UUID) (model.Server, error) {
	if f.getErr != nil {
		return model.Server{}, f.getErr
	}
	return f.server, nil
}

type fakeRoles struct {
	role   role.Role
	getErr error
}

func (f *fakeRoles) GetByID(_ context.Context, _ uuid.UUID) (role.Role, error) {
	if f.getErr != nil {
		return role.Role{}, f.getErr
	}
	return f.role, nil
}

type fakeMembers struct {
	isMember    bool
	isMemberErr error

	assignErr    error
	assignCalls  int
	assignedTo   string
	assignedRole uuid.UUID

	removeErr   error
	removeCalls int

	listRoles []role.Role
	listErr   error
}

func (f *fakeMembers) IsMember(_ context.Context, _ uuid.UUID, _ string) (bool, error) {
	if f.isMemberErr != nil {
		return false, f.isMemberErr
	}
	return f.isMember, nil
}

func (f *fakeMembers) AssignRole(_ context.Context, _ uuid.UUID, userID string, roleID uuid.UUID) error {
	f.assignCalls++
	f.assignedTo, f.assignedRole = userID, roleID
	return f.assignErr
}

func (f *fakeMembers) RemoveRole(_ context.Context, _ uuid.UUID, _ string, _ uuid.UUID) error {
	f.removeCalls++
	return f.removeErr
}

func (f *fakeMembers) ListRolesForMember(_ context.Context, _ uuid.UUID, _ string) ([]role.Role, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listRoles, nil
}

func ownerServer() model.Server {
	return model.Server{ID: uuid.New(), OwnerID: testOwner, Name: "Test Server"}
}

func newService(servers *fakeServers, roles *fakeRoles, members *fakeMembers) *member_role_service.MemberRoleService {
	return member_role_service.New(servers, roles, members)
}
