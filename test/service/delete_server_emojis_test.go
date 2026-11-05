package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/service"
)

func TestDeleteServerRemovesEmojiImages(t *testing.T) {
	id := uuid.New()
	repo := &fakeRepoHelper{getResult: model.Server{ID: id, Name: "Mi server", OwnerID: testOwner}}
	icons := &fakeIconsHelper{}

	err := service.New(repo, icons, repo, repo).DeleteServer(context.Background(), service.DeleteServerParams{
		ServerID: id, ActorUserID: testOwner, ConfirmName: "Mi server",
	})
	if err != nil {
		t.Fatalf("DeleteServer() error = %v", err)
	}
	if len(icons.deletedEmojiServers) != 1 || icons.deletedEmojiServers[0] != id.String() {
		t.Errorf("deleted emoji folders = %v", icons.deletedEmojiServers)
	}
}

func TestDeleteServerByStaffRemovesEmojiImages(t *testing.T) {
	id := uuid.New()
	repo := &fakeRepoHelper{}
	icons := &fakeIconsHelper{}

	if err := service.New(repo, icons, repo, repo).DeleteServerByStaff(context.Background(), id); err != nil {
		t.Fatalf("DeleteServerByStaff() error = %v", err)
	}
	if len(icons.deletedEmojiServers) != 1 || icons.deletedEmojiServers[0] != id.String() {
		t.Errorf("deleted emoji folders = %v", icons.deletedEmojiServers)
	}
}

func TestDeleteServerSucceedsWhenEmojiCleanupFails(t *testing.T) {
	id := uuid.New()
	repo := &fakeRepoHelper{getResult: model.Server{ID: id, Name: "Mi server", OwnerID: testOwner}}
	icons := &fakeIconsHelper{deleteEmojisErr: errors.New("storage down")}

	err := service.New(repo, icons, repo, repo).DeleteServer(context.Background(), service.DeleteServerParams{
		ServerID: id, ActorUserID: testOwner, ConfirmName: "Mi server",
	})
	if err != nil {
		t.Errorf("DeleteServer() error = %v", err)
	}
}

func TestDeleteServerFailureKeepsEmojiImages(t *testing.T) {
	id := uuid.New()
	boom := errors.New("boom")
	repo := &fakeRepoHelper{getResult: model.Server{ID: id, Name: "Mi server", OwnerID: testOwner}, deleteErr: boom}
	icons := &fakeIconsHelper{}

	err := service.New(repo, icons, repo, repo).DeleteServer(context.Background(), service.DeleteServerParams{
		ServerID: id, ActorUserID: testOwner, ConfirmName: "Mi server",
	})
	if !errors.Is(err, boom) {
		t.Fatalf("error = %v", err)
	}
	if len(icons.deletedEmojiServers) != 0 {
		t.Errorf("deleted emoji folders = %v", icons.deletedEmojiServers)
	}
}
