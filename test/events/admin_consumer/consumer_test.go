package admin_consumer_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/events-contract/admin"
	"github.com/discordia-grupo01/events-contract/envelope"
	"github.com/discordia-grupo01/servers/internal/events/admin_consumer"
)

const secret = "shared-secret"

type fakeStore struct {
	applied   bool
	err       error
	calls     int
	userID    string
	suspended bool
	version   int64
}

func (f *fakeStore) Apply(_ context.Context, userID string, suspended bool, version int64) (bool, error) {
	f.calls++
	f.userID, f.suspended, f.version = userID, suspended, version
	return f.applied, f.err
}

func meta() envelope.Meta {
	return envelope.Meta{EventID: uuid.NewString(), OccurredAt: time.Now().UTC()}
}

func suspendedEvent(userID string, version int64) admin.UserSuspended {
	e := admin.UserSuspended{Meta: meta(), UserID: userID, Version: version}
	e.Sign(secret)
	return e
}

func reactivatedEvent(userID string, version int64) admin.UserReactivated {
	e := admin.UserReactivated{Meta: meta(), UserID: userID, Version: version}
	e.Sign(secret)
	return e
}

func body(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func TestShouldRequeue_AppliesSignedSuspension(t *testing.T) {
	store := &fakeStore{applied: true}
	evt := suspendedEvent("user-1", 3)

	if admin_consumer.ShouldRequeue(context.Background(), store, secret, admin.RoutingKeyUserSuspended, body(t, evt)) {
		t.Fatal("ShouldRequeue = true, want false (ack)")
	}
	if store.calls != 1 || store.userID != "user-1" || !store.suspended || store.version != 3 {
		t.Errorf("store = %+v, want Apply(user-1, true, 3) once", store)
	}
}

func TestShouldRequeue_AppliesSignedReactivation(t *testing.T) {
	store := &fakeStore{applied: true}
	evt := reactivatedEvent("user-1", 4)

	if admin_consumer.ShouldRequeue(context.Background(), store, secret, admin.RoutingKeyUserReactivated, body(t, evt)) {
		t.Fatal("ShouldRequeue = true, want false (ack)")
	}
	if store.calls != 1 || store.userID != "user-1" || store.suspended || store.version != 4 {
		t.Errorf("store = %+v, want Apply(user-1, false, 4) once", store)
	}
}

// A duplicate or out-of-order event is acked without retrying.
func TestShouldRequeue_StaleVersionAcks(t *testing.T) {
	store := &fakeStore{applied: false}
	if admin_consumer.ShouldRequeue(context.Background(), store, secret, admin.RoutingKeyUserSuspended, body(t, suspendedEvent("user-1", 1))) {
		t.Fatal("ShouldRequeue = true, want false (ack)")
	}
	if store.calls != 1 {
		t.Errorf("Apply calls = %d, want 1", store.calls)
	}
}

// A database failure is transient: the message is retried.
func TestShouldRequeue_StoreErrorRequeues(t *testing.T) {
	store := &fakeStore{err: errors.New("db down")}
	if !admin_consumer.ShouldRequeue(context.Background(), store, secret, admin.RoutingKeyUserSuspended, body(t, suspendedEvent("user-1", 1))) {
		t.Fatal("ShouldRequeue = false, want true (requeue)")
	}
}

// Messages that can never succeed are dropped (acked) without touching the
// store, so they don't loop forever and never change any state.
func TestShouldRequeue_DropsUnverifiableOrInvalidMessages(t *testing.T) {
	unsigned := suspendedEvent("user-1", 1)
	unsigned.Signature = ""
	badSignature := suspendedEvent("user-1", 1)
	badSignature.Signature = "deadbeef"
	notHex := suspendedEvent("user-1", 1)
	notHex.Signature = "not-hex"
	otherSecret := admin.UserSuspended{Meta: meta(), UserID: "user-1", Version: 1}
	otherSecret.Sign("another-secret")
	tampered := suspendedEvent("user-1", 1)
	tampered.UserID = "user-2"
	// A valid suspension replayed under the reactivation routing key.
	replayed := suspendedEvent("user-1", 1)

	emptyUser := suspendedEvent("", 1)
	zeroVersion := suspendedEvent("user-1", 0)
	negativeVersion := suspendedEvent("user-1", -1)

	cases := []struct {
		name       string
		routingKey string
		body       []byte
	}{
		{"unknown routing key", "admin.something_else", body(t, suspendedEvent("user-1", 1))},
		{"malformed json", admin.RoutingKeyUserSuspended, []byte("{not json")},
		{"missing signature", admin.RoutingKeyUserSuspended, body(t, unsigned)},
		{"wrong signature", admin.RoutingKeyUserSuspended, body(t, badSignature)},
		{"signature not hex", admin.RoutingKeyUserSuspended, body(t, notHex)},
		{"signed with another secret", admin.RoutingKeyUserSuspended, body(t, otherSecret)},
		{"tampered user_id", admin.RoutingKeyUserSuspended, body(t, tampered)},
		{"suspension replayed as reactivation", admin.RoutingKeyUserReactivated, body(t, replayed)},
		{"empty user_id", admin.RoutingKeyUserSuspended, body(t, emptyUser)},
		{"version 0", admin.RoutingKeyUserSuspended, body(t, zeroVersion)},
		{"negative version", admin.RoutingKeyUserSuspended, body(t, negativeVersion)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeStore{applied: true}
			if admin_consumer.ShouldRequeue(context.Background(), store, secret, tc.routingKey, tc.body) {
				t.Fatal("ShouldRequeue = true, want false (drop)")
			}
			if store.calls != 0 {
				t.Errorf("Apply calls = %d, want 0", store.calls)
			}
		})
	}
}

// An empty secret never verifies, so nothing is applied even if the event was
// signed with an empty one.
func TestShouldRequeue_EmptySecretDropsEverything(t *testing.T) {
	evt := admin.UserSuspended{Meta: meta(), UserID: "user-1", Version: 1}
	evt.Sign("")
	store := &fakeStore{applied: true}

	if admin_consumer.ShouldRequeue(context.Background(), store, "", admin.RoutingKeyUserSuspended, body(t, evt)) {
		t.Fatal("ShouldRequeue = true, want false (drop)")
	}
	if store.calls != 0 {
		t.Errorf("Apply calls = %d, want 0", store.calls)
	}
}
