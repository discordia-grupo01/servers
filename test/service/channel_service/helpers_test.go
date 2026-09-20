package channel_service_test

import (
	"context"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/service/channel_service"
)

const testOwner = "owner-1"

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

type fakeCategories struct {
	category model.Category
	getErr   error
}

func (f *fakeCategories) GetByID(_ context.Context, _ uuid.UUID) (model.Category, error) {
	if f.getErr != nil {
		return model.Category{}, f.getErr
	}
	return f.category, nil
}

type fakeChannels struct {
	createErr   error
	created     model.Channel
	createCalls int

	getByIDChannel model.Channel
	getByIDErr     error

	updateErr   error
	updatedID   uuid.UUID
	updateName  string
	updateTopic *string

	deleteErr    error
	deletedID    uuid.UUID
	deleteCalled bool

	moveErr       error
	movedID       uuid.UUID
	movedCategory *uuid.UUID
	moveCalled    bool

	listResult []model.Channel
	listErr    error

	reorderErr   error
	reorderedIDs []uuid.UUID
}

func (f *fakeChannels) Create(_ context.Context, ch model.Channel) error {
	f.createCalls++
	if f.createErr != nil {
		return f.createErr
	}
	f.created = ch
	return nil
}

func (f *fakeChannels) GetByID(_ context.Context, _ uuid.UUID) (model.Channel, error) {
	if f.getByIDErr != nil {
		return model.Channel{}, f.getByIDErr
	}
	return f.getByIDChannel, nil
}

func (f *fakeChannels) Update(_ context.Context, id uuid.UUID, name string, topic *string) error {
	f.updatedID = id
	f.updateName = name
	f.updateTopic = topic
	return f.updateErr
}

func (f *fakeChannels) Delete(_ context.Context, id uuid.UUID) error {
	f.deleteCalled = true
	f.deletedID = id
	return f.deleteErr
}

func (f *fakeChannels) MoveToCategory(_ context.Context, id uuid.UUID, categoryID *uuid.UUID) error {
	f.moveCalled = true
	f.movedID = id
	f.movedCategory = categoryID
	return f.moveErr
}

func (f *fakeChannels) ListByServerAndCategory(_ context.Context, _ uuid.UUID, _ *uuid.UUID) ([]model.Channel, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listResult, nil
}

func (f *fakeChannels) ReorderPositions(_ context.Context, orderedIDs []uuid.UUID) error {
	f.reorderedIDs = orderedIDs
	return f.reorderErr
}

func ownerServer() model.Server {
	return model.Server{ID: uuid.New(), OwnerID: testOwner, Name: "Test Server"}
}

func newService(servers *fakeServers, categories *fakeCategories, channels *fakeChannels) *channel_service.ChannelService {
	return channel_service.New(servers, categories, channels)
}
