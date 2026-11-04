package admin_consumer_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/events-contract/admin"
	"github.com/discordia-grupo01/events-contract/envelope"
	"github.com/discordia-grupo01/servers/internal/events/admin_consumer"
)

type fakeServerStore struct {
	applied     bool
	err         error
	calls       int
	serverID    uuid.UUID
	suspendedAt *time.Time
	version     int64
}

func (f *fakeServerStore) ApplyServerModeration(_ context.Context, id uuid.UUID, at *time.Time, version int64) (bool, error) {
	f.calls++
	f.serverID, f.suspendedAt, f.version = id, at, version
	return f.applied, f.err
}

type fakeDeleter struct {
	err   error
	calls int
	id    uuid.UUID
}

func (f *fakeDeleter) DeleteServerByStaff(_ context.Context, id uuid.UUID) error {
	f.calls++
	f.id = id
	return f.err
}

type fakeCache struct{ resets int }

func (f *fakeCache) Reset() { f.resets++ }

type serverFixture struct {
	store   *fakeServerStore
	deleter *fakeDeleter
	cache   *fakeCache
}

func newFixture(applied bool) serverFixture {
	return serverFixture{store: &fakeServerStore{applied: applied}, deleter: &fakeDeleter{}, cache: &fakeCache{}}
}

func (f serverFixture) handlers() admin_consumer.ServerHandlers {
	return admin_consumer.ServerHandlers{Store: f.store, Deleter: f.deleter, Cache: f.cache}
}

func (f serverFixture) process(t *testing.T, key string, payload any) bool {
	t.Helper()
	return admin_consumer.ShouldRequeueServerEvent(context.Background(), f.handlers(), secret, key, body(t, payload))
}

func serverSuspended(serverID string, version int64) admin.ServerSuspended {
	e := admin.ServerSuspended{Meta: meta(), ServerID: serverID, Version: version}
	e.Sign(secret)
	return e
}

func serverReactivated(serverID string, version int64) admin.ServerReactivated {
	e := admin.ServerReactivated{Meta: meta(), ServerID: serverID, Version: version}
	e.Sign(secret)
	return e
}

func deletionRequested(serverID string, version int64) admin.ServerDeletionRequested {
	e := admin.ServerDeletionRequested{Meta: meta(), ServerID: serverID, Version: version}
	e.Sign(secret)
	return e
}

func TestServerEvent_SignedSuspensionIsAppliedWithItsOccurredAt(t *testing.T) {
	f, id := newFixture(true), uuid.New()
	evt := serverSuspended(id.String(), 3)

	if f.process(t, admin.RoutingKeyServerSuspended, evt) {
		t.Fatal("ShouldRequeueServerEvent = true, want false (ack)")
	}
	if f.store.calls != 1 || f.store.serverID != id || f.store.version != 3 {
		t.Fatalf("store = %+v, want ApplyServerModeration(%s, _, 3) once", f.store, id)
	}
	if f.store.suspendedAt == nil || !f.store.suspendedAt.Equal(evt.OccurredAt) {
		t.Errorf("suspendedAt = %v, want the event's occurred_at %v", f.store.suspendedAt, evt.OccurredAt)
	}
	if f.cache.resets != 1 {
		t.Errorf("cache resets = %d, want 1 after applying", f.cache.resets)
	}
	if f.deleter.calls != 0 {
		t.Error("a suspension must not delete anything")
	}
}

func TestServerEvent_SignedReactivationClearsTheSuspension(t *testing.T) {
	f, id := newFixture(true), uuid.New()

	if f.process(t, admin.RoutingKeyServerReactivated, serverReactivated(id.String(), 4)) {
		t.Fatal("ShouldRequeueServerEvent = true, want false (ack)")
	}
	if f.store.calls != 1 || f.store.serverID != id || f.store.version != 4 || f.store.suspendedAt != nil {
		t.Errorf("store = %+v, want ApplyServerModeration(%s, nil, 4) once", f.store, id)
	}
	if f.cache.resets != 1 {
		t.Errorf("cache resets = %d, want 1", f.cache.resets)
	}
}

