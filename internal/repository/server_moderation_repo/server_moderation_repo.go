// Package server_moderation_repo implements Postgres access for the platform
// moderation state of a server (servers.suspended_at / moderation_version): it
// applies admin's events and answers whether a server, or the server a channel,
// category, role or invitation belongs to, is suspended.
package server_moderation_repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// ApplyServerModeration stores the moderation state of a server unless the same
// or a higher version is already stored, and reports whether it was applied.
// suspendedAt is when the server was suspended; nil means it was reactivated.
// Redelivered and out-of-order events are harmless no-ops, in one atomic
// statement. A server that doesn't exist (already deleted) is also a no-op:
// there is nothing left to suspend.
func (r *Repository) ApplyServerModeration(ctx context.Context, serverID uuid.UUID, suspendedAt *time.Time, version int64) (bool, error) {
	var at *time.Time
	if suspendedAt != nil {
		utc := suspendedAt.UTC()
		at = &utc
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE servers
		   SET suspended_at = $2, moderation_version = $3
		 WHERE id = $1 AND moderation_version < $3`,
		serverID, at, version,
	)
	if err != nil {
		return false, fmt.Errorf("apply server moderation: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// SuspendedByServer reports whether the server is suspended. found is false
// when there is no such server.
func (r *Repository) SuspendedByServer(ctx context.Context, serverID uuid.UUID) (suspended, found bool, err error) {
	return r.lookup(ctx, `SELECT suspended_at IS NOT NULL FROM servers WHERE id = $1`, serverID)
}

// SuspendedByChannel is SuspendedByServer for the server that owns the channel.
func (r *Repository) SuspendedByChannel(ctx context.Context, channelID uuid.UUID) (suspended, found bool, err error) {
	return r.lookup(ctx, `
		SELECT s.suspended_at IS NOT NULL
		  FROM channels c JOIN servers s ON s.id = c.server_id
		 WHERE c.id = $1`, channelID)
}

// SuspendedByCategory is SuspendedByServer for the server that owns the category.
func (r *Repository) SuspendedByCategory(ctx context.Context, categoryID uuid.UUID) (suspended, found bool, err error) {
	return r.lookup(ctx, `
		SELECT s.suspended_at IS NOT NULL
		  FROM categories c JOIN servers s ON s.id = c.server_id
		 WHERE c.id = $1`, categoryID)
}

// SuspendedByRole is SuspendedByServer for the server that owns the role.
func (r *Repository) SuspendedByRole(ctx context.Context, roleID uuid.UUID) (suspended, found bool, err error) {
	return r.lookup(ctx, `
		SELECT s.suspended_at IS NOT NULL
		  FROM roles r JOIN servers s ON s.id = r.server_id
		 WHERE r.id = $1`, roleID)
}

// SuspendedByInvite is SuspendedByServer for the server an invitation code
// belongs to, whatever the invitation's own state (revoked, expired...).
func (r *Repository) SuspendedByInvite(ctx context.Context, code string) (suspended, found bool, err error) {
	return r.lookup(ctx, `
		SELECT s.suspended_at IS NOT NULL
		  FROM invitations i JOIN servers s ON s.id = i.server_id
		 WHERE i.code = $1`, code)
}

func (r *Repository) lookup(ctx context.Context, query string, arg any) (suspended, found bool, err error) {
	err = r.pool.QueryRow(ctx, query, arg).Scan(&suspended)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, false, nil
	}
	if err != nil {
		return false, false, fmt.Errorf("check server suspension: %w", err)
	}
	return suspended, true, nil
}
