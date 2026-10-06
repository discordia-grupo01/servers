package channel_service_test

import (
	"context"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/outbox"
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
	createErr      error
	created        model.Channel
	createCalls    int
	gotCreateEvent outbox.Event

	getByIDChannel model.Channel
	getByIDErr     error

	updateErr   error
	updatedID   uuid.UUID
	updateName  string
	updateTopic *string

	deleteErr      error
	deletedID      uuid.UUID
	deleteCalled   bool
	gotDeleteEvent outbox.Event

	moveErr       error
	movedID       uuid.UUID
	movedCategory *uuid.UUID
	moveCalled    bool

	listResult []model.Channel
	listErr    error

	reorderErr   error
	reorderedIDs []uuid.UUID
}

func (f *fakeChannels) Create(_ context.Context, ch model.Channel, evt outbox.Event) error {
	f.createCalls++
	if f.createErr != nil {
		return f.createErr
	}
	f.created = ch
	f.gotCreateEvent = evt
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

func (f *fakeChannels) Delete(_ context.Context, id uuid.UUID, evt outbox.Event) error {
	f.deleteCalled = true
	f.deletedID = id
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.gotDeleteEvent = evt
	return nil
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
	return newServiceWithStandings(servers, categories, channels, &fakeStandings{})
}

func newServiceWithStandings(servers *fakeServers, categories *fakeCategories, channels *fakeChannels, standings *fakeStandings) *channel_service.ChannelService {
	return channel_service.New(servers, categories, channels, standings)
}

// fakeStandings returns the same standing for every non-owner caller (the
// owner never reaches it: service.ResolveStanding short-circuits them). The
// zero value makes every non-owner a non-member, i.e. not an administrator.
type fakeStandings struct {
	standing role.Standing
	err      error
}

func (f *fakeStandings) MemberStanding(_ context.Context, _ uuid.UUID, _ string) (role.Standing, error) {
	if f.err != nil {
		return role.Standing{}, f.err
	}
	return f.standing, nil
}

type fakeProfileNames struct {
	name string
	err  error
}

func (f *fakeProfileNames) GetName(_ context.Context, _ string) (string, error) {
	return f.name, f.err
}

// adminStanding is a non-owner member holding ADMINISTRATOR through a role.
func adminStanding() role.Standing {
	return role.Standing{IsMember: true, Permissions: role.PermAdministrator}
}
