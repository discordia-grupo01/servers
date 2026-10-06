package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/channelperm"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/service"
)

// #57 filtrado: a server with #general and #staff, where @everyone loses
// VIEW_CHANNELS in #staff.
func hiddenStaffServer(owner string) (model.Server, model.Channel, model.Channel, map[uuid.UUID]channelperm.Layers) {
	general := model.Channel{ID: uuid.New(), Name: "general", Kind: model.ChannelText}
	staff := model.Channel{ID: uuid.New(), Name: "staff", Kind: model.ChannelText}
	srv := model.Server{ID: uuid.New(), Name: "S", OwnerID: owner, Channels: []model.Channel{general, staff}}
	layers := map[uuid.UUID]channelperm.Layers{staff.ID: {Everyone: channelperm.Override{Deny: role.PermViewChannels}}}
	return srv, general, staff, layers
}

func channelNames(chs []model.Channel) []string {
	out := make([]string, 0, len(chs))
	for _, ch := range chs {
		out = append(out, ch.Name)
	}
	return out
}

func viewer() role.Standing {
	return role.Standing{IsMember: true, Permissions: role.PermViewChannels | role.PermSendMessages}
}

func TestGetServer_HidesChannelsTheMemberCantSee(t *testing.T) {
	srv, _, _, layers := hiddenStaffServer("someone-else")
	repo, icons := &fakeRepoHelper{getResult: srv, standing: viewer(), layers: layers}, &fakeIconsHelper{}

	got, err := service.New(repo, icons, repo, repo).GetServer(context.Background(), srv.ID, testOwner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if names := channelNames(got.Channels); len(names) != 1 || names[0] != "general" {
		t.Errorf("channels = %v, want [general]", names)
	}
}

func TestGetServer_OwnerAndAdministratorsSeeEverything(t *testing.T) {
	srv, _, _, layers := hiddenStaffServer(testOwner)
	repo, icons := &fakeRepoHelper{getResult: srv, layers: layers}, &fakeIconsHelper{}
	got, err := service.New(repo, icons, repo, repo).GetServer(context.Background(), srv.ID, testOwner)
	if err != nil || len(got.Channels) != 2 {
		t.Errorf("owner: channels = %v, err = %v", channelNames(got.Channels), err)
	}
	if repo.layersCalls != 0 {
		t.Error("overrides were loaded for the owner")
	}

	srv, _, _, layers = hiddenStaffServer("someone-else")
	admin := role.Standing{IsMember: true, Permissions: role.PermAdministrator}
	repo = &fakeRepoHelper{getResult: srv, standing: admin, layers: layers}
	got, err = service.New(repo, icons, repo, repo).GetServer(context.Background(), srv.ID, testOwner)
	if err != nil || len(got.Channels) != 2 {
		t.Errorf("administrator: channels = %v, err = %v", channelNames(got.Channels), err)
	}
}

func TestGetServer_NonMemberGetsNoChannels(t *testing.T) {
	srv, _, _, _ := hiddenStaffServer("someone-else")
	repo, icons := &fakeRepoHelper{getResult: srv}, &fakeIconsHelper{}

	got, err := service.New(repo, icons, repo, repo).GetServer(context.Background(), srv.ID, testOwner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "S" || got.Channels == nil || len(got.Channels) != 0 {
		t.Errorf("got = %+v, want the server with an empty (non-nil) channel list", got)
	}
}

// Fail closed: if the overrides can't be read, no list is returned at all.
func TestGetServer_OverridesError_FailsClosed(t *testing.T) {
	srv, _, _, _ := hiddenStaffServer("someone-else")
	boom := errors.New("db down")
	repo, icons := &fakeRepoHelper{getResult: srv, standing: viewer(), layersErr: boom}, &fakeIconsHelper{}
	if _, err := service.New(repo, icons, repo, repo).GetServer(context.Background(), srv.ID, testOwner); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}

	repo = &fakeRepoHelper{getResult: srv, standingErr: boom}
	if _, err := service.New(repo, icons, repo, repo).GetServer(context.Background(), srv.ID, testOwner); !errors.Is(err, boom) {
		t.Fatalf("standing error: err = %v, want %v", err, boom)
	}
}

func TestListMyServers_HidesChannelsPerServer(t *testing.T) {
	mine, _, _, layers := hiddenStaffServer(testOwner)
	theirs, _, _, theirLayers := hiddenStaffServer("someone-else")
	for k, v := range theirLayers {
		layers[k] = v
	}
	repo, icons := &fakeRepoHelper{listResult: []model.Server{mine, theirs}, standing: viewer(), layers: layers}, &fakeIconsHelper{}

	got, err := service.New(repo, icons, repo, repo).ListMyServers(context.Background(), testOwner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n := channelNames(got[0].Channels); len(n) != 2 {
		t.Errorf("own server: channels = %v, want both (the owner sees everything)", n)
	}
	if n := channelNames(got[1].Channels); len(n) != 1 || n[0] != "general" {
		t.Errorf("other server: channels = %v, want [general]", n)
	}
	if repo.layersCalls != 1 {
		t.Errorf("ChannelLayers called %d times, want once for every server together", repo.layersCalls)
	}
}

func TestListMyServers_NoServers_NoVisibilityQueries(t *testing.T) {
	repo, icons := &fakeRepoHelper{}, &fakeIconsHelper{}
	got, err := service.New(repo, icons, repo, repo).ListMyServers(context.Background(), testOwner)
	if err != nil || len(got) != 0 || repo.layersCalls != 0 {
		t.Errorf("got %v, err %v, layers calls %d", got, err, repo.layersCalls)
	}
}

func TestListMyServers_VisibilityErrors_FailClosed(t *testing.T) {
	srv, _, _, _ := hiddenStaffServer("someone-else")
	boom := errors.New("db down")
	for name, repo := range map[string]*fakeRepoHelper{
		"standings": {listResult: []model.Server{srv}, standingsErr: boom},
		"overrides": {listResult: []model.Server{srv}, layersErr: boom},
	} {
		if _, err := service.New(repo, &fakeIconsHelper{}, repo, repo).ListMyServers(context.Background(), testOwner); !errors.Is(err, boom) {
			t.Errorf("%s: err = %v, want %v", name, err, boom)
		}
	}
}
