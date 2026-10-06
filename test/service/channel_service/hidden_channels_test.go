package channel_service_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/channelperm"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/service/channel_service"
)

// manager holds MANAGE_CHANNELS but, by default, no override lets them see
// the hidden channel.
func manager() role.Standing {
	return role.Standing{IsMember: true, Permissions: role.PermManageChannels | role.PermViewChannels}
}

// hiddenFixture: a server whose #staff is hidden from @everyone.
type hiddenFixture struct {
	srv        model.Server
	staff      model.Channel
	channels   *fakeChannels
	visibility *fakeVisibility
	standings  *fakeStandings
	svc        *channel_service.ChannelService
}

func newHiddenFixture(st role.Standing) *hiddenFixture {
	srv := ownerServer()
	staff := model.Channel{ID: uuid.New(), ServerID: srv.ID, Name: "staff", Kind: model.ChannelText}
	srv.Channels = []model.Channel{staff}
	channels := &fakeChannels{getByIDChannel: staff}
	visibility := &fakeVisibility{layers: map[uuid.UUID]channelperm.Layers{
		staff.ID: {Everyone: channelperm.Override{Deny: role.PermViewChannels}},
	}}
	standings := &fakeStandings{standing: st}
	return &hiddenFixture{
		srv: srv, staff: staff, channels: channels, visibility: visibility, standings: standings,
		svc: newServiceWithVisibility(&fakeServers{server: srv}, &fakeCategories{}, channels, standings, visibility),
	}
}

func str(s string) *string { return &s }

// #57: a manager who can't see a channel can't edit, delete or move it, and
// gets a 404 (not a 403) so they don't learn it exists.
func TestHiddenChannel_ManagementIsNotFound(t *testing.T) {
	f := newHiddenFixture(manager())
	ctx := context.Background()

	_, err := f.svc.UpdateChannel(ctx, channel_service.UpdateChannelParams{ChannelID: f.staff.ID, UserID: "manager-1", Name: str("x")})
	if !errors.Is(err, model.ErrChannelNotFound) {
		t.Errorf("update: err = %v, want ErrChannelNotFound", err)
	}
	if err := f.svc.DeleteChannel(ctx, channel_service.DeleteChannelParams{ChannelID: f.staff.ID, UserID: "manager-1"}); !errors.Is(err, model.ErrChannelNotFound) {
		t.Errorf("delete: err = %v, want ErrChannelNotFound", err)
	}
	_, err = f.svc.MoveChannelToCategory(ctx, channel_service.MoveChannelToCategoryParams{ChannelID: f.staff.ID, UserID: "manager-1"})
	if !errors.Is(err, model.ErrChannelNotFound) {
		t.Errorf("move: err = %v, want ErrChannelNotFound", err)
	}
	if f.channels.updatedID != uuid.Nil || f.channels.deleteCalled || f.channels.moveCalled {
		t.Error("the hidden channel was modified")
	}
}

