// Package moderation_repo implements Postgres access for server bans (#25,
// #26).
package moderation_repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/moderation"
	"github.com/discordia-grupo01/servers/internal/outbox"
)

const (
	pgUniqueViolation = "23505"
	pkServerBans      = "server_bans_pkey"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// BanMember bans ban.UserID from ban.ServerID in a single transaction:
//
//   - records the ban (ErrAlreadyBanned if there already is one),
//   - removes the membership, which cascades to their role assignments
//     (ErrMemberNotFound, and nothing is kept, if they aren't a member),
//   - cancels the pending ownership transfer addressed to them, if any,
//   - writes events to the outbox, so they are published only if the ban
//     commits.
//
// The server row is locked first. That serializes the ban with an ownership
// transfer being accepted (which rewrites servers.owner_id), so the owner
// check here can't be outdated: banning whoever owns the server at commit
// time fails with ErrCannotBanOwner.
func (r *Repository) BanMember(ctx context.Context, ban moderation.Ban, events ...outbox.Event) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var ownerID string
		err := tx.QueryRow(ctx,
			`SELECT owner_id FROM servers WHERE id = $1 FOR NO KEY UPDATE`, ban.ServerID,
		).Scan(&ownerID)
		if errors.Is(err, pgx.ErrNoRows) {
			return model.ErrServerNotFound
		}
		if err != nil {
			return fmt.Errorf("lock server: %w", err)
		}
		if ownerID == ban.UserID {
			return model.ErrCannotBanOwner
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO server_bans (server_id, user_id, reason, banned_by, banned_at)
			VALUES ($1, $2, $3, $4, $5)`,
			ban.ServerID, ban.UserID, ban.Reason, ban.BannedBy, ban.BannedAt,
		)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation && pgErr.ConstraintName == pkServerBans {
				return model.ErrAlreadyBanned
			}
			return fmt.Errorf("insert ban: %w", err)
		}

		tag, err := tx.Exec(ctx,
			`DELETE FROM server_members WHERE server_id = $1 AND user_id = $2`,
			ban.ServerID, ban.UserID,
		)
		if err != nil {
			return fmt.Errorf("remove member: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return model.ErrMemberNotFound
		}

		if _, err := tx.Exec(ctx, `
			UPDATE ownership_transfers SET status = 'cancelled', resolved_at = $3
			 WHERE server_id = $1 AND to_user_id = $2 AND status = 'pending'`,
			ban.ServerID, ban.UserID, ban.BannedAt,
		); err != nil {
			return fmt.Errorf("cancel pending transfer: %w", err)
		}

		for _, evt := range events {
			if err := outbox.Insert(ctx, tx, evt); err != nil {
				return err
			}
		}
		return nil
	})
}

// UnbanMember revokes userID's ban on serverID and writes events to the
// outbox in the same transaction. ErrBanNotFound if there is no ban; with two
// concurrent unbans the second one gets it, so the event is written once.
// Membership is not restored: the user has to join again with an invitation.
func (r *Repository) UnbanMember(ctx context.Context, serverID uuid.UUID, userID string, events ...outbox.Event) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			`DELETE FROM server_bans WHERE server_id = $1 AND user_id = $2`,
			serverID, userID,
		)
		if err != nil {
			return fmt.Errorf("delete ban: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return model.ErrBanNotFound
		}
		for _, evt := range events {
			if err := outbox.Insert(ctx, tx, evt); err != nil {
				return err
			}
		}
		return nil
	})
}

// ListBans returns a page of serverID's bans, newest first, and the total
// number of bans.
func (r *Repository) ListBans(ctx context.Context, serverID uuid.UUID, limit, offset int) ([]moderation.Ban, int, error) {
	var total int
	if err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM server_bans WHERE server_id = $1`, serverID,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count bans: %w", err)
	}

	rows, err := r.pool.Query(ctx, `
		SELECT server_id, user_id, reason, banned_by, banned_at
		  FROM server_bans
		 WHERE server_id = $1
		 ORDER BY banned_at DESC, user_id
		 LIMIT $2 OFFSET $3`,
		serverID, limit, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list bans: %w", err)
	}
	defer rows.Close()

	bans := make([]moderation.Ban, 0)
	for rows.Next() {
		var b moderation.Ban
		if err := rows.Scan(&b.ServerID, &b.UserID, &b.Reason, &b.BannedBy, &b.BannedAt); err != nil {
			return nil, 0, fmt.Errorf("scan ban: %w", err)
		}
		bans = append(bans, b)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("list bans: %w", err)
	}
	return bans, total, nil
}
