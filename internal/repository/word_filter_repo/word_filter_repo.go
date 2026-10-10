// Package word_filter_repo implements Postgres access for a server's banned
// words list.
package word_filter_repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/outbox"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// List returns the server's banned words in the order they were saved.
func (r *Repository) List(ctx context.Context, serverID uuid.UUID) ([]string, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT word FROM server_banned_words WHERE server_id = $1 ORDER BY position`,
		serverID,
	)
	if err != nil {
		return nil, fmt.Errorf("list banned words: %w", err)
	}
	words, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("scan banned words: %w", err)
	}
	return words, nil
}

// Replace swaps the whole list and enqueues events in the same transaction.
// The server row is locked so concurrent replaces commit, and publish, in
// order.
func (r *Repository) Replace(ctx context.Context, serverID uuid.UUID, words []string, events ...outbox.Event) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var id uuid.UUID
		err := tx.QueryRow(ctx, `SELECT id FROM servers WHERE id = $1 FOR UPDATE`, serverID).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return model.ErrServerNotFound
		}
		if err != nil {
			return fmt.Errorf("lock server: %w", err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM server_banned_words WHERE server_id = $1`, serverID); err != nil {
			return fmt.Errorf("delete banned words: %w", err)
		}
		if len(words) > 0 {
			if _, err := tx.Exec(ctx, `
				INSERT INTO server_banned_words (server_id, word, position)
				SELECT $1, w.word, w.position
				  FROM unnest($2::text[]) WITH ORDINALITY AS w(word, position)`,
				serverID, words,
			); err != nil {
				return fmt.Errorf("insert banned words: %w", err)
			}
		}
		for _, evt := range events {
			if err := outbox.Insert(ctx, tx, evt); err != nil {
				return err
			}
		}
		return nil
	})
}
