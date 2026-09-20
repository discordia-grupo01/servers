package channel_service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/service/channel_service"
)

func TestCreateChannel_Valid_CreatesTextChannel(t *testing.T) {
	srv := ownerServer()
	channels := &fakeChannels{}
	svc := newService(&fakeServers{server: srv}, &fakeCategories{}, channels)

	ch, err := svc.CreateChannel(context.Background(), channel_service.CreateChannelParams{
		ServerID: srv.ID, Name: "general", Kind: "text", UserID: testOwner,
	})
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	if ch.Name != "general" || ch.Kind != model.ChannelText || ch.ServerID != srv.ID {
		t.Errorf("channel = %+v", ch)
	}
	if channels.createCalls != 1 {
		t.Errorf("createCalls = %d, want 1", channels.createCalls)
	}
}

func TestCreateChannel_EmptyName_ErrNameRequired(t *testing.T) {
	svc := newService(&fakeServers{server: ownerServer()}, &fakeCategories{}, &fakeChannels{})
	_, err := svc.CreateChannel(context.Background(), channel_service.CreateChannelParams{
		ServerID: uuid.New(), Name: "   ", Kind: "text", UserID: testOwner,
	})
	if !errors.Is(err, model.ErrChannelNameRequired) {
		t.Errorf("err = %v, want ErrChannelNameRequired", err)
	}
}

func TestCreateChannel_ControlCharInName_ErrNameInvalidChars(t *testing.T) {
	svc := newService(&fakeServers{server: ownerServer()}, &fakeCategories{}, &fakeChannels{})
	_, err := svc.CreateChannel(context.Background(), channel_service.CreateChannelParams{
		ServerID: uuid.New(), Name: "bad\x00name", Kind: "text", UserID: testOwner,
	})
	if !errors.Is(err, model.ErrChannelNameInvalidChars) {
		t.Errorf("err = %v, want ErrChannelNameInvalidChars", err)
	}
}

func TestCreateChannel_InvalidKind_ErrKindInvalid(t *testing.T) {
	svc := newService(&fakeServers{server: ownerServer()}, &fakeCategories{}, &fakeChannels{})
	_, err := svc.CreateChannel(context.Background(), channel_service.CreateChannelParams{
		ServerID: uuid.New(), Name: "general", Kind: "video", UserID: testOwner,
	})
	if !errors.Is(err, model.ErrChannelKindInvalid) {
		t.Errorf("err = %v, want ErrChannelKindInvalid", err)
	}
}

func TestCreateChannel_NotOwner_ErrForbidden(t *testing.T) {
	svc := newService(&fakeServers{server: ownerServer()}, &fakeCategories{}, &fakeChannels{})
	_, err := svc.CreateChannel(context.Background(), channel_service.CreateChannelParams{
		ServerID: uuid.New(), Name: "general", Kind: "text", UserID: "someone-else",
	})
	if !errors.Is(err, model.ErrForbiddenNotOwner) {
		t.Errorf("err = %v, want ErrForbiddenNotOwner", err)
	}
}

func TestCreateChannel_CategoryFromAnotherServer_ErrServerMismatch(t *testing.T) {
	srv := ownerServer()
	cat := model.Category{ID: uuid.New(), ServerID: uuid.New()} // different server
	svc := newService(&fakeServers{server: srv}, &fakeCategories{category: cat}, &fakeChannels{})

	catID := cat.ID
	_, err := svc.CreateChannel(context.Background(), channel_service.CreateChannelParams{
		ServerID: srv.ID, Name: "general", Kind: "text", CategoryID: &catID, UserID: testOwner,
	})
	if !errors.Is(err, model.ErrCategoryServerMismatch) {
		t.Errorf("err = %v, want ErrCategoryServerMismatch", err)
	}
}

func TestCreateChannel_NameTakenFromRepo_Propagates(t *testing.T) {
	svc := newService(&fakeServers{server: ownerServer()}, &fakeCategories{}, &fakeChannels{createErr: model.ErrChannelNameTaken})
	_, err := svc.CreateChannel(context.Background(), channel_service.CreateChannelParams{
		ServerID: uuid.New(), Name: "general", Kind: "text", UserID: testOwner,
	})
	if !errors.Is(err, model.ErrChannelNameTaken) {
		t.Errorf("err = %v, want ErrChannelNameTaken", err)
	}
}

