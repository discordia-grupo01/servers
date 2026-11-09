package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/discordia-grupo01/servers/internal/logging"
	"github.com/discordia-grupo01/servers/internal/telemetry"
)

type Publisher interface {
	PublishOrError(ctx context.Context, routingKey string, payload any) error
}

const PollInterval = 2 * time.Second

// maxPerTick bounds how many events one tick publishes. With a backlog the
// poller keeps going instead of waiting for the next tick: a cap of 20 per 2s
// tick limited the whole service to 10 events/s.
const maxPerTick = 2000

type Poller struct {
	pool      *pgxpool.Pool
	publisher Publisher
}

func NewPoller(pool *pgxpool.Pool, publisher Publisher) *Poller {
	return &Poller{pool: pool, publisher: publisher}
}

func (p *Poller) Start(ctx context.Context) {
	ticker := time.NewTicker(PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.drain(ctx)
		}
	}
}

func (p *Poller) drain(ctx context.Context) {
	for i := 0; i < maxPerTick && ctx.Err() == nil; i++ {
		published, err := p.publishOne(ctx)
		if err != nil {
			logging.Errorf("outbox: poll tick failed: %v", err)
			return
		}
		if !published {
			return
		}
	}
}

func (p *Poller) publishOne(ctx context.Context) (published bool, err error) {
	err = pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		var id uuid.UUID
		var routingKey string
		var payload []byte
		var traceparent *string
		row := tx.QueryRow(ctx, `
			SELECT id, routing_key, payload, traceparent
			  FROM outbox_events
			 WHERE published_at IS NULL
			 ORDER BY created_at
			 LIMIT 1
			   FOR UPDATE SKIP LOCKED`)
		if err := row.Scan(&id, &routingKey, &payload, &traceparent); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			return fmt.Errorf("select pending event: %w", err)
		}
		published = true

		publishCtx := ctx
		if traceparent != nil {
			publishCtx = telemetry.ContextWithTraceparent(ctx, *traceparent)
		}
		publishCtx, span := otel.Tracer("github.com/discordia-grupo01/servers").Start(publishCtx, "outbox publish "+routingKey,
			trace.WithSpanKind(trace.SpanKindProducer),
			trace.WithAttributes(
				attribute.String("messaging.system", "rabbitmq"),
				attribute.String("messaging.destination.name", routingKey),
			),
		)
		pubErr := p.publisher.PublishOrError(publishCtx, routingKey, json.RawMessage(payload))
		if pubErr != nil {
			span.SetStatus(codes.Error, "publish failed")
		}
		span.End()

		if pubErr != nil {
			logging.Warnf("outbox: publish %q (id=%s) failed, will retry next tick: %v", routingKey, id, pubErr)
			_, err := tx.Exec(ctx, `
				UPDATE outbox_events SET attempts = attempts + 1, last_error = $2 WHERE id = $1`,
				id, pubErr.Error(),
			)
			return err
		}

		_, err := tx.Exec(ctx, `UPDATE outbox_events SET published_at = now() WHERE id = $1`, id)
		return err
	})
	return published, err
}
