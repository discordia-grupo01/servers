package membership_service_test

import (
	"context"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/service/membership_service"
)

const (
	testOwner  = "owner-1"
	testMember = "member-1"
)

type fakeServers struct {
	server model.Server
	getErr error
}

func (f *fakeServers) GetByID(context.Context, uuid.UUID) (model.Server, error) {
	if f.getErr != nil {
		return model.Server{}, f.getErr
	}
	return f.server, nil
}

type fakeMembers struct {
	removed     bool
	removeErr   error
	removeCalls int
	removedUser string
}

func (f *fakeMembers) RemoveMember(_ context.Context, _ uuid.UUID, userID string) (bool, error) {
	f.removeCalls++
	f.removedUser = userID
	if f.removeErr != nil {
		return false, f.removeErr
	}
	return f.removed, nil
}

// memberServer returns a server owned by testOwner (so testMember is a plain
// member, not the owner).
func memberServer() model.Server {
	return model.Server{ID: uuid.New(), OwnerID: testOwner, Name: "Test Server"}
}

func newService(servers *fakeServers, members *fakeMembers) *membership_service.MembershipService {
	return membership_service.New(servers, members)
}

func leaveParams(srv model.Server, actor string) membership_service.LeaveParams {
	return membership_service.LeaveParams{ServerID: srv.ID, ActorUserID: actor, TargetUserID: actor}
}