// With an override that gives their role VIEW_CHANNELS back, they can manage
// it normally.
func TestHiddenChannel_VisibleThroughAnOverride_CanBeManaged(t *testing.T) {
	f := newHiddenFixture(manager())
	l := f.visibility.layers[f.staff.ID]
	l.Roles = []channelperm.Override{{Allow: role.PermViewChannels}}
	f.visibility.layers[f.staff.ID] = l

	if err := f.svc.DeleteChannel(context.Background(), channel_service.DeleteChannelParams{ChannelID: f.staff.ID, UserID: "manager-1"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !f.channels.deleteCalled {
		t.Error("the channel wasn't deleted")
	}
}

// The owner and administrators see every channel: no override lookup.
func TestHiddenChannel_OwnerAndAdministratorsAreUnaffected(t *testing.T) {
	f := newHiddenFixture(role.Standing{})
	if err := f.svc.DeleteChannel(context.Background(), channel_service.DeleteChannelParams{ChannelID: f.staff.ID, UserID: testOwner}); err != nil {
		t.Errorf("owner: unexpected error %v", err)
	}

	f = newHiddenFixture(adminStanding())
	if err := f.svc.DeleteChannel(context.Background(), channel_service.DeleteChannelParams{ChannelID: f.staff.ID, UserID: "admin-1"}); err != nil {
		t.Errorf("administrator: unexpected error %v", err)
	}
	if f.visibility.calls != 0 {
		t.Error("overrides were loaded for an administrator")
	}
}

// Non-members keep getting the permission error, as before #57.
func TestHiddenChannel_NonMemberStillForbidden(t *testing.T) {
	f := newHiddenFixture(role.Standing{})
	err := f.svc.DeleteChannel(context.Background(), channel_service.DeleteChannelParams{ChannelID: f.staff.ID, UserID: "stranger"})
	if !errors.Is(err, model.ErrForbiddenNotAdmin) {
		t.Errorf("err = %v, want ErrForbiddenNotAdmin", err)
	}
}

func TestHiddenChannel_OverridesError_Propagates(t *testing.T) {
	f := newHiddenFixture(manager())
	boom := errors.New("db down")
	f.visibility.err = boom
	if err := f.svc.DeleteChannel(context.Background(), channel_service.DeleteChannelParams{ChannelID: f.staff.ID, UserID: "manager-1"}); !errors.Is(err, boom) {
		t.Errorf("err = %v, want %v", err, boom)
	}
}

// #57 reorder: the manager sends only what they see; #staff keeps its place.
func TestReorderChannels_HiddenChannelKeepsItsPlace(t *testing.T) {
	f := newHiddenFixture(manager())
	reglas := model.Channel{ID: uuid.New(), ServerID: f.srv.ID, Name: "reglas"}
	charla := model.Channel{ID: uuid.New(), ServerID: f.srv.ID, Name: "charla"}
	memes := model.Channel{ID: uuid.New(), ServerID: f.srv.ID, Name: "memes"}
	current := []model.Channel{reglas, f.staff, charla, memes}
	f.srv.Channels = current
	f.channels.listResult = current
	f.svc = newServiceWithVisibility(&fakeServers{server: f.srv}, &fakeCategories{}, f.channels, f.standings, f.visibility)

	err := f.svc.ReorderChannels(context.Background(), channel_service.ReorderChannelsParams{
		ServerID: f.srv.ID, UserID: "manager-1", ChannelIDs: []uuid.UUID{memes.ID, reglas.ID, charla.ID},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := []uuid.UUID{memes.ID, f.staff.ID, reglas.ID, charla.ID}; !reflect.DeepEqual(f.channels.reorderedIDs, want) {
		t.Errorf("order = %v, want memes, staff, reglas, charla", f.channels.reorderedIDs)
	}
	if !f.channels.gotHidden[f.staff.ID] || len(f.channels.gotHidden) != 1 {
		t.Errorf("hidden = %v, want only #staff", f.channels.gotHidden)
	}

	// Including the hidden channel is an invalid (stale) list.
	f.channels.reorderedIDs = nil
	err = f.svc.ReorderChannels(context.Background(), channel_service.ReorderChannelsParams{
		ServerID: f.srv.ID, UserID: "manager-1", ChannelIDs: []uuid.UUID{memes.ID, f.staff.ID, reglas.ID, charla.ID},
	})
	if !errors.Is(err, model.ErrChannelReorderInvalid) || f.channels.reorderedIDs != nil {
		t.Errorf("with the hidden id: err = %v, written %v", err, f.channels.reorderedIDs)
	}
}

func TestReorderChannels_OwnerSeesEverything_NoHiddenChannels(t *testing.T) {
	f := newHiddenFixture(role.Standing{})
	f.channels.listResult = []model.Channel{f.staff}
	err := f.svc.ReorderChannels(context.Background(), channel_service.ReorderChannelsParams{
		ServerID: f.srv.ID, UserID: testOwner, ChannelIDs: []uuid.UUID{f.staff.ID},
	})
	if err != nil || len(f.channels.gotHidden) != 0 || f.visibility.calls != 0 {
		t.Errorf("err = %v, hidden = %v, overrides lookups = %d", err, f.channels.gotHidden, f.visibility.calls)
	}
}

func TestReorderChannels_OverridesError_Propagates(t *testing.T) {
	f := newHiddenFixture(manager())
	boom := errors.New("db down")
	f.visibility.err = boom
	err := f.svc.ReorderChannels(context.Background(), channel_service.ReorderChannelsParams{ServerID: f.srv.ID, UserID: "manager-1"})
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want %v", err, boom)
	}
}
