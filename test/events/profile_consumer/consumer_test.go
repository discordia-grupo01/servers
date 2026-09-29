package profile_consumer_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/events-contract/envelope"
	"github.com/discordia-grupo01/servers/internal/events/profile_consumer"
	"github.com/discordia-grupo01/servers/internal/model/userprofile"
)

type fakeStore struct {
	applied bool
	err     error
	calls   int
	got     userprofile.Profile
}

func (f *fakeStore) Upsert(_ context.Context, p userprofile.Profile) (bool, error) {
	f.calls++
	f.got = p
	return f.applied, f.err
}

func validEvent() profile_consumer.ProfileUpdated {
	return profile_consumer.ProfileUpdated{
		Meta:   envelope.Meta{EventID: uuid.NewString(), OccurredAt: time.Now().UTC()},
		UserID: uuid.NewString(), Version: 2,
		Name: "Manu", AvatarURL: "https://cdn/a.png", Description: "hi",
		StatusText: "playing", StatusEmoji: "🎮",
	}
}

func body(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func TestShouldRequeue_StoresProfileAndAcks(t *testing.T) {
	store := &fakeStore{applied: true}
	evt := validEvent()

	if profile_consumer.ShouldRequeue(context.Background(), store, profile_consumer.RoutingKeyProfileUpdated, body(t, evt)) {
		t.Fatal("ShouldRequeue = true, want false (ack)")
	}
	want := userprofile.Profile{
		UserID: evt.UserID, Name: evt.Name, AvatarURL: evt.AvatarURL, Description: evt.Description,
		StatusText: evt.StatusText, StatusEmoji: evt.StatusEmoji, Version: evt.Version,
	}
	if store.got != want {
		t.Errorf("stored = %+v, want %+v", store.got, want)
	}
}

// A duplicate or out-of-order event is acked without retrying.
func TestShouldRequeue_StaleVersionAcks(t *testing.T) {
	store := &fakeStore{applied: false}
	if profile_consumer.ShouldRequeue(context.Background(), store, profile_consumer.RoutingKeyProfileUpdated, body(t, validEvent())) {
		t.Fatal("ShouldRequeue = true, want false (ack)")
	}
	if store.calls != 1 {
		t.Errorf("Upsert calls = %d, want 1", store.calls)
	}
}

// A database failure is transient: the message is retried.
func TestShouldRequeue_StoreErrorRequeues(t *testing.T) {
	store := &fakeStore{err: errors.New("db down")}
	if !profile_consumer.ShouldRequeue(context.Background(), store, profile_consumer.RoutingKeyProfileUpdated, body(t, validEvent())) {
		t.Fatal("ShouldRequeue = false, want true (requeue)")
	}
}

// Messages that can never succeed are dropped (acked) without touching the
// store, so they don't loop forever.
func TestShouldRequeue_DropsInvalidMessages(t *testing.T) {
	emptyUser := validEvent()
	emptyUser.UserID = ""
	notUUID := validEvent()
	notUUID.UserID = "not-a-uuid"
	zeroVersion := validEvent()
	zeroVersion.Version = 0
	negativeVersion := validEvent()
	negativeVersion.Version = -1

	cases := []struct {
		name       string
		routingKey string
		body       []byte
	}{
		{"unknown routing key", "users.something_else", body(t, validEvent())},
		{"malformed json", profile_consumer.RoutingKeyProfileUpdated, []byte("{not json")},
		{"empty user_id", profile_consumer.RoutingKeyProfileUpdated, body(t, emptyUser)},
		{"user_id not a uuid", profile_consumer.RoutingKeyProfileUpdated, body(t, notUUID)},
		{"version 0", profile_consumer.RoutingKeyProfileUpdated, body(t, zeroVersion)},
		{"negative version", profile_consumer.RoutingKeyProfileUpdated, body(t, negativeVersion)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeStore{applied: true}
			if profile_consumer.ShouldRequeue(context.Background(), store, tc.routingKey, tc.body) {
				t.Fatal("ShouldRequeue = true, want false (drop)")
			}
			if store.calls != 0 {
				t.Errorf("Upsert calls = %d, want 0", store.calls)
			}
		})
	}
}

// The wire shape is exactly the documented users.profile_updated payload:
// nothing more (in particular, never email) and nothing less.
func TestProfileUpdated_JSONShape(t *testing.T) {
	var got map[string]any
	if err := json.Unmarshal(body(t, validEvent()), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := []string{"event_id", "occurred_at", "user_id", "version", "name", "avatar_url", "description", "status_text", "status_emoji"}
	if len(got) != len(want) {
		t.Errorf("keys = %v, want exactly %v", got, want)
	}
	for _, k := range want {
		if _, ok := got[k]; !ok {
			t.Errorf("missing key %q", k)
		}
	}
}
