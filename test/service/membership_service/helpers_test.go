package membership_service_test

import (
	"context"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/outbox"
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
	removed        bool
	removeErr      error
	removeCalls    int
	removedUser    string
	gotOutboxEvent outbox.Event

	isMember    bool
	isMemberErr error

	listResult []model.Member
	listTotal  int
	listErr    error
	gotSearch  string
	gotLimit   int
	gotOffset  int
}

func (f *fakeMembers) RemoveMember(_ context.Context, _ uuid.UUID, userID string, evt outbox.Event) (bool, error) {
	f.removeCalls++
	f.removedUser = userID
	if f.removeErr != nil {
		return false, f.removeErr
	}
	if f.removed {
		f.gotOutboxEvent = evt
	}
	return f.removed, nil
}

func (f *fakeMembers) IsMember(context.Context, uuid.UUID, string) (bool, error) {
	if f.isMemberErr != nil {
		return false, f.isMemberErr
	}
	return f.isMember, nil
}

func (f *fakeMembers) ListMembers(_ context.Context, _ uuid.UUID, search string, limit, offset int) ([]model.Member, int, error) {
	f.gotSearch = search
	f.gotLimit = limit
	f.gotOffset = offset
	if f.listErr != nil {
		return nil, 0, f.listErr
	}
	return f.listResult, f.listTotal, nil
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
