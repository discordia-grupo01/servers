package service_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/channelperm"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/outbox"
	"github.com/discordia-grupo01/servers/internal/service"
)

const testOwner = "user-123"

type fakeRepoHelper struct {
	err             error
	calls           int
	gotServer       model.Server
	gotCats         []model.Category
	gotChans        []model.Channel
	gotOutboxEvents []outbox.Event

	listResult  []model.Server
	listErr     error
	listCalls   int
	gotListUser string

	getResult model.Server
	getErr    error
	getCalls  int

	iconURLsCalls int

	memberResult bool
	memberErr    error
	memberCalls  int

	updateErr   error
	updateCalls int

	deleteErr       error
	deleteCalls     int
	deleteMemberIDs []string
	deleteChannels  []uuid.UUID
	deleteEvents    []outbox.Event

	// standing is what MemberStanding (and MemberStandings, for every
	// server) returns for any non-owner caller; the zero value makes them a
	// non-member (not an administrator).
	standing    role.Standing
	standingErr error

	// layers is what ChannelLayers returns, for any user and servers.
	layers       map[uuid.UUID]channelperm.Layers
	layersErr    error
	layersCalls  int
	standingsErr error
}

func (f *fakeRepoHelper) MemberStanding(_ context.Context, _ uuid.UUID, _ string) (role.Standing, error) {
	if f.standingErr != nil {
		return role.Standing{}, f.standingErr
	}
	return f.standing, nil
}

func (f *fakeRepoHelper) MemberStandings(_ context.Context, _ string, serverIDs []uuid.UUID) (map[uuid.UUID]role.Standing, error) {
	if f.standingsErr != nil {
		return nil, f.standingsErr
	}
	out := make(map[uuid.UUID]role.Standing, len(serverIDs))
	for _, id := range serverIDs {
		out[id] = f.standing
	}
	return out, nil
}

func (f *fakeRepoHelper) ChannelLayers(context.Context, string, []uuid.UUID) (map[uuid.UUID]channelperm.Layers, error) {
	f.layersCalls++
	return f.layers, f.layersErr
}

func (f *fakeRepoHelper) CreateServer(_ context.Context, s model.Server, cats []model.Category, ch []model.Channel, events []outbox.Event) error {
	f.calls++
	if f.err != nil {
		return f.err
	}
	f.gotServer, f.gotCats, f.gotChans, f.gotOutboxEvents = s, cats, ch, events
	f.getResult = s
	return nil
}

func (f *fakeRepoHelper) ListForUser(_ context.Context, userID string) ([]model.Server, error) {
	f.listCalls++
	f.gotListUser = userID
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listResult, nil
}

func (f *fakeRepoHelper) GetByID(_ context.Context, _ uuid.UUID) (model.Server, error) {
	f.getCalls++
	if f.getErr != nil {
		return model.Server{}, f.getErr
	}
	return f.getResult, nil
}

func (f *fakeRepoHelper) IsMember(_ context.Context, _ uuid.UUID, _ string) (bool, error) {
	f.memberCalls++
	return f.memberResult, f.memberErr
}

func (f *fakeRepoHelper) IconURLs(_ context.Context, _ uuid.UUID) (*string, *string, error) {
	f.iconURLsCalls++
	if f.getErr != nil {
		return nil, nil, f.getErr
	}
	return f.getResult.IconURL, f.getResult.BannerURL, nil
}

func (f *fakeRepoHelper) UpdateServer(_ context.Context, _ uuid.UUID, name, nameKey string, iconURL, bannerURL *string) error {
	f.updateCalls++
	if f.updateErr != nil {
		return f.updateErr
	}
	f.getResult.Name, f.getResult.NameKey, f.getResult.IconURL, f.getResult.BannerURL = name, nameKey, iconURL, bannerURL
	return nil
}

func (f *fakeRepoHelper) DeleteServer(_ context.Context, _ uuid.UUID, buildEvents func([]string, []uuid.UUID) ([]outbox.Event, error)) error {
	f.deleteCalls++
	if f.deleteErr != nil {
		return f.deleteErr
	}
	events, err := buildEvents(f.deleteMemberIDs, f.deleteChannels)
	if err != nil {
		return err
	}
	f.deleteEvents = events
	return nil
}

type fakeIconsHelper struct {
	err          error
	savedID      string
	savedData    []byte
	deleteCalled bool
	deletedKeys  []string
}

func (f *fakeIconsHelper) Save(id string, data []byte) (string, error) {
	f.savedID, f.savedData = id, data
	if f.err != nil {
		return "", f.err
	}
	return "/v1/servers/" + id + "/icon?v=fakehash", nil
}

func (f *fakeIconsHelper) Delete(key string) error {
	f.deleteCalled = true
	f.deletedKeys = append(f.deletedKeys, key)
	return nil
}

func create(t *testing.T, p service.CreateServerParams) (model.Server, error, *fakeRepoHelper, *fakeIconsHelper) {
	t.Helper()
	repo, icons := &fakeRepoHelper{}, &fakeIconsHelper{}
	srv, err := service.New(repo, icons, repo, repo).CreateServer(context.Background(), p)
	return srv, err, repo, icons
}

func createRepoErr(t *testing.T, p service.CreateServerParams, repoErr error) (model.Server, error, *fakeRepoHelper, *fakeIconsHelper) {
	t.Helper()
	repo, icons := &fakeRepoHelper{err: repoErr}, &fakeIconsHelper{}
	srv, err := service.New(repo, icons, repo, repo).CreateServer(context.Background(), p)
	return srv, err, repo, icons
}

func createIconErr(t *testing.T, p service.CreateServerParams, iconErr error) (model.Server, error, *fakeRepoHelper, *fakeIconsHelper) {
	t.Helper()
	repo, icons := &fakeRepoHelper{}, &fakeIconsHelper{err: iconErr}
	srv, err := service.New(repo, icons, repo, repo).CreateServer(context.Background(), p)
	return srv, err, repo, icons
}

func validParams(name string) service.CreateServerParams {
	return service.CreateServerParams{Name: name, OwnerID: testOwner}
}

func iconParams(name string) service.CreateServerParams {
	return service.CreateServerParams{Name: name, OwnerID: testOwner, Icon: []byte("some-bytes")}
}

func findOutboxEvent(t *testing.T, events []outbox.Event, routingKey string) outbox.Event {
	t.Helper()
	for _, e := range events {
		if e.RoutingKey == routingKey {
			return e
		}
	}
	t.Fatalf("no outbox event with routing key %q among %v", routingKey, events)
	return outbox.Event{}
}
