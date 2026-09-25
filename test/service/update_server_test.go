package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/service"
)

func serverToUpdate() model.Server {
	iconURL := "/v1/servers/srv-1/icon"
	return model.Server{ID: uuid.New(), Name: "Old Name", NameKey: "old name", OwnerID: testOwner, IconURL: &iconURL}
}

func update(t *testing.T, repo *fakeRepoHelper, icons *fakeIconsHelper, p service.UpdateServerParams) (model.Server, error) {
	t.Helper()
	return service.New(repo, icons).UpdateServer(context.Background(), p)
}

// CA1: a valid edit by the owner persists and is reflected back.
func TestUpdateServer_NameChange_Persists(t *testing.T) {
	srv := serverToUpdate()
	repo, icons := &fakeRepoHelper{getResult: srv}, &fakeIconsHelper{}
	newName := "New Name"

	got, err := update(t, repo, icons, service.UpdateServerParams{
		ServerID: srv.ID, ActorUserID: testOwner, Name: &newName,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != newName || got.NameKey != "new name" {
		t.Errorf("got = %+v", got)
	}
	if repo.updateCalls != 1 {
		t.Errorf("UpdateServer calls = %d, want 1", repo.updateCalls)
	}
	if icons.savedID != "" {
		t.Error("changing only the name must not touch the icon store")
	}
}

func TestUpdateServer_IconChange_SavesUnderServerID(t *testing.T) {
	srv := serverToUpdate()
	repo, icons := &fakeRepoHelper{getResult: srv}, &fakeIconsHelper{}
	data := []byte("new-icon-bytes")

	got, err := update(t, repo, icons, service.UpdateServerParams{
		ServerID: srv.ID, ActorUserID: testOwner, Icon: data,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if icons.savedID != srv.ID.String() || string(icons.savedData) != string(data) {
		t.Errorf("icon store got id=%q data=%q", icons.savedID, icons.savedData)
	}
	if got.IconURL == nil || *got.IconURL != "/v1/servers/"+srv.ID.String()+"/icon?v=fakehash" {
		t.Errorf("icon_url = %v", got.IconURL)
	}
}

func TestUpdateServer_IconChange_DeletesThePreviousVersion(t *testing.T) {
	oldIconURL := "/v1/servers/srv-1/icon?v=oldhash"
	srv := serverToUpdate()
	srv.IconURL = &oldIconURL
	repo, icons := &fakeRepoHelper{getResult: srv}, &fakeIconsHelper{}

	_, err := update(t, repo, icons, service.UpdateServerParams{
		ServerID: srv.ID, ActorUserID: testOwner, Icon: []byte("new-icon-bytes"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !icons.deleteCalled {
		t.Error("changing the icon must clean up the previous version (avoid orphaned files growing the bucket forever)")
	}
}

func TestUpdateServer_NameOnlyChange_DoesNotDeleteTheIcon(t *testing.T) {
	iconURL := "/v1/servers/srv-1/icon?v=currenthash"
	srv := serverToUpdate()
	srv.IconURL = &iconURL
	repo, icons := &fakeRepoHelper{getResult: srv}, &fakeIconsHelper{}
	newName := "New Name"

	_, err := update(t, repo, icons, service.UpdateServerParams{
		ServerID: srv.ID, ActorUserID: testOwner, Name: &newName,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if icons.deleteCalled {
		t.Error("changing only the name must not delete the current icon")
	}
}

func TestUpdateServer_BannerChange_SavesUnderDistinctKey(t *testing.T) {
	srv := serverToUpdate()
	repo, icons := &fakeRepoHelper{getResult: srv}, &fakeIconsHelper{}
	data := []byte("new-banner-bytes")

	got, err := update(t, repo, icons, service.UpdateServerParams{
		ServerID: srv.ID, ActorUserID: testOwner, Banner: data,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantKey := service.BannerObjectID(srv.ID)
	if icons.savedID != wantKey || string(icons.savedData) != string(data) {
		t.Errorf("icon store got id=%q data=%q, want id=%q", icons.savedID, icons.savedData, wantKey)
	}
	if got.BannerURL == nil {
		t.Error("banner_url = nil, want it set")
	}
	if got.IconURL == nil || *got.IconURL != *srv.IconURL {
		t.Error("changing the banner must not touch the existing icon_url")
	}
}

func TestUpdateServer_RemoveBanner_ClearsURLAndDeletesFromStore(t *testing.T) {
	bannerURL := "/v1/servers/srv-1/banner?v=deadbeef"
	srv := serverToUpdate()
	srv.BannerURL = &bannerURL
	repo, icons := &fakeRepoHelper{getResult: srv}, &fakeIconsHelper{}

	got, err := update(t, repo, icons, service.UpdateServerParams{
		ServerID: srv.ID, ActorUserID: testOwner, RemoveBanner: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.BannerURL != nil {
		t.Errorf("banner_url = %v, want nil", got.BannerURL)
	}
	if !icons.deleteCalled {
		t.Error("removing the banner must delete it from the icon store (avoid orphaned files)")
	}
}

// CA2: a non-owner cannot edit the server, and nothing is persisted.
func TestUpdateServer_NotOwner_Forbidden(t *testing.T) {
	srv := serverToUpdate()
	repo, icons := &fakeRepoHelper{getResult: srv}, &fakeIconsHelper{}
	newName := "Hijacked"

	_, err := update(t, repo, icons, service.UpdateServerParams{
		ServerID: srv.ID, ActorUserID: "someone-else", Name: &newName,
	})
	if !errors.Is(err, model.ErrForbiddenNotOwner) {
		t.Errorf("err = %v, want ErrForbiddenNotOwner", err)
	}
	if repo.updateCalls != 0 {
		t.Error("UpdateServer must not be called when the actor isn't the owner")
	}
}

func TestUpdateServer_Unauthenticated(t *testing.T) {
	repo, icons := &fakeRepoHelper{}, &fakeIconsHelper{}

	_, err := update(t, repo, icons, service.UpdateServerParams{ServerID: uuid.New()})
	if !errors.Is(err, model.ErrUnauthorized) {
		t.Errorf("err = %v, want ErrUnauthorized", err)
	}
	if repo.getCalls != 0 {
		t.Error("must not even look up the server without an actor")
	}
}

func TestUpdateServer_UnknownServer_PropagatesNotFound(t *testing.T) {
	repo, icons := &fakeRepoHelper{getErr: model.ErrServerNotFound}, &fakeIconsHelper{}
	newName := "New Name"

	_, err := update(t, repo, icons, service.UpdateServerParams{
		ServerID: uuid.New(), ActorUserID: testOwner, Name: &newName,
	})
	if !errors.Is(err, model.ErrServerNotFound) {
		t.Errorf("err = %v, want ErrServerNotFound", err)
	}
}

// CA3: invalid data is rejected and nothing is persisted or uploaded.
func TestUpdateServer_InvalidName_Rejected(t *testing.T) {
	srv := serverToUpdate()
	repo, icons := &fakeRepoHelper{getResult: srv}, &fakeIconsHelper{}
	tooShort := "a"

	_, err := update(t, repo, icons, service.UpdateServerParams{
		ServerID: srv.ID, ActorUserID: testOwner, Name: &tooShort,
	})
	if !errors.Is(err, model.ErrNameTooShort) {
		t.Errorf("err = %v, want ErrNameTooShort", err)
	}
	if repo.updateCalls != 0 {
		t.Error("UpdateServer must not be called with an invalid name")
	}
}

func TestUpdateServer_InvalidNameWithIcon_DoesNotUploadTheIcon(t *testing.T) {
	srv := serverToUpdate()
	repo, icons := &fakeRepoHelper{getResult: srv}, &fakeIconsHelper{}
	empty := ""

	_, err := update(t, repo, icons, service.UpdateServerParams{
		ServerID: srv.ID, ActorUserID: testOwner, Name: &empty, Icon: []byte("data"),
	})
	if !errors.Is(err, model.ErrNameRequired) {
		t.Errorf("err = %v, want ErrNameRequired", err)
	}
	if icons.savedID != "" {
		t.Error("an invalid name must fail before any image is uploaded")
	}
}

func TestUpdateServer_NameTaken_Propagated(t *testing.T) {
	srv := serverToUpdate()
	repo := &fakeRepoHelper{getResult: srv, updateErr: model.ErrNameTaken}
	icons := &fakeIconsHelper{}
	newName := "Taken Name"

	_, err := update(t, repo, icons, service.UpdateServerParams{
		ServerID: srv.ID, ActorUserID: testOwner, Name: &newName,
	})
	if !errors.Is(err, model.ErrNameTaken) {
		t.Errorf("err = %v, want ErrNameTaken", err)
	}
}

func TestUpdateServer_UnsupportedIcon_Rejected(t *testing.T) {
	srv := serverToUpdate()
	repo := &fakeRepoHelper{getResult: srv}
	icons := &fakeIconsHelper{err: model.ErrIconUnsupportedType}

	_, err := update(t, repo, icons, service.UpdateServerParams{
		ServerID: srv.ID, ActorUserID: testOwner, Icon: []byte("not-an-image"),
	})
	if !errors.Is(err, model.ErrIconUnsupportedType) {
		t.Errorf("err = %v, want ErrIconUnsupportedType", err)
	}
	if repo.updateCalls != 0 {
		t.Error("UpdateServer must not be called when the icon is rejected")
	}
}
