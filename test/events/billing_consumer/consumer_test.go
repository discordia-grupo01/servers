package billing_consumer_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/events-contract/boosts"
	"github.com/discordia-grupo01/events-contract/envelope"
	"github.com/discordia-grupo01/servers/internal/events/billing_consumer"
	"github.com/discordia-grupo01/servers/internal/model"
)

const secret = "test-secret"

type fakeStore struct {
	err   error
	calls []string
	boost model.Boost
}

func (f *fakeStore) record(step string, b model.Boost) error {
	f.calls = append(f.calls, step)
	f.boost = b
	return f.err
}

func (f *fakeStore) Request(_ context.Context, b model.Boost) error { return f.record("request", b) }
func (f *fakeStore) Cancel(_ context.Context, b model.Boost) error  { return f.record("cancel", b) }
func (f *fakeStore) Remove(_ context.Context, b model.Boost) error  { return f.record("remove", b) }

func signed(t *testing.T, routingKey string, change func(*boosts.SagaEvent)) []byte {
	t.Helper()
	evt := boosts.SagaEvent{
		Meta:     envelope.Meta{EventID: uuid.NewString(), OccurredAt: time.Now()},
		BoostID:  uuid.NewString(),
		ServerID: uuid.NewString(),
		UserID:   "user-1",
	}
	if change != nil {
		change(&evt)
	}
	evt.Sign(routingKey, secret)
	body, err := json.Marshal(evt)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestShouldRequeue_RoutesEachStep(t *testing.T) {
	for routingKey, want := range map[string]string{
		boosts.RoutingKeyRequested: "request",
		boosts.RoutingKeyCancelled: "cancel",
		boosts.RoutingKeyRemoved:   "remove",
	} {
		store := &fakeStore{}
		if billing_consumer.ShouldRequeue(context.Background(), store, secret, routingKey, signed(t, routingKey, nil)) {
			t.Errorf("%s: requeued", routingKey)
		}
		if len(store.calls) != 1 || store.calls[0] != want {
			t.Errorf("%s: calls = %v, want %s", routingKey, store.calls, want)
		}
		if store.boost.UserID != "user-1" || store.boost.ID == uuid.Nil || store.boost.ServerID == uuid.Nil {
			t.Errorf("%s: boost = %+v", routingKey, store.boost)
		}
	}
}

func TestShouldRequeue_DropsWhatCanNeverSucceed(t *testing.T) {
	requested := boosts.RoutingKeyRequested
	tests := map[string]struct {
		secret     string
		routingKey string
		body       []byte
	}{
		"forged signature": {"another-secret", requested, signed(t, requested, nil)},
		"no secret":        {"", requested, signed(t, requested, nil)},
		// A signed "removed" must not count as a "requested".
		"replayed under another step": {secret, requested, signed(t, boosts.RoutingKeyRemoved, nil)},
		"unknown routing key":         {secret, "billing.other", signed(t, "billing.other", nil)},
		"malformed":                   {secret, requested, []byte("{")},
		"invalid boost id": {secret, requested, signed(t, requested, func(e *boosts.SagaEvent) {
			e.BoostID = "nope"
		})},
		"invalid server id": {secret, requested, signed(t, requested, func(e *boosts.SagaEvent) {
			e.ServerID = "nope"
		})},
		"empty user": {secret, requested, signed(t, requested, func(e *boosts.SagaEvent) {
			e.UserID = ""
		})},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			store := &fakeStore{}
			if billing_consumer.ShouldRequeue(context.Background(), store, tt.secret, tt.routingKey, tt.body) {
				t.Error("requeued a message that can never succeed")
			}
			if len(store.calls) != 0 {
				t.Errorf("store was called: %v", store.calls)
			}
		})
	}
}

func TestShouldRequeue_DatabaseFailureRetries(t *testing.T) {
	store := &fakeStore{err: errors.New("db down")}
	body := signed(t, boosts.RoutingKeyRequested, nil)
	if !billing_consumer.ShouldRequeue(context.Background(), store, secret, boosts.RoutingKeyRequested, body) {
		t.Error("a database failure must be retried, not rejected")
	}
}
