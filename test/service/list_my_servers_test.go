package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

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

// A suspended server is listed, marked, and shows nothing of its content: no
// channels and no categories, and no visibility is worked out for it.
func TestListMyServers_SuspendedServerComesWithoutContent(t *testing.T) {
	when := time.Now().UTC()
	channel := model.Channel{ID: uuid.New(), Name: "general", Kind: model.ChannelText}
	active := model.Server{ID: uuid.New(), Name: "Activo", OwnerID: testOwner, Channels: []model.Channel{channel}, Categories: []model.Category{{ID: uuid.New()}}}
	suspended := model.Server{
		ID: uuid.New(), Name: "Suspendido", OwnerID: testOwner, SuspendedAt: &when,
		Channels: []model.Channel{channel}, Categories: []model.Category{{ID: uuid.New()}},
	}
	repo := &fakeRepoHelper{listResult: []model.Server{suspended, active}}

	got, err := service.New(repo, &fakeIconsHelper{}, repo, repo).ListMyServers(context.Background(), testOwner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(got) != 2 || got[0].Name != "Suspendido" || got[1].Name != "Activo" {
		t.Fatalf("servers = %v, want both, in the same order", got)
	}
	if !got[0].Suspended() {
		t.Error("the suspended server lost its mark")
	}
	if got[0].Channels == nil || len(got[0].Channels) != 0 || len(got[0].Categories) != 0 {
		t.Errorf("suspended server: channels=%v categories=%v, want empty (and channels never nil)", got[0].Channels, got[0].Categories)
	}
	if got[1].Suspended() || len(got[1].Channels) != 1 || len(got[1].Categories) != 1 {
		t.Errorf("active server = %+v, want it untouched", got[1])
	}
}

// A suspended server first in the list must not stop the rest from being
// filtered: a channel hidden in the next server stays hidden.
func TestListMyServers_ServerAfterASuspendedOneIsStillFiltered(t *testing.T) {
	when := time.Now().UTC()
	suspended := model.Server{ID: uuid.New(), Name: "Suspendido", OwnerID: testOwner, SuspendedAt: &when}
	theirs, _, _, layers := hiddenStaffServer("someone-else")
	repo := &fakeRepoHelper{listResult: []model.Server{suspended, theirs}, standing: viewer(), layers: layers}

	got, err := service.New(repo, &fakeIconsHelper{}, repo, repo).ListMyServers(context.Background(), testOwner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n := channelNames(got[1].Channels); len(n) != 1 || n[0] != "general" {
		t.Errorf("server after the suspended one: channels = %v, want [general]", n)
	}
}

// When every server is suspended there is nothing to load visibility for.
func TestListMyServers_OnlySuspendedServersSkipsTheVisibilityQueries(t *testing.T) {
	when := time.Now().UTC()
	repo := &fakeRepoHelper{
		listResult:   []model.Server{{ID: uuid.New(), SuspendedAt: &when}},
		standingsErr: errors.New("must not be called"),
		layersErr:    errors.New("must not be called"),
	}

	got, err := service.New(repo, &fakeIconsHelper{}, repo, repo).ListMyServers(context.Background(), testOwner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || !got[0].Suspended() {
		t.Errorf("servers = %+v, want the suspended one", got)
	}
	if repo.layersCalls != 0 {
		t.Errorf("ChannelLayers calls = %d, want 0", repo.layersCalls)
	}
}
