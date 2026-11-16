package outbox

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/discordia-grupo01/servers/internal/telemetry"
)

// NotifyChannel is the Postgres channel Insert notifies on commit.
const NotifyChannel = "outbox_events"

type Event struct {
	RoutingKey string
	Payload    []byte
}

func New(routingKey string, payload any) (Event, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return Event{}, fmt.Errorf("outbox: marshal payload for %q: %w", routingKey, err)
	}
	return Event{RoutingKey: routingKey, Payload: body}, nil
}

func Insert(ctx context.Context, tx pgx.Tx, evt Event) error {
	// pg_notify is delivered on commit, so the poller wakes up as soon as the
	// event is visible instead of waiting for the next tick.
	_, err := tx.Exec(ctx, `
		WITH inserted AS (
			INSERT INTO outbox_events (id, routing_key, payload, traceparent)
			VALUES ($1, $2, $3, NULLIF($4, ''))
			RETURNING id
		)
		SELECT pg_notify('`+NotifyChannel+`', '') FROM inserted`,
		uuid.New(), evt.RoutingKey, evt.Payload, telemetry.Traceparent(ctx),
	)
	if err != nil {
		return fmt.Errorf("outbox: insert event %q: %w", evt.RoutingKey, err)
	}
	return nil
}
