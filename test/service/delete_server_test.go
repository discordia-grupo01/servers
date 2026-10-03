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
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/service"
)

func serverToDelete() model.Server {
	id := uuid.New()
	icon := "/v1/servers/" + id.String() + "/icon?v=iconhash"
	banner := "/v1/servers/" + id.String() + "/banner?v=bannerhash"
	return model.Server{ID: id, Name: "Mi Comunidad", NameKey: "mi comunidad", OwnerID: testOwner, IconURL: &icon, BannerURL: &banner}
}

func deleteServer(t *testing.T, repo *fakeRepoHelper, icons *fakeIconsHelper, p service.DeleteServerParams) error {
	t.Helper()
	return service.New(repo, icons, repo).DeleteServer(context.Background(), p)
}

func TestDeleteServer_OwnerConfirmingTheName_Deletes(t *testing.T) {
	srv := serverToDelete()
	repo, icons := &fakeRepoHelper{getResult: srv}, &fakeIconsHelper{}

	err := deleteServer(t, repo, icons, service.DeleteServerParams{ServerID: srv.ID, ActorUserID: testOwner, ConfirmName: "Mi Comunidad"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.deleteCalls != 1 {
		t.Errorf("DeleteServer calls = %d, want 1", repo.deleteCalls)
	}
}

func TestDeleteServer_ConfirmationIgnoresSurroundingWhitespace(t *testing.T) {
	srv := serverToDelete()
	repo, icons := &fakeRepoHelper{getResult: srv}, &fakeIconsHelper{}

	err := deleteServer(t, repo, icons, service.DeleteServerParams{ServerID: srv.ID, ActorUserID: testOwner, ConfirmName: "  Mi Comunidad \n"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.deleteCalls != 1 {
		t.Errorf("DeleteServer calls = %d, want 1", repo.deleteCalls)
	}
}

func TestDeleteServer_NonOwnerIsRejected(t *testing.T) {
	cases := map[string]role.Standing{
		"plain member":  {IsMember: true},
		"administrator": {IsMember: true, Permissions: role.PermAdministrator},
		"manage server": {IsMember: true, Permissions: role.PermManageServer},
		"outsider":      {},
	}
	for name, standing := range cases {
		t.Run(name, func(t *testing.T) {
			srv := serverToDelete()
			repo, icons := &fakeRepoHelper{getResult: srv, standing: standing}, &fakeIconsHelper{}

			err := deleteServer(t, repo, icons, service.DeleteServerParams{ServerID: srv.ID, ActorUserID: "someone-else", ConfirmName: "Mi Comunidad"})
			if !errors.Is(err, model.ErrForbiddenNotOwner) {
				t.Fatalf("err = %v, want ErrForbiddenNotOwner", err)
			}
			if repo.deleteCalls != 0 || icons.deleteCalled {
				t.Error("a non-owner must not delete anything")
			}
		})
	}
}

func TestDeleteServer_NonOwnerGetsForbiddenBeforeLearningWhetherTheNameMatches(t *testing.T) {
	srv := serverToDelete()
	repo, icons := &fakeRepoHelper{getResult: srv}, &fakeIconsHelper{}

	err := deleteServer(t, repo, icons, service.DeleteServerParams{ServerID: srv.ID, ActorUserID: "someone-else", ConfirmName: "wrong"})
	if !errors.Is(err, model.ErrForbiddenNotOwner) {
		t.Fatalf("err = %v, want ErrForbiddenNotOwner", err)
	}
}

func TestDeleteServer_WithoutConfirmation_DoesNotDelete(t *testing.T) {
	cases := map[string]string{
		"empty":            "",
		"blank":            "   ",
		"different name":   "Otra Comunidad",
		"different case":   "mi comunidad",
		"partial":          "Mi Comun",
		"name plus suffix": "Mi Comunidad!",
	}
	for name, confirm := range cases {
		t.Run(name, func(t *testing.T) {
			srv := serverToDelete()
			repo, icons := &fakeRepoHelper{getResult: srv}, &fakeIconsHelper{}

			err := deleteServer(t, repo, icons, service.DeleteServerParams{ServerID: srv.ID, ActorUserID: testOwner, ConfirmName: confirm})
			if !errors.Is(err, model.ErrDeleteConfirmationMismatch) {
				t.Fatalf("err = %v, want ErrDeleteConfirmationMismatch", err)
			}
			if repo.deleteCalls != 0 || icons.deleteCalled {
				t.Error("an unconfirmed deletion must not touch anything")
			}
		})
	}
}

func TestDeleteServer_RequiresAnAuthenticatedActor(t *testing.T) {
	srv := serverToDelete()
	repo, icons := &fakeRepoHelper{getResult: srv}, &fakeIconsHelper{}

	err := deleteServer(t, repo, icons, service.DeleteServerParams{ServerID: srv.ID, ActorUserID: "", ConfirmName: "Mi Comunidad"})
	if !errors.Is(err, model.ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
	if repo.getCalls != 0 || repo.deleteCalls != 0 {
		t.Error("an anonymous call must not reach the repository")
	}
}

func TestDeleteServer_UnknownServer(t *testing.T) {
	repo, icons := &fakeRepoHelper{getErr: model.ErrServerNotFound}, &fakeIconsHelper{}

	err := deleteServer(t, repo, icons, service.DeleteServerParams{ServerID: uuid.New(), ActorUserID: testOwner, ConfirmName: "x"})
	if !errors.Is(err, model.ErrServerNotFound) {
		t.Fatalf("err = %v, want ErrServerNotFound", err)
	}
	if repo.deleteCalls != 0 {
		t.Error("DeleteServer must not be called for an unknown server")
	}
}

func TestDeleteServer_ServerDeletedConcurrently(t *testing.T) {
	srv := serverToDelete()
	repo, icons := &fakeRepoHelper{getResult: srv, deleteErr: model.ErrServerNotFound}, &fakeIconsHelper{}

	err := deleteServer(t, repo, icons, service.DeleteServerParams{ServerID: srv.ID, ActorUserID: testOwner, ConfirmName: "Mi Comunidad"})
	if !errors.Is(err, model.ErrServerNotFound) {
		t.Fatalf("err = %v, want ErrServerNotFound", err)
	}
	if icons.deleteCalled {
		t.Error("images must not be removed when the deletion did not happen")
	}
}

func TestDeleteServer_RepositoryFailureKeepsTheImages(t *testing.T) {
	srv := serverToDelete()
	boom := errors.New("db down")
	repo, icons := &fakeRepoHelper{getResult: srv, deleteErr: boom}, &fakeIconsHelper{}

	err := deleteServer(t, repo, icons, service.DeleteServerParams{ServerID: srv.ID, ActorUserID: testOwner, ConfirmName: "Mi Comunidad"})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the repository error", err)
	}
	if icons.deleteCalled {
		t.Error("images must survive a failed deletion")
	}
}

func TestDeleteServer_RemovesIconAndBannerAfterDeleting(t *testing.T) {
	srv := serverToDelete()
	repo, icons := &fakeRepoHelper{getResult: srv}, &fakeIconsHelper{}

	if err := deleteServer(t, repo, icons, service.DeleteServerParams{ServerID: srv.ID, ActorUserID: testOwner, ConfirmName: "Mi Comunidad"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(icons.deletedKeys) != 2 {
		t.Fatalf("deleted %d images, want 2 (icon and banner): %v", len(icons.deletedKeys), icons.deletedKeys)
	}
}

func TestDeleteServer_ServerWithoutImagesDeletesWithoutTouchingTheStore(t *testing.T) {
	srv := model.Server{ID: uuid.New(), Name: "Mi Comunidad", OwnerID: testOwner}
	repo, icons := &fakeRepoHelper{getResult: srv}, &fakeIconsHelper{}

	if err := deleteServer(t, repo, icons, service.DeleteServerParams{ServerID: srv.ID, ActorUserID: testOwner, ConfirmName: "Mi Comunidad"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if icons.deleteCalled {
		t.Error("there was nothing to remove from the icon store")
	}
}

func TestDeleteServer_PublishesMemberLeftPerMemberThenServerDeleted(t *testing.T) {
	srv := serverToDelete()
	ch1, ch2 := uuid.New(), uuid.New()
	repo := &fakeRepoHelper{
		getResult:       srv,
		deleteMemberIDs: []string{testOwner, "user-2", "user-3"},
		deleteChannels:  []uuid.UUID{ch1, ch2},
	}

	if err := deleteServer(t, repo, &fakeIconsHelper{}, service.DeleteServerParams{ServerID: srv.ID, ActorUserID: testOwner, ConfirmName: "Mi Comunidad"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(repo.deleteEvents) != 4 {
		t.Fatalf("events = %d, want 3 member_left + 1 server_deleted", len(repo.deleteEvents))
	}

	left := map[string]bool{}
	for _, e := range repo.deleteEvents[:3] {
		if e.RoutingKey != membership.RoutingKeyMemberLeft {
			t.Fatalf("routing key = %q, want %q", e.RoutingKey, membership.RoutingKeyMemberLeft)
		}
		var payload membership.MemberLeft
		if err := json.Unmarshal(e.Payload, &payload); err != nil {
			t.Fatalf("member_left payload: %v", err)
		}
		if payload.ServerID != srv.ID.String() || payload.EventID == "" || payload.OccurredAt.IsZero() {
			t.Errorf("member_left = %+v", payload)
		}
		left[payload.UserID] = true
	}
	for _, u := range []string{testOwner, "user-2", "user-3"} {
		if !left[u] {
			t.Errorf("no member_left for %s", u)
		}
	}

	last := repo.deleteEvents[3]
	if last.RoutingKey != servers.RoutingKeyServerDeleted {
		t.Fatalf("last routing key = %q, want %q", last.RoutingKey, servers.RoutingKeyServerDeleted)
	}
	var deleted servers.ServerDeleted
	if err := json.Unmarshal(last.Payload, &deleted); err != nil {
		t.Fatalf("server_deleted payload: %v", err)
	}
	if deleted.ServerID != srv.ID.String() || deleted.EventID == "" {
		t.Errorf("server_deleted = %+v", deleted)
	}
	if len(deleted.ChannelIDs) != 2 || deleted.ChannelIDs[0] != ch1.String() || deleted.ChannelIDs[1] != ch2.String() {
		t.Errorf("channel_ids = %v, want [%s %s]", deleted.ChannelIDs, ch1, ch2)
	}
}

func TestDeleteServer_ServerWithoutChannelsPublishesAnEmptyChannelList(t *testing.T) {
	srv := serverToDelete()
	repo := &fakeRepoHelper{getResult: srv, deleteMemberIDs: []string{testOwner}}

	if err := deleteServer(t, repo, &fakeIconsHelper{}, service.DeleteServerParams{ServerID: srv.ID, ActorUserID: testOwner, ConfirmName: "Mi Comunidad"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	last := repo.deleteEvents[len(repo.deleteEvents)-1]
	var raw map[string]any
	if err := json.Unmarshal(last.Payload, &raw); err != nil {
		t.Fatal(err)
	}
	if ids, ok := raw["channel_ids"].([]any); !ok || len(ids) != 0 {
		t.Errorf("channel_ids = %v, want an empty list (never null)", raw["channel_ids"])
	}
}
