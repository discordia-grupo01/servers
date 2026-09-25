package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Publisher interface {
	PublishOrError(ctx context.Context, routingKey string, payload any) error
}

const PollInterval = 2 * time.Second

const batchSize = 20

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
	for i := 0; i < batchSize; i++ {
		published, err := p.publishOne(ctx)
		if err != nil {
			log.Printf("outbox: poll tick failed: %v", err)
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
		row := tx.QueryRow(ctx, `
			SELECT id, routing_key, payload
			  FROM outbox_events
			 WHERE published_at IS NULL
			 ORDER BY created_at
			 LIMIT 1
			   FOR UPDATE SKIP LOCKED`)
		if err := row.Scan(&id, &routingKey, &payload); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			return fmt.Errorf("select pending event: %w", err)
		}
		published = true

		if pubErr := p.publisher.PublishOrError(ctx, routingKey, json.RawMessage(payload)); pubErr != nil {
			log.Printf("outbox: publish %q (id=%s) failed, will retry next tick: %v", routingKey, id, pubErr)
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
