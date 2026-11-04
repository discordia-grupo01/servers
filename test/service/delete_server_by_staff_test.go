package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/events-contract/membership"
	"github.com/discordia-grupo01/events-contract/servers"
	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/service"
)

func newService(repo *fakeRepoHelper, icons *fakeIconsHelper) *service.Service {
	return service.New(repo, icons, repo, repo)
}

func serviceDeleteParams(srv model.Server) service.DeleteServerParams {
	return service.DeleteServerParams{ServerID: srv.ID, ActorUserID: testOwner, ConfirmName: srv.Name}
}

func deleteByStaff(repo *fakeRepoHelper, icons *fakeIconsHelper, id uuid.UUID) error {
	return newService(repo, icons).DeleteServerByStaff(context.Background(), id)
}

// El staff borra sin ser el owner y sin escribir el nombre: esa confirmacion ya
// la hizo en admin.
func TestDeleteServerByStaff_DeletesWithoutOwnerNorConfirmation(t *testing.T) {
	srv := serverToDelete()
	repo, icons := &fakeRepoHelper{getResult: srv}, &fakeIconsHelper{}

	if err := deleteByStaff(repo, icons, srv.ID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.deleteCalls != 1 {
		t.Errorf("DeleteServer calls = %d, want 1", repo.deleteCalls)
	}
}

// Publica exactamente lo mismo que el borrado del owner: un member_left por
// miembro y al final servers.server_deleted con los canales.
func TestDeleteServerByStaff_PublishesTheSameEventsAsTheOwnerDeletion(t *testing.T) {
	srv := serverToDelete()
	ch1, ch2 := uuid.New(), uuid.New()
	newRepo := func() *fakeRepoHelper {
		return &fakeRepoHelper{getResult: srv, deleteMemberIDs: []string{testOwner, "user-2"}, deleteChannels: []uuid.UUID{ch1, ch2}}
	}

	byOwner := newRepo()
	if err := deleteServer(t, byOwner, &fakeIconsHelper{}, serviceDeleteParams(srv)); err != nil {
		t.Fatalf("owner deletion: %v", err)
	}
	byStaff := newRepo()
	if err := deleteByStaff(byStaff, &fakeIconsHelper{}, srv.ID); err != nil {
		t.Fatalf("staff deletion: %v", err)
	}

	if len(byStaff.deleteEvents) != 3 {
		t.Fatalf("events = %d, want 2 member_left + 1 server_deleted", len(byStaff.deleteEvents))
	}
	if len(byStaff.deleteEvents) != len(byOwner.deleteEvents) {
		t.Fatalf("staff published %d events, owner %d", len(byStaff.deleteEvents), len(byOwner.deleteEvents))
	}
	for i, e := range byStaff.deleteEvents {
		if e.RoutingKey != byOwner.deleteEvents[i].RoutingKey {
			t.Errorf("event %d: routing key %q, owner deletion had %q", i, e.RoutingKey, byOwner.deleteEvents[i].RoutingKey)
		}
	}
	for _, e := range byStaff.deleteEvents[:2] {
		if e.RoutingKey != membership.RoutingKeyMemberLeft {
			t.Errorf("routing key = %q, want %q", e.RoutingKey, membership.RoutingKeyMemberLeft)
		}
	}
	last := byStaff.deleteEvents[2]
	if last.RoutingKey != servers.RoutingKeyServerDeleted {
		t.Fatalf("last routing key = %q, want %q", last.RoutingKey, servers.RoutingKeyServerDeleted)
	}
	var deleted servers.ServerDeleted
	if err := json.Unmarshal(last.Payload, &deleted); err != nil {
		t.Fatalf("server_deleted payload: %v", err)
	}
	if deleted.ServerID != srv.ID.String() || len(deleted.ChannelIDs) != 2 {
		t.Errorf("server_deleted = %+v, want server %s and 2 channels", deleted, srv.ID)
	}
}

// Un pedido repetido, o un servidor que el owner ya borro, no es un error ni
// publica nada: el consumidor hace ack.
func TestDeleteServerByStaff_UnknownServerIsANoOp(t *testing.T) {
	t.Run("not found when reading it", func(t *testing.T) {
		repo, icons := &fakeRepoHelper{getErr: model.ErrServerNotFound}, &fakeIconsHelper{}
		if err := deleteByStaff(repo, icons, uuid.New()); err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if repo.deleteCalls != 0 || icons.deleteCalled {
			t.Error("nothing to delete for an unknown server")
		}
	})
	t.Run("deleted by someone else in between", func(t *testing.T) {
		srv := serverToDelete()
		repo, icons := &fakeRepoHelper{getResult: srv, deleteErr: model.ErrServerNotFound}, &fakeIconsHelper{}
		if err := deleteByStaff(repo, icons, srv.ID); err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if icons.deleteCalled {
			t.Error("images must not be removed when this call did not delete the server")
		}
	})
}

// Cualquier otro error se devuelve, para que el consumidor reintente.
func TestDeleteServerByStaff_RepositoryErrorsAreReturned(t *testing.T) {
	boom := errors.New("db down")
	srv := serverToDelete()

	t.Run("reading", func(t *testing.T) {
		repo := &fakeRepoHelper{getErr: boom}
		if err := deleteByStaff(repo, &fakeIconsHelper{}, srv.ID); !errors.Is(err, boom) {
			t.Fatalf("err = %v, want the repository error", err)
		}
		if repo.deleteCalls != 0 {
			t.Error("must not delete when it could not read the server")
		}
	})
	t.Run("deleting keeps the images", func(t *testing.T) {
		repo, icons := &fakeRepoHelper{getResult: srv, deleteErr: boom}, &fakeIconsHelper{}
		if err := deleteByStaff(repo, icons, srv.ID); !errors.Is(err, boom) {
			t.Fatalf("err = %v, want the repository error", err)
		}
		if icons.deleteCalled {
			t.Error("images must survive a failed deletion")
		}
	})
}

func TestDeleteServerByStaff_RemovesIconAndBannerAfterDeleting(t *testing.T) {
	srv := serverToDelete()
	repo, icons := &fakeRepoHelper{getResult: srv}, &fakeIconsHelper{}

	if err := deleteByStaff(repo, icons, srv.ID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(icons.deletedKeys) != 2 {
		t.Fatalf("deleted %d images, want 2 (icon and banner): %v", len(icons.deletedKeys), icons.deletedKeys)
	}
}

func TestDeleteServerByStaff_ServerWithoutImagesDoesNotTouchTheStore(t *testing.T) {
	srv := model.Server{ID: uuid.New(), Name: "Mi Comunidad", OwnerID: testOwner}
	repo, icons := &fakeRepoHelper{getResult: srv}, &fakeIconsHelper{}

	if err := deleteByStaff(repo, icons, srv.ID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if icons.deleteCalled {
		t.Error("there was nothing to remove from the icon store")
	}
}
