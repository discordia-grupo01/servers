package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/service"
)

func TestListMyServers_RequiresIdentity(t *testing.T) {
	repo, icons := &fakeRepoHelper{}, &fakeIconsHelper{}
	_, err := service.New(repo, icons, repo, repo).ListMyServers(context.Background(), "")
	if !errors.Is(err, model.ErrUnauthorized) {
		t.Errorf("err = %v, want ErrUnauthorized", err)
	}
	if repo.listCalls != 0 {
		t.Error("repo must not be queried without a caller id")
	}
}

func TestListMyServers_ReturnsWhatTheRepoGives(t *testing.T) {
	want := []model.Server{{Name: "A"}, {Name: "B"}}
	repo, icons := &fakeRepoHelper{listResult: want}, &fakeIconsHelper{}

	got, err := service.New(repo, icons, repo, repo).ListMyServers(context.Background(), testOwner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d servers, want 2", len(got))
	}
	if repo.listCalls != 1 || repo.gotListUser != testOwner {
		t.Errorf("repo called %d times with user %q, want 1 with %q", repo.listCalls, repo.gotListUser, testOwner)
	}
}

func TestListMyServers_PropagatesRepositoryError(t *testing.T) {
	repo, icons := &fakeRepoHelper{listErr: errors.New("db down")}, &fakeIconsHelper{}
	_, err := service.New(repo, icons, repo, repo).ListMyServers(context.Background(), testOwner)
	if err == nil {
		t.Fatal("expected the repository error to surface")
	}
}