func withNativeCategories(srv model.Server) model.Server {
	textKind, voiceKind := model.ChannelText, model.ChannelVoice
	srv.Categories = []model.Category{
		{ID: uuid.New(), ServerID: srv.ID, Name: model.DefaultTextCategoryName, DefaultKind: &textKind},
		{ID: uuid.New(), ServerID: srv.ID, Name: model.DefaultVoiceCategoryName, DefaultKind: &voiceKind},
	}
	return srv
}

func TestCreateChannel_NoCategoryID_TextChannel_DefaultsToNativeTextCategory(t *testing.T) {
	srv := withNativeCategories(ownerServer())
	channels := &fakeChannels{}
	svc := newService(&fakeServers{server: srv}, &fakeCategories{}, channels)

	ch, err := svc.CreateChannel(context.Background(), channel_service.CreateChannelParams{
		ServerID: srv.ID, Name: "general", Kind: "text", UserID: testOwner,
	})
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	wantCategory := findNativeCategory(t, srv, model.ChannelText)
	if ch.CategoryID == nil || *ch.CategoryID != wantCategory.ID {
		t.Errorf("CategoryID = %v, want %s", ch.CategoryID, wantCategory.ID)
	}
}

func TestCreateChannel_NoCategoryID_VoiceChannel_DefaultsToNativeVoiceCategory(t *testing.T) {
	srv := withNativeCategories(ownerServer())
	channels := &fakeChannels{}
	svc := newService(&fakeServers{server: srv}, &fakeCategories{}, channels)

	ch, err := svc.CreateChannel(context.Background(), channel_service.CreateChannelParams{
		ServerID: srv.ID, Name: "general-voice", Kind: "voice", UserID: testOwner,
	})
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	wantCategory := findNativeCategory(t, srv, model.ChannelVoice)
	if ch.CategoryID == nil || *ch.CategoryID != wantCategory.ID {
		t.Errorf("CategoryID = %v, want %s", ch.CategoryID, wantCategory.ID)
	}
}

// TestCreateChannel_NoCategoryID_NoNativeCategory_FallsBackToNil covers what
// happens once the native category for that kind has been deleted (they can
// be, like any other category, see model.Category.DefaultKind): the channel
// just lands in "sin categoria" instead of erroring or resurrecting one.
func TestCreateChannel_NoCategoryID_NoNativeCategory_FallsBackToNil(t *testing.T) {
	srv := ownerServer() // no Categories at all
	channels := &fakeChannels{}
	svc := newService(&fakeServers{server: srv}, &fakeCategories{}, channels)

	ch, err := svc.CreateChannel(context.Background(), channel_service.CreateChannelParams{
		ServerID: srv.ID, Name: "general", Kind: "text", UserID: testOwner,
	})
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	if ch.CategoryID != nil {
		t.Errorf("CategoryID = %v, want nil", ch.CategoryID)
	}
}

func TestCreateChannel_ExplicitCategoryID_OverridesNativeDefault(t *testing.T) {
	srv := withNativeCategories(ownerServer())
	explicit := model.Category{ID: uuid.New(), ServerID: srv.ID}
	channels := &fakeChannels{}
	svc := newService(&fakeServers{server: srv}, &fakeCategories{category: explicit}, channels)

	catID := explicit.ID
	ch, err := svc.CreateChannel(context.Background(), channel_service.CreateChannelParams{
		ServerID: srv.ID, Name: "general", Kind: "text", CategoryID: &catID, UserID: testOwner,
	})
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	if ch.CategoryID == nil || *ch.CategoryID != explicit.ID {
		t.Errorf("CategoryID = %v, want the explicit category %s, not the native default", ch.CategoryID, explicit.ID)
	}
}

func findNativeCategory(t *testing.T, srv model.Server, kind model.ChannelKind) model.Category {
	t.Helper()
	for _, cat := range srv.Categories {
		if cat.DefaultKind != nil && *cat.DefaultKind == kind {
			return cat
		}
	}
	t.Fatalf("no native category for kind %s in %+v", kind, srv.Categories)
	return model.Category{}
}

func TestUpdateChannel_NameAndTopic_Updates(t *testing.T) {
	srv := ownerServer()
	ch := model.Channel{ID: uuid.New(), ServerID: srv.ID, Name: "old", Kind: model.ChannelText}
	channels := &fakeChannels{getByIDChannel: ch}
	svc := newService(&fakeServers{server: srv}, &fakeCategories{}, channels)

	newName := "new-name"
	newTopic := "the topic"
	got, err := svc.UpdateChannel(context.Background(), channel_service.UpdateChannelParams{
		ChannelID: ch.ID, UserID: testOwner, Name: &newName, Topic: &newTopic,
	})
	if err != nil {
		t.Fatalf("UpdateChannel: %v", err)
	}
	if got.Name != newName || got.Topic == nil || *got.Topic != newTopic {
		t.Errorf("got = %+v", got)
	}
	if channels.updateName != newName || channels.updateTopic == nil || *channels.updateTopic != newTopic {
		t.Errorf("repo.Update called with name=%q topic=%v", channels.updateName, channels.updateTopic)
	}
}

