// Package channel_repo implements Postgres access for the Channel domain type.
package channel_repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/channelperm"
	"github.com/discordia-grupo01/servers/internal/outbox"
)

const pgUniqueViolation = "23505"

var nameTakenConstraints = map[string]bool{
	"uq_channels_server_category_name":    true,
	"uq_channels_server_no_category_name": true,
}

func nameTakenErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation && nameTakenConstraints[pgErr.ConstraintName] {
		return model.ErrChannelNameTaken
	}
	return nil
}

type ChannelRepository struct {
	pool *pgxpool.Pool
}

func NewChannelRepository(pool *pgxpool.Pool) *ChannelRepository {
	return &ChannelRepository{pool: pool}
}

func (r *ChannelRepository) Create(ctx context.Context, ch model.Channel, channelCreatedEvent outbox.Event) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO channels (id, server_id, name, kind, position, category_id, topic, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			ch.ID, ch.ServerID, ch.Name, string(ch.Kind), ch.Position, ch.CategoryID, ch.Topic, ch.CreatedAt,
		)
		if err != nil {
			if taken := nameTakenErr(err); taken != nil {
				return taken
			}
			return fmt.Errorf("insert channel: %w", err)
		}
		return outbox.Insert(ctx, tx, channelCreatedEvent)
	})
}

func (r *ChannelRepository) GetByID(ctx context.Context, id uuid.UUID) (model.Channel, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, server_id, name, kind, position, category_id, topic, created_at
		  FROM channels WHERE id = $1`, id)

	var ch model.Channel
	var kind string
	if err := row.Scan(&ch.ID, &ch.ServerID, &ch.Name, &kind, &ch.Position, &ch.CategoryID, &ch.Topic, &ch.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Channel{}, model.ErrChannelNotFound
		}
		return model.Channel{}, fmt.Errorf("select channel: %w", err)
	}
	ch.Kind = model.ChannelKind(kind)
	return ch, nil
}

// Update sets name and topic. Callers pass the channel's current values for
// whichever field they don't want to change (the service layer resolves the
// "leave untouched" default, same as role_service does for UpdateRolePermissions).
func (r *ChannelRepository) Update(ctx context.Context, id uuid.UUID, name string, topic *string) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE channels SET name = $1, topic = $2 WHERE id = $3`,
		name, topic, id,
	)
	if err != nil {
		if taken := nameTakenErr(err); taken != nil {
			return taken
		}
		return fmt.Errorf("update channel: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return model.ErrChannelNotFound
	}
	return nil
}

func (r *ChannelRepository) Delete(ctx context.Context, id uuid.UUID, channelDeletedEvent outbox.Event) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM channels WHERE id = $1`, id)
		if err != nil {
			return fmt.Errorf("delete channel: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return model.ErrChannelNotFound
		}
		return outbox.Insert(ctx, tx, channelDeletedEvent)
	})
}

func (r *ChannelRepository) MoveToCategory(ctx context.Context, id uuid.UUID, categoryID *uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `UPDATE channels SET category_id = $1 WHERE id = $2`, categoryID, id)
	if err != nil {
		if taken := nameTakenErr(err); taken != nil {
			return taken
		}
		return fmt.Errorf("move channel to category: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return model.ErrChannelNotFound
	}
	return nil
}

// ListByServerAndCategory lists the channels of serverID currently in
// categoryID (nil meaning "no category"), ordered by position.
func (r *ChannelRepository) ListByServerAndCategory(ctx context.Context, serverID uuid.UUID, categoryID *uuid.UUID) ([]model.Channel, error) {
	var rows pgx.Rows
	var err error
	if categoryID == nil {
		rows, err = r.pool.Query(ctx, `
			SELECT id, server_id, name, kind, position, category_id, topic, created_at
			  FROM channels WHERE server_id = $1 AND category_id IS NULL ORDER BY position, id`, serverID)
	} else {
		rows, err = r.pool.Query(ctx, `
			SELECT id, server_id, name, kind, position, category_id, topic, created_at
			  FROM channels WHERE server_id = $1 AND category_id = $2 ORDER BY position, id`, serverID, *categoryID)
	}
	if err != nil {
		return nil, fmt.Errorf("list channels by category: %w", err)
	}
	defer rows.Close()

	channels := make([]model.Channel, 0)
	for rows.Next() {
		var ch model.Channel
		var kind string
		if err := rows.Scan(&ch.ID, &ch.ServerID, &ch.Name, &kind, &ch.Position, &ch.CategoryID, &ch.Topic, &ch.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan channel: %w", err)
		}
		ch.Kind = model.ChannelKind(kind)
		channels = append(channels, ch)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list channels by category: %w", err)
	}
	return channels, nil
}

// ReorderPositions reorders the channels of serverID in categoryID (nil
// meaning "no category"). requested is the new order of the bucket's
// channels that aren't in hidden; hidden channels keep their place (#57, see
// channelperm.MergeVisibleOrder). Every channel of the bucket ends up with
// its index as position.
//
// The bucket's channels are locked (FOR UPDATE) and validated inside the
// transaction, so a channel created, moved or deleted concurrently can't
// leave duplicate positions: if requested isn't exactly the bucket's
// non-hidden channels right now, it fails with model.ErrChannelReorderInvalid
// and nothing is written.
func (r *ChannelRepository) ReorderPositions(ctx context.Context, serverID uuid.UUID, categoryID *uuid.UUID, requested []uuid.UUID, hidden map[uuid.UUID]bool) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id FROM channels
			 WHERE server_id = $1 AND category_id IS NOT DISTINCT FROM $2
			 ORDER BY position, id
			 FOR UPDATE`, serverID, categoryID)
		if err != nil {
			return fmt.Errorf("lock channels: %w", err)
		}
		current, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		if err != nil {
			return fmt.Errorf("lock channels: %w", err)
		}

		merged, ok := channelperm.MergeVisibleOrder(current, hidden, requested)
		if !ok {
			return model.ErrChannelReorderInvalid
		}
		for i, id := range merged {
			if _, err := tx.Exec(ctx, `UPDATE channels SET position = $1 WHERE id = $2`, i, id); err != nil {
				return fmt.Errorf("update channel position: %w", err)
			}
		}
		return nil
	})
}
