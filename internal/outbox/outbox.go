package outbox

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

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
	_, err := tx.Exec(ctx, `
		INSERT INTO outbox_events (id, routing_key, payload)
		VALUES ($1, $2, $3)`,
		uuid.New(), evt.RoutingKey, evt.Payload,
	)
	if err != nil {
		return fmt.Errorf("outbox: insert event %q: %w", evt.RoutingKey, err)
	}
	return nil
}