func TestUpdateChannel_OnlyTopic_KeepsExistingName(t *testing.T) {
	srv := ownerServer()
	ch := model.Channel{ID: uuid.New(), ServerID: srv.ID, Name: "keep-me", Kind: model.ChannelText}
	channels := &fakeChannels{getByIDChannel: ch}
	svc := newService(&fakeServers{server: srv}, &fakeCategories{}, channels)

	newTopic := "only topic changes"
	got, err := svc.UpdateChannel(context.Background(), channel_service.UpdateChannelParams{
		ChannelID: ch.ID, UserID: testOwner, Topic: &newTopic,
	})
	if err != nil {
		t.Fatalf("UpdateChannel: %v", err)
	}
	if got.Name != "keep-me" {
		t.Errorf("name = %q, want unchanged", got.Name)
	}
}

func TestUpdateChannel_NotOwner_ErrForbidden(t *testing.T) {
	srv := ownerServer()
	ch := model.Channel{ID: uuid.New(), ServerID: srv.ID}
	svc := newService(&fakeServers{server: srv}, &fakeCategories{}, &fakeChannels{getByIDChannel: ch})

	_, err := svc.UpdateChannel(context.Background(), channel_service.UpdateChannelParams{
		ChannelID: ch.ID, UserID: "someone-else",
	})
	if !errors.Is(err, model.ErrForbiddenNotOwner) {
		t.Errorf("err = %v, want ErrForbiddenNotOwner", err)
	}
}

func TestDeleteChannel_Valid_CallsRepoDelete(t *testing.T) {
	srv := ownerServer()
	ch := model.Channel{ID: uuid.New(), ServerID: srv.ID}
	channels := &fakeChannels{getByIDChannel: ch}
	svc := newService(&fakeServers{server: srv}, &fakeCategories{}, channels)

	if err := svc.DeleteChannel(context.Background(), channel_service.DeleteChannelParams{
		ChannelID: ch.ID, UserID: testOwner,
	}); err != nil {
		t.Fatalf("DeleteChannel: %v", err)
	}
	if !channels.deleteCalled || channels.deletedID != ch.ID {
		t.Errorf("repo.Delete not called with the right id")
	}
}

func TestDeleteChannel_NotOwner_ErrForbidden(t *testing.T) {
	srv := ownerServer()
	ch := model.Channel{ID: uuid.New(), ServerID: srv.ID}
	svc := newService(&fakeServers{server: srv}, &fakeCategories{}, &fakeChannels{getByIDChannel: ch})

	err := svc.DeleteChannel(context.Background(), channel_service.DeleteChannelParams{
		ChannelID: ch.ID, UserID: "someone-else",
	})
	if !errors.Is(err, model.ErrForbiddenNotOwner) {
		t.Errorf("err = %v, want ErrForbiddenNotOwner", err)
	}
}

func TestMoveChannelToCategory_Valid_MovesIt(t *testing.T) {
	srv := ownerServer()
	ch := model.Channel{ID: uuid.New(), ServerID: srv.ID}
	cat := model.Category{ID: uuid.New(), ServerID: srv.ID}
	channels := &fakeChannels{getByIDChannel: ch}
	svc := newService(&fakeServers{server: srv}, &fakeCategories{category: cat}, channels)

	catID := cat.ID
	got, err := svc.MoveChannelToCategory(context.Background(), channel_service.MoveChannelToCategoryParams{
		ChannelID: ch.ID, UserID: testOwner, CategoryID: &catID,
	})
	if err != nil {
		t.Fatalf("MoveChannelToCategory: %v", err)
	}
	if got.CategoryID == nil || *got.CategoryID != catID {
		t.Errorf("got.CategoryID = %v, want %v", got.CategoryID, catID)
	}
	if !channels.moveCalled || channels.movedCategory == nil || *channels.movedCategory != catID {
		t.Errorf("repo.MoveToCategory not called correctly")
	}
}

