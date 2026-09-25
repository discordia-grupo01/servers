package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/service"
)

func TestGetServer_ReturnsWhatTheRepoGives(t *testing.T) {
	want := model.Server{Name: "A Server", OwnerID: testOwner}
	repo, icons := &fakeRepoHelper{getResult: want}, &fakeIconsHelper{}

	got, err := service.New(repo, icons).GetServer(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != want.Name {
		t.Errorf("got = %+v, want %+v", got, want)
	}
	if repo.getCalls != 1 {
		t.Errorf("GetByID calls = %d, want 1", repo.getCalls)
	}
}

func TestGetServer_UnknownServer_PropagatesNotFound(t *testing.T) {
	repo, icons := &fakeRepoHelper{getErr: model.ErrServerNotFound}, &fakeIconsHelper{}

	_, err := service.New(repo, icons).GetServer(context.Background(), uuid.New())
	if !errors.Is(err, model.ErrServerNotFound) {
		t.Errorf("err = %v, want ErrServerNotFound", err)
	}
}

func TestGetServerImageURLs_ReturnsWhatTheRepoGives(t *testing.T) {
	iconURL, bannerURL := "/v1/servers/srv-1/icon?v=abc", "/v1/servers/srv-1/banner?v=def"
	want := model.Server{IconURL: &iconURL, BannerURL: &bannerURL}
	repo, icons := &fakeRepoHelper{getResult: want}, &fakeIconsHelper{}

	gotIcon, gotBanner, err := service.New(repo, icons).GetServerImageURLs(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotIcon == nil || *gotIcon != iconURL || gotBanner == nil || *gotBanner != bannerURL {
		t.Errorf("got icon=%v banner=%v", gotIcon, gotBanner)
	}
	if repo.iconURLsCalls != 1 {
		t.Errorf("IconURLs calls = %d, want 1", repo.iconURLsCalls)
	}
	if repo.getCalls != 0 {
		t.Error("must use the lean IconURLs lookup, not the full GetByID")
	}
}

func TestGetServerImageURLs_UnknownServer_PropagatesNotFound(t *testing.T) {
	repo, icons := &fakeRepoHelper{getErr: model.ErrServerNotFound}, &fakeIconsHelper{}

	_, _, err := service.New(repo, icons).GetServerImageURLs(context.Background(), uuid.New())
	if !errors.Is(err, model.ErrServerNotFound) {
		t.Errorf("err = %v, want ErrServerNotFound", err)
	}
}