// A duplicate or out-of-order event is acked and leaves the cache alone.
func TestServerEvent_StaleVersionOrUnknownServerAcksWithoutResettingTheCache(t *testing.T) {
	f := newFixture(false)

	if f.process(t, admin.RoutingKeyServerSuspended, serverSuspended(uuid.NewString(), 1)) {
		t.Fatal("ShouldRequeueServerEvent = true, want false (ack)")
	}
	if f.store.calls != 1 {
		t.Errorf("Apply calls = %d, want 1", f.store.calls)
	}
	if f.cache.resets != 0 {
		t.Errorf("cache resets = %d, want 0 when nothing was applied", f.cache.resets)
	}
}

func TestServerEvent_ApplyWorksWithoutACache(t *testing.T) {
	store := &fakeServerStore{applied: true}
	h := admin_consumer.ServerHandlers{Store: store, Deleter: &fakeDeleter{}}
	evt := serverSuspended(uuid.NewString(), 1)

	if admin_consumer.ShouldRequeueServerEvent(context.Background(), h, secret, admin.RoutingKeyServerSuspended, body(t, evt)) {
		t.Fatal("want ack")
	}
	if store.calls != 1 {
		t.Errorf("Apply calls = %d, want 1", store.calls)
	}
}

func TestServerEvent_SignedDeletionRequestDeletesTheServer(t *testing.T) {
	f, id := newFixture(true), uuid.New()

	if f.process(t, admin.RoutingKeyServerDeletionRequested, deletionRequested(id.String(), 2)) {
		t.Fatal("ShouldRequeueServerEvent = true, want false (ack)")
	}
	if f.deleter.calls != 1 || f.deleter.id != id {
		t.Errorf("deleter = %+v, want DeleteServerByStaff(%s) once", f.deleter, id)
	}
	if f.store.calls != 0 {
		t.Error("a deletion request must not touch the moderation state")
	}
}

// A repeated deletion request is just another call to an idempotent deleter.
func TestServerEvent_DuplicateDeletionRequestIsAcked(t *testing.T) {
	f, id := newFixture(true), uuid.New()
	evt := deletionRequested(id.String(), 2)

	for i := range 2 {
		if f.process(t, admin.RoutingKeyServerDeletionRequested, evt) {
			t.Fatalf("delivery %d: want ack", i+1)
		}
	}
	if f.deleter.calls != 2 {
		t.Errorf("deleter calls = %d, want 2 (idempotency is the deleter's job)", f.deleter.calls)
	}
}

func TestServerEvent_DatabaseFailuresAreRetried(t *testing.T) {
	boom := errors.New("db down")

	t.Run("applying", func(t *testing.T) {
		f := serverFixture{store: &fakeServerStore{err: boom}, deleter: &fakeDeleter{}, cache: &fakeCache{}}
		if !f.process(t, admin.RoutingKeyServerSuspended, serverSuspended(uuid.NewString(), 1)) {
			t.Fatal("ShouldRequeueServerEvent = false, want true (requeue)")
		}
		if f.cache.resets != 0 {
			t.Error("a failed write must not reset the cache")
		}
	})
	t.Run("deleting", func(t *testing.T) {
		f := serverFixture{store: &fakeServerStore{}, deleter: &fakeDeleter{err: boom}, cache: &fakeCache{}}
		if !f.process(t, admin.RoutingKeyServerDeletionRequested, deletionRequested(uuid.NewString(), 1)) {
			t.Fatal("ShouldRequeueServerEvent = false, want true (requeue)")
		}
	})
}