func TestMoveChannelToCategory_ToNoCategory_PassesNil(t *testing.T) {
	srv := ownerServer()
	ch := model.Channel{ID: uuid.New(), ServerID: srv.ID}
	channels := &fakeChannels{getByIDChannel: ch}
	svc := newService(&fakeServers{server: srv}, &fakeCategories{}, channels)

	got, err := svc.MoveChannelToCategory(context.Background(), channel_service.MoveChannelToCategoryParams{
		ChannelID: ch.ID, UserID: testOwner, CategoryID: nil,
	})
	if err != nil {
		t.Fatalf("MoveChannelToCategory: %v", err)
	}
	if got.CategoryID != nil {
		t.Errorf("got.CategoryID = %v, want nil", got.CategoryID)
	}
	if channels.movedCategory != nil {
		t.Errorf("repo.MoveToCategory got categoryID=%v, want nil", channels.movedCategory)
	}
}

func TestMoveChannelToCategory_CategoryFromAnotherServer_ErrServerMismatch(t *testing.T) {
	srv := ownerServer()
	ch := model.Channel{ID: uuid.New(), ServerID: srv.ID}
	cat := model.Category{ID: uuid.New(), ServerID: uuid.New()}
	svc := newService(&fakeServers{server: srv}, &fakeCategories{category: cat}, &fakeChannels{getByIDChannel: ch})

	catID := cat.ID
	_, err := svc.MoveChannelToCategory(context.Background(), channel_service.MoveChannelToCategoryParams{
		ChannelID: ch.ID, UserID: testOwner, CategoryID: &catID,
	})
	if !errors.Is(err, model.ErrCategoryServerMismatch) {
		t.Errorf("err = %v, want ErrCategoryServerMismatch", err)
	}
}

func TestReorderChannels_ExactCurrentSet_Reorders(t *testing.T) {
	srv := ownerServer()
	id1, id2 := uuid.New(), uuid.New()
	channels := &fakeChannels{listResult: []model.Channel{{ID: id1}, {ID: id2}}}
	svc := newService(&fakeServers{server: srv}, &fakeCategories{}, channels)

	err := svc.ReorderChannels(context.Background(), channel_service.ReorderChannelsParams{
		ServerID: srv.ID, UserID: testOwner, ChannelIDs: []uuid.UUID{id2, id1},
	})
	if err != nil {
		t.Fatalf("ReorderChannels: %v", err)
	}
	if len(channels.reorderedIDs) != 2 || channels.reorderedIDs[0] != id2 || channels.reorderedIDs[1] != id1 {
		t.Errorf("reorderedIDs = %v", channels.reorderedIDs)
	}
}

func TestReorderChannels_MissingChannel_ErrReorderInvalid(t *testing.T) {
	srv := ownerServer()
	id1, id2 := uuid.New(), uuid.New()
	channels := &fakeChannels{listResult: []model.Channel{{ID: id1}, {ID: id2}}}
	svc := newService(&fakeServers{server: srv}, &fakeCategories{}, channels)

	err := svc.ReorderChannels(context.Background(), channel_service.ReorderChannelsParams{
		ServerID: srv.ID, UserID: testOwner, ChannelIDs: []uuid.UUID{id1},
	})
	if !errors.Is(err, model.ErrChannelReorderInvalid) {
		t.Errorf("err = %v, want ErrChannelReorderInvalid", err)
	}
	if channels.reorderedIDs != nil {
		t.Errorf("repo.ReorderPositions should not have been called")
	}
}

func TestReorderChannels_ExtraChannel_ErrReorderInvalid(t *testing.T) {
	srv := ownerServer()
	id1, id2 := uuid.New(), uuid.New()
	channels := &fakeChannels{listResult: []model.Channel{{ID: id1}}}
	svc := newService(&fakeServers{server: srv}, &fakeCategories{}, channels)

	err := svc.ReorderChannels(context.Background(), channel_service.ReorderChannelsParams{
		ServerID: srv.ID, UserID: testOwner, ChannelIDs: []uuid.UUID{id1, id2},
	})
	if !errors.Is(err, model.ErrChannelReorderInvalid) {
		t.Errorf("err = %v, want ErrChannelReorderInvalid", err)
	}
}

func TestReorderChannels_NotOwner_ErrForbidden(t *testing.T) {
	srv := ownerServer()
	svc := newService(&fakeServers{server: srv}, &fakeCategories{}, &fakeChannels{})

	err := svc.ReorderChannels(context.Background(), channel_service.ReorderChannelsParams{
		ServerID: srv.ID, UserID: "someone-else", ChannelIDs: []uuid.UUID{},
	})
	if !errors.Is(err, model.ErrForbiddenNotOwner) {
		t.Errorf("err = %v, want ErrForbiddenNotOwner", err)
	}
}
