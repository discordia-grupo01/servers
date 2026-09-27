package channel_service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/service/channel_service"
)

const testAdmin = "admin-1"

// Bug #22: a non-owner holding ADMINISTRATOR manages channels.
func TestAdministrator_CanManageChannels(t *testing.T) {
	srv := ownerServer()
	ch := model.Channel{ID: uuid.New(), ServerID: srv.ID}
	id1, id2 := uuid.New(), uuid.New()
	channels := &fakeChannels{getByIDChannel: ch, listResult: []model.Channel{{ID: id1}, {ID: id2}}}
	svc := newServiceWithStandings(&fakeServers{server: srv}, &fakeCategories{}, channels, &fakeStandings{standing: adminStanding()})
	ctx := context.Background()

	if _, err := svc.CreateChannel(ctx, channel_service.CreateChannelParams{ServerID: srv.ID, Name: "general", Kind: "text", UserID: testAdmin}); err != nil {
		t.Errorf("CreateChannel: %v", err)
	}
	newName := "renamed"
	if _, err := svc.UpdateChannel(ctx, channel_service.UpdateChannelParams{ChannelID: ch.ID, UserID: testAdmin, Name: &newName}); err != nil {
		t.Errorf("UpdateChannel: %v", err)
	}
	if _, err := svc.MoveChannelToCategory(ctx, channel_service.MoveChannelToCategoryParams{ChannelID: ch.ID, UserID: testAdmin}); err != nil {
		t.Errorf("MoveChannelToCategory: %v", err)
	}
	if err := svc.ReorderChannels(ctx, channel_service.ReorderChannelsParams{ServerID: srv.ID, UserID: testAdmin, ChannelIDs: []uuid.UUID{id2, id1}}); err != nil {
		t.Errorf("ReorderChannels: %v", err)
	}
	if err := svc.DeleteChannel(ctx, channel_service.DeleteChannelParams{ChannelID: ch.ID, UserID: testAdmin}); err != nil {
		t.Errorf("DeleteChannel: %v", err)
	}
}

func TestGranularManageChannels_Allowed(t *testing.T) {
	srv := ownerServer()
	ch := model.Channel{ID: uuid.New(), ServerID: srv.ID}
	standing := role.Standing{IsMember: true, Permissions: role.PermManageChannels}
	svc := newServiceWithStandings(&fakeServers{server: srv}, &fakeCategories{}, &fakeChannels{getByIDChannel: ch}, &fakeStandings{standing: standing})

	if _, err := svc.CreateChannel(context.Background(), channel_service.CreateChannelParams{ServerID: srv.ID, Name: "general", Kind: "text", UserID: "mod-1"}); err != nil {
		t.Errorf("CreateChannel: %v", err)
	}
	newName := "renamed"
	if _, err := svc.UpdateChannel(context.Background(), channel_service.UpdateChannelParams{ChannelID: ch.ID, UserID: "mod-1", Name: &newName}); err != nil {
		t.Errorf("UpdateChannel: %v", err)
	}
}

func TestManageChannelsWithoutPermission_Forbidden(t *testing.T) {
	srv := ownerServer()
	standing := role.Standing{IsMember: true, Permissions: role.PermSendMessages}
	svc := newServiceWithStandings(&fakeServers{server: srv}, &fakeCategories{}, &fakeChannels{}, &fakeStandings{standing: standing})

	_, err := svc.CreateChannel(context.Background(), channel_service.CreateChannelParams{ServerID: srv.ID, Name: "general", Kind: "text", UserID: "member-1"})
	if !errors.Is(err, model.ErrForbiddenNotAdmin) {
		t.Errorf("err = %v, want ErrForbiddenNotAdmin", err)
	}
}

func TestCreateChannel_StandingLookupError_Propagates(t *testing.T) {
	boom := errors.New("db down")
	svc := newServiceWithStandings(&fakeServers{server: ownerServer()}, &fakeCategories{}, &fakeChannels{}, &fakeStandings{err: boom})

	_, err := svc.CreateChannel(context.Background(), channel_service.CreateChannelParams{
		ServerID: uuid.New(), Name: "general", Kind: "text", UserID: testAdmin,
	})
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want %v", err, boom)
	}
}
