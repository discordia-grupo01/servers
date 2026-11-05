package emoji_repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/emoji"
	"github.com/discordia-grupo01/servers/internal/outbox"
)

const (
	pgUniqueViolation = "23505"
	activeNameIndex   = "uq_server_emojis_active_name"
	emojiColumns      = `id, server_id, name, animated, image_url, created_by, created_at, deleted_at, deleted_by`
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// Create locks the server row so two concurrent uploads can't both take the
// last free slot.
func (r *Repository) Create(ctx context.Context, e emoji.Emoji, maxEmojis int, evt outbox.Event) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var locked int
		err := tx.QueryRow(ctx, `SELECT 1 FROM servers WHERE id = $1 FOR NO KEY UPDATE`, e.ServerID).Scan(&locked)
		if errors.Is(err, pgx.ErrNoRows) {
			return model.ErrServerNotFound
		}
		if err != nil {
			return fmt.Errorf("lock server: %w", err)
		}

		var used int
		if err := tx.QueryRow(ctx,
			`SELECT COUNT(*) FROM server_emojis WHERE server_id = $1 AND deleted_at IS NULL`, e.ServerID,
		).Scan(&used); err != nil {
			return fmt.Errorf("count emojis: %w", err)
		}
		if used >= maxEmojis {
			return model.ErrEmojiLimitReached
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO server_emojis (id, server_id, name, animated, image_url, created_by, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			e.ID, e.ServerID, e.Name, e.Animated, e.ImageURL, e.CreatedBy, e.CreatedAt,
		); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation && pgErr.ConstraintName == activeNameIndex {
				return model.ErrEmojiNameTaken
			}
			return fmt.Errorf("insert emoji: %w", err)
		}
		return outbox.Insert(ctx, tx, evt)
	})
}

func (r *Repository) List(ctx context.Context, serverID uuid.UUID) ([]emoji.Emoji, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+emojiColumns+`
		  FROM server_emojis
		 WHERE server_id = $1 AND deleted_at IS NULL
		 ORDER BY created_at, id`, serverID)
	if err != nil {
		return nil, fmt.Errorf("list emojis: %w", err)
	}
	defer rows.Close()

	emojis := make([]emoji.Emoji, 0)
	for rows.Next() {
		e, err := scanEmoji(rows)
		if err != nil {
			return nil, fmt.Errorf("scan emoji: %w", err)
		}
		emojis = append(emojis, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list emojis: %w", err)
	}
	return emojis, nil
}

func (r *Repository) Delete(
	ctx context.Context,
	serverID, emojiID uuid.UUID,
	by string,
	at time.Time,
	buildEvent func(emoji.Emoji) (outbox.Event, error),
) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		deleted, err := scanEmoji(tx.QueryRow(ctx, `
			UPDATE server_emojis
			   SET deleted_at = $3, deleted_by = $4
			 WHERE id = $1 AND server_id = $2 AND deleted_at IS NULL
			RETURNING `+emojiColumns, emojiID, serverID, at, by))
		if errors.Is(err, pgx.ErrNoRows) {
			return model.ErrEmojiNotFound
		}
		if err != nil {
			return fmt.Errorf("delete emoji: %w", err)
		}
		evt, err := buildEvent(deleted)
		if err != nil {
			return err
		}
		return outbox.Insert(ctx, tx, evt)
	})
}

func scanEmoji(row pgx.Row) (emoji.Emoji, error) {
	var e emoji.Emoji
	err := row.Scan(&e.ID, &e.ServerID, &e.Name, &e.Animated, &e.ImageURL, &e.CreatedBy, &e.CreatedAt, &e.DeletedAt, &e.DeletedBy)
	return e, err
}
