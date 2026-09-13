package ownership_transfer_service_test

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/service/ownership_transfer_service"
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
	isMember    bool
	isMemberErr error
}

func (f *fakeMembers) IsMember(context.Context, uuid.UUID, string) (bool, error) {
	if f.isMemberErr != nil {
		return false, f.isMemberErr
	}
	return f.isMember, nil
}

type fakeTransfers struct {
	createErr error
	created   model.OwnershipTransfer
	createdN  int

	getResult model.OwnershipTransfer
	getErr    error

	pendingResult model.OwnershipTransfer
	pendingErr    error
	pendingCalls  int

	acceptResult model.OwnershipTransfer
	acceptErr    error
	acceptCalls  int

	rejectResult model.OwnershipTransfer
	rejectErr    error
	rejectCalls  int

	cancelResult model.OwnershipTransfer
	cancelErr    error
	cancelCalls  int
}

func (f *fakeTransfers) Create(_ context.Context, t model.OwnershipTransfer) error {
	f.createdN++
	f.created = t
	return f.createErr
}

func (f *fakeTransfers) GetByID(context.Context, uuid.UUID) (model.OwnershipTransfer, error) {
	if f.getErr != nil {
		return model.OwnershipTransfer{}, f.getErr
	}
	return f.getResult, nil
}

func (f *fakeTransfers) GetPendingByServer(context.Context, uuid.UUID) (model.OwnershipTransfer, error) {
	f.pendingCalls++
	if f.pendingErr != nil {
		return model.OwnershipTransfer{}, f.pendingErr
	}
	return f.pendingResult, nil
}

func (f *fakeTransfers) Accept(context.Context, uuid.UUID, time.Time) (model.OwnershipTransfer, error) {
	f.acceptCalls++
	if f.acceptErr != nil {
		return model.OwnershipTransfer{}, f.acceptErr
	}
	return f.acceptResult, nil
}

func (f *fakeTransfers) Reject(context.Context, uuid.UUID, time.Time) (model.OwnershipTransfer, error) {
	f.rejectCalls++
	if f.rejectErr != nil {
		return model.OwnershipTransfer{}, f.rejectErr
	}
	return f.rejectResult, nil
}

func (f *fakeTransfers) Cancel(context.Context, uuid.UUID, time.Time) (model.OwnershipTransfer, error) {
	f.cancelCalls++
	if f.cancelErr != nil {
		return model.OwnershipTransfer{}, f.cancelErr
	}
	return f.cancelResult, nil
}

func ownerServer() model.Server {
	return model.Server{ID: uuid.New(), OwnerID: testOwner, Name: "Test Server"}
}

func newService(servers *fakeServers, members *fakeMembers, transfers *fakeTransfers) *ownership_transfer_service.Service {
	return ownership_transfer_service.New(servers, members, transfers)
}