// Whatever can never succeed is dropped, and nothing is suspended or deleted.
// A forged deletion is the worst case: it is irreversible.
func TestServerEvent_DropsUnverifiableOrInvalidMessages(t *testing.T) {
	id := uuid.NewString()

	unsigned := deletionRequested(id, 1)
	unsigned.Signature = ""
	badSignature := deletionRequested(id, 1)
	badSignature.Signature = "deadbeef"
	notHex := deletionRequested(id, 1)
	notHex.Signature = "not-hex"
	otherSecret := admin.ServerDeletionRequested{Meta: meta(), ServerID: id, Version: 1}
	otherSecret.Sign("another-secret")
	tampered := deletionRequested(id, 1)
	tampered.ServerID = uuid.NewString()
	tamperedVersion := deletionRequested(id, 1)
	tamperedVersion.Version = 9

	// A valid suspension replayed as a deletion request, and the other way round.
	susp := serverSuspended(id, 1)
	asDeletion := admin.ServerDeletionRequested{Meta: susp.Meta, ServerID: susp.ServerID, Version: susp.Version, Signature: susp.Signature}
	del := deletionRequested(id, 1)
	asSuspension := admin.ServerSuspended{Meta: del.Meta, ServerID: del.ServerID, Version: del.Version, Signature: del.Signature}
	// An account event can't pass as a server event either.
	user := admin.UserSuspended{Meta: meta(), UserID: id, Version: 1}
	user.Sign(secret)
	userAsServer := admin.ServerSuspended{Meta: user.Meta, ServerID: user.UserID, Version: user.Version, Signature: user.Signature}

	notUUID := deletionRequested("not-a-uuid", 1)
	emptyID := deletionRequested("", 1)
	zeroVersion := deletionRequested(id, 0)
	negativeVersion := deletionRequested(id, -1)

	cases := []struct {
		name    string
		key     string
		payload any
	}{
		{"unsigned deletion", admin.RoutingKeyServerDeletionRequested, unsigned},
		{"bad signature", admin.RoutingKeyServerDeletionRequested, badSignature},
		{"signature not hex", admin.RoutingKeyServerDeletionRequested, notHex},
		{"signed with another secret", admin.RoutingKeyServerDeletionRequested, otherSecret},
		{"tampered server_id", admin.RoutingKeyServerDeletionRequested, tampered},
		{"tampered version", admin.RoutingKeyServerDeletionRequested, tamperedVersion},
		{"suspension replayed as a deletion", admin.RoutingKeyServerDeletionRequested, asDeletion},
		{"deletion replayed as a suspension", admin.RoutingKeyServerSuspended, asSuspension},
		{"user suspension replayed as a server one", admin.RoutingKeyServerSuspended, userAsServer},
		{"server_id not a UUID", admin.RoutingKeyServerDeletionRequested, notUUID},
		{"empty server_id", admin.RoutingKeyServerDeletionRequested, emptyID},
		{"version 0", admin.RoutingKeyServerDeletionRequested, zeroVersion},
		{"negative version", admin.RoutingKeyServerSuspended, negativeVersion},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(true)
			if f.process(t, tc.key, tc.payload) {
				t.Fatal("ShouldRequeueServerEvent = true, want false (drop)")
			}
			if f.store.calls != 0 || f.deleter.calls != 0 || f.cache.resets != 0 {
				t.Errorf("store calls=%d deleter calls=%d cache resets=%d, want all 0", f.store.calls, f.deleter.calls, f.cache.resets)
			}
		})
	}
}

// An empty shared secret verifies nothing, so every event is dropped.
func TestServerEvent_EmptySecretDropsEverything(t *testing.T) {
	f := newFixture(true)
	evt := deletionRequested(uuid.NewString(), 1)

	if admin_consumer.ShouldRequeueServerEvent(context.Background(), f.handlers(), "", admin.RoutingKeyServerDeletionRequested, body(t, evt)) {
		t.Fatal("want drop (ack)")
	}
	if f.deleter.calls != 0 {
		t.Error("nothing may be deleted without a secret to verify against")
	}
}

func TestServerEvent_MalformedJSONAndUnknownKeysAreDropped(t *testing.T) {
	f := newFixture(true)
	for _, key := range []string{admin.RoutingKeyServerSuspended, admin.RoutingKeyServerReactivated, admin.RoutingKeyServerDeletionRequested} {
		if admin_consumer.ShouldRequeueServerEvent(context.Background(), f.handlers(), secret, key, []byte("{not json")) {
			t.Errorf("%s: malformed JSON requeued, want drop", key)
		}
	}
	if admin_consumer.ShouldRequeueServerEvent(context.Background(), f.handlers(), secret, "admin.server_exploded", body(t, serverSuspended(uuid.NewString(), 1))) {
		t.Error("unknown routing key requeued, want drop")
	}
	if f.store.calls != 0 || f.deleter.calls != 0 {
		t.Error("nothing may be applied for malformed or unknown messages")
	}
}

func TestServerEvent_UnverifiedOccurredAtDoesNotLeakIntoState(t *testing.T) {
	// occurred_at is part of the signed text: changing it invalidates the event.
	f := newFixture(true)
	evt := serverSuspended(uuid.NewString(), 1)
	evt.Meta = envelope.Meta{EventID: evt.EventID, OccurredAt: evt.OccurredAt.Add(time.Hour)}

	if f.process(t, admin.RoutingKeyServerSuspended, evt) {
		t.Fatal("want drop")
	}
	if f.store.calls != 0 {
		t.Error("an event with a tampered occurred_at must not be applied")
	}
}
