package word_filter_service_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"

	moderationevents "github.com/discordia-grupo01/events-contract/moderation"
	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/moderation"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/outbox"
	"github.com/discordia-grupo01/servers/internal/service/word_filter_service"
)

const (
	ownerID     = "owner-1"
	moderatorID = "mod-1"
	memberID    = "member-1"
)

type fakeServers struct{ server model.Server }

func (f *fakeServers) GetByID(context.Context, uuid.UUID) (model.Server, error) {
	return f.server, nil
}

type fakeStandings struct{ byUser map[string]role.Standing }

func (f *fakeStandings) MemberStanding(_ context.Context, _ uuid.UUID, userID string) (role.Standing, error) {
	return f.byUser[userID], nil
}

type fakeRepo struct {
	words        []string
	replaceCalls int
	saved        []string
	events       []outbox.Event
}

func (f *fakeRepo) List(context.Context, uuid.UUID) ([]string, error) { return f.words, nil }

func (f *fakeRepo) Replace(_ context.Context, _ uuid.UUID, words []string, events ...outbox.Event) error {
	f.replaceCalls++
	f.saved = words
	f.events = events
	return nil
}

func newService() (*word_filter_service.Service, *fakeRepo, uuid.UUID) {
	serverID := uuid.New()
	repo := &fakeRepo{words: []string{"tonto"}}
	standings := &fakeStandings{byUser: map[string]role.Standing{
		moderatorID: {IsMember: true, Permissions: role.DefaultPermissions | role.PermManageMessages},
		memberID:    {IsMember: true, Permissions: role.DefaultPermissions},
		"admin-1":   {IsMember: true, Permissions: role.PermAdministrator},
	}}
	svc := word_filter_service.New(&fakeServers{server: model.Server{ID: serverID, OwnerID: ownerID}}, standings, repo)
	return svc, repo, serverID
}

func TestReplace_CA2_SavesTheNormalizedListAndPublishesIt(t *testing.T) {
	svc, repo, serverID := newService()

	got, err := svc.Replace(context.Background(), word_filter_service.ReplaceParams{
		ServerID: serverID, ActorUserID: moderatorID, Words: []string{" Tonto", "no  sirve", "tonto"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"tonto", "no sirve"}
	if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(repo.saved, want) {
		t.Fatalf("returned %q, saved %q, want %q", got, repo.saved, want)
	}
	if len(repo.events) != 1 || repo.events[0].RoutingKey != moderationevents.RoutingKeyWordFilterUpdated {
		t.Fatalf("events = %+v", repo.events)
	}
	var evt moderationevents.WordFilterUpdated
	if err := json.Unmarshal(repo.events[0].Payload, &evt); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if evt.ServerID != serverID.String() || !reflect.DeepEqual(evt.Words, want) || evt.EventID == "" || evt.OccurredAt.IsZero() {
		t.Errorf("event = %+v", evt)
	}
}

func TestReplace_AnEmptyListRemovesTheFilterAndPublishesAnEmptyArray(t *testing.T) {
	svc, repo, serverID := newService()

	if _, err := svc.Replace(context.Background(), word_filter_service.ReplaceParams{
		ServerID: serverID, ActorUserID: moderatorID, Words: []string{},
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var payload map[string]json.RawMessage
	_ = json.Unmarshal(repo.events[0].Payload, &payload)
	if string(payload["words"]) != "[]" {
		t.Errorf("words = %s, want []", payload["words"])
	}
}

func TestReplaceAndList_OwnerAndAdministratorsCanWithoutTheGrant(t *testing.T) {
	for _, actor := range []string{ownerID, "admin-1"} {
		svc, repo, serverID := newService()
		if _, err := svc.Replace(context.Background(), word_filter_service.ReplaceParams{
			ServerID: serverID, ActorUserID: actor, Words: []string{"x"},
		}); err != nil || repo.replaceCalls != 1 {
			t.Errorf("%s: replace err = %v, calls = %d", actor, err, repo.replaceCalls)
		}
		if got, err := svc.List(context.Background(), word_filter_service.ListParams{ServerID: serverID, ActorUserID: actor}); err != nil || len(got) != 1 {
			t.Errorf("%s: list = %q, %v", actor, got, err)
		}
	}
}

func TestReplaceAndList_RequireManageMessages(t *testing.T) {
	for _, actor := range []string{memberID, "stranger"} {
		svc, repo, serverID := newService()

		_, err := svc.Replace(context.Background(), word_filter_service.ReplaceParams{
			ServerID: serverID, ActorUserID: actor, Words: []string{"x"},
		})
		if !errors.Is(err, model.ErrForbiddenMissingPermission) || repo.replaceCalls != 0 {
			t.Errorf("%s: replace err = %v, calls = %d", actor, err, repo.replaceCalls)
		}
		if _, err := svc.List(context.Background(), word_filter_service.ListParams{ServerID: serverID, ActorUserID: actor}); !errors.Is(err, model.ErrForbiddenMissingPermission) {
			t.Errorf("%s: list err = %v", actor, err)
		}
	}
}

func TestReplace_InvalidWordsSaveNothing(t *testing.T) {
	svc, repo, serverID := newService()

	_, err := svc.Replace(context.Background(), word_filter_service.ReplaceParams{
		ServerID: serverID, ActorUserID: moderatorID, Words: []string{"ok", " "},
	})
	if !errors.Is(err, moderation.ErrBannedWordEmpty) || repo.replaceCalls != 0 {
		t.Errorf("err = %v, calls = %d", err, repo.replaceCalls)
	}
}

func TestReplace_RequiresAUser(t *testing.T) {
	svc, _, serverID := newService()
	if _, err := svc.Replace(context.Background(), word_filter_service.ReplaceParams{ServerID: serverID, Words: []string{}}); !errors.Is(err, model.ErrUnauthorized) {
		t.Errorf("err = %v", err)
	}
}
