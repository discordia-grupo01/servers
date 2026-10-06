package channel_service_test

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/channelperm"
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

	reorderErr   error
	reorderedIDs []uuid.UUID
	gotHidden    map[uuid.UUID]bool
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

// ReorderPositions validates and merges like the real repository: listResult
// is the bucket's current order, and reorderedIDs records the full order
// written (nil if nothing was).
func (f *fakeChannels) ReorderPositions(_ context.Context, _ uuid.UUID, _ *uuid.UUID, requested []uuid.UUID, hidden map[uuid.UUID]bool) error {
	f.gotHidden = hidden
	if f.reorderErr != nil {
		return f.reorderErr
	}
	current := make([]uuid.UUID, 0, len(f.listResult))
	for _, ch := range f.listResult {
		current = append(current, ch.ID)
	}
	merged, ok := channelperm.MergeVisibleOrder(current, hidden, requested)
	if !ok {
		return model.ErrChannelReorderInvalid
	}
	f.reorderedIDs = merged
	return nil
}

func ownerServer() model.Server {
	return model.Server{ID: uuid.New(), OwnerID: testOwner, Name: "Test Server"}
}

func newService(servers *fakeServers, categories *fakeCategories, channels *fakeChannels) *channel_service.ChannelService {
	return newServiceWithStandings(servers, categories, channels, &fakeStandings{})
}

func newServiceWithStandings(servers *fakeServers, categories *fakeCategories, channels *fakeChannels, standings *fakeStandings) *channel_service.ChannelService {
	return newServiceWithVisibility(servers, categories, channels, standings, &fakeVisibility{})
}

func newServiceWithVisibility(servers *fakeServers, categories *fakeCategories, channels *fakeChannels, standings *fakeStandings, visibility *fakeVisibility) *channel_service.ChannelService {
	return channel_service.New(servers, categories, channels, standings, visibility)
}

// fakeVisibility returns the same channel overrides for every user; the zero
// value means no overrides at all.
type fakeVisibility struct {
	layers map[uuid.UUID]channelperm.Layers
	err    error
	calls  int
}

func (f *fakeVisibility) MemberStandings(context.Context, string, []uuid.UUID) (map[uuid.UUID]role.Standing, error) {
	return nil, errors.New("channel_service must not need MemberStandings")
}

func (f *fakeVisibility) ChannelLayers(context.Context, string, []uuid.UUID) (map[uuid.UUID]channelperm.Layers, error) {
	f.calls++
	return f.layers, f.err
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
