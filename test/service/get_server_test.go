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

	got, err := service.New(repo, icons, repo, repo).GetServer(context.Background(), uuid.New(), testOwner)
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

	_, err := service.New(repo, icons, repo, repo).GetServer(context.Background(), uuid.New(), testOwner)
	if !errors.Is(err, model.ErrServerNotFound) {
		t.Errorf("err = %v, want ErrServerNotFound", err)
	}
}

func TestGetServerImageURLs_ReturnsWhatTheRepoGives(t *testing.T) {
	iconURL, bannerURL := "/v1/servers/srv-1/icon?v=abc", "/v1/servers/srv-1/banner?v=def"
	want := model.Server{IconURL: &iconURL, BannerURL: &bannerURL}
	repo, icons := &fakeRepoHelper{getResult: want}, &fakeIconsHelper{}

	gotIcon, gotBanner, err := service.New(repo, icons, repo, repo).GetServerImageURLs(context.Background(), uuid.New())
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

	_, _, err := service.New(repo, icons, repo, repo).GetServerImageURLs(context.Background(), uuid.New())
	if !errors.Is(err, model.ErrServerNotFound) {
		t.Errorf("err = %v, want ErrServerNotFound", err)
	}
}

func TestCheckMembership_Member_ReturnsNil(t *testing.T) {
	repo, icons := &fakeRepoHelper{memberResult: true}, &fakeIconsHelper{}

	if err := service.New(repo, icons, repo, repo).CheckMembership(context.Background(), uuid.New(), "user-1"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if repo.memberCalls != 1 || repo.getCalls != 0 {
		t.Errorf("IsMember calls = %d, GetByID calls = %d, want 1 and 0", repo.memberCalls, repo.getCalls)
	}
}

func TestCheckMembership_NotAMember_ReturnsForbidden(t *testing.T) {
	repo, icons := &fakeRepoHelper{}, &fakeIconsHelper{}

	err := service.New(repo, icons, repo, repo).CheckMembership(context.Background(), uuid.New(), "user-1")
	if !errors.Is(err, model.ErrForbiddenNotMember) {
		t.Errorf("err = %v, want ErrForbiddenNotMember", err)
	}
}

func TestCheckMembership_UnknownServer_PropagatesNotFound(t *testing.T) {
	repo, icons := &fakeRepoHelper{memberErr: model.ErrServerNotFound}, &fakeIconsHelper{}

	err := service.New(repo, icons, repo, repo).CheckMembership(context.Background(), uuid.New(), "user-1")
	if !errors.Is(err, model.ErrServerNotFound) {
		t.Errorf("err = %v, want ErrServerNotFound", err)
	}
}

func TestCheckMembership_NoUser_ReturnsUnauthorized(t *testing.T) {
	repo, icons := &fakeRepoHelper{memberResult: true}, &fakeIconsHelper{}

	err := service.New(repo, icons, repo, repo).CheckMembership(context.Background(), uuid.New(), "")
	if !errors.Is(err, model.ErrUnauthorized) {
		t.Errorf("err = %v, want ErrUnauthorized", err)
	}
	if repo.memberCalls != 0 {
		t.Error("must not query without a user")
	}
}
