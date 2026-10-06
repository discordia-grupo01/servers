// Package moderation_repo implements Postgres access for server bans (#25,
// #26) and kicks (#59).
package moderation_repo

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
	"github.com/discordia-grupo01/servers/internal/model/moderation"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/model/userprofile"
	"github.com/discordia-grupo01/servers/internal/outbox"
	"github.com/discordia-grupo01/servers/internal/repository"
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
// transfer being accepted (which rewrites servers.owner_id) and with every
// change to roles or role assignments (see repository.LockServerShared), so
// nothing read here can be outdated:
//
//   - banning whoever owns the server at commit time fails with
//     ErrCannotBanOwner,
//   - check receives the actor's (ban.BannedBy) and the target's standing as
//     they are now, and its error, if any, is returned as-is with nothing
//     written. It is where the permission and the hierarchy are decided.
func (r *Repository) BanMember(ctx context.Context, ban moderation.Ban, check func(actor, target role.Standing) error, events ...outbox.Event) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		ownerID, err := lockServer(ctx, tx, ban.ServerID)
		if err != nil {
			return err
		}
		if ownerID == ban.UserID {
			return model.ErrCannotBanOwner
		}

		var actorID string
		if ban.BannedBy != nil {
			actorID = *ban.BannedBy
		}
		if err := checkStandings(ctx, tx, ban.ServerID, ownerID, actorID, ban.UserID, check); err != nil {
			return err
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

		if err := removeMember(ctx, tx, ban.ServerID, ban.UserID, ban.BannedAt); err != nil {
			return err
		}
		return insertEvents(ctx, tx, events)
	})
}

// KickMember removes kick.UserID from kick.ServerID in a single transaction
// (#59), with the same locking and re-check as BanMember but without
// recording anything that keeps them out: they can rejoin with any valid
// invitation.
//
//   - removes the membership, which cascades to their role assignments
//     (ErrMemberNotFound, and nothing is written, if they aren't a member --
//     e.g. a concurrent kick already removed them),
//   - cancels the pending ownership transfer addressed to them, if any,
//   - writes events to the outbox.
//
// Kicking whoever owns the server at commit time fails with
// ErrCannotKickOwner; check decides the permission and the hierarchy with
// both standings read under the server lock.
func (r *Repository) KickMember(ctx context.Context, kick moderation.Kick, check func(actor, target role.Standing) error, events ...outbox.Event) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		ownerID, err := lockServer(ctx, tx, kick.ServerID)
		if err != nil {
			return err
		}
		if ownerID == kick.UserID {
			return model.ErrCannotKickOwner
		}
		if err := checkStandings(ctx, tx, kick.ServerID, ownerID, kick.KickedBy, kick.UserID, check); err != nil {
			return err
		}
		if err := removeMember(ctx, tx, kick.ServerID, kick.UserID, kick.KickedAt); err != nil {
			return err
		}
		return insertEvents(ctx, tx, events)
	})
}

// lockServer takes the server row FOR NO KEY UPDATE and returns its owner.
// That serializes bans and kicks with an ownership transfer being accepted
// (which rewrites servers.owner_id) and with every change to roles or role
// assignments (see repository.LockServerShared).
func lockServer(ctx context.Context, tx pgx.Tx, serverID uuid.UUID) (ownerID string, err error) {
	err = tx.QueryRow(ctx,
		`SELECT owner_id FROM servers WHERE id = $1 FOR NO KEY UPDATE`, serverID,
	).Scan(&ownerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", model.ErrServerNotFound
	}
	if err != nil {
		return "", fmt.Errorf("lock server: %w", err)
	}
	return ownerID, nil
}

// checkStandings reads the actor's and the target's standing inside tx and
// hands them to check, returning its error as-is.
func checkStandings(ctx context.Context, tx pgx.Tx, serverID uuid.UUID, ownerID, actorID, targetID string, check func(actor, target role.Standing) error) error {
	actor, err := repository.StandingOf(ctx, tx, serverID, ownerID, actorID)
	if err != nil {
		return err
	}
	target, err := repository.StandingOf(ctx, tx, serverID, ownerID, targetID)
	if err != nil {
		return err
	}
	return check(actor, target)
}

// removeMember deletes userID's membership (cascading to their role
// assignments) and cancels the pending ownership transfer addressed to them,
// if any. ErrMemberNotFound if they aren't a member.
func removeMember(ctx context.Context, tx pgx.Tx, serverID uuid.UUID, userID string, at time.Time) error {
	tag, err := tx.Exec(ctx,
		`DELETE FROM server_members WHERE server_id = $1 AND user_id = $2`,
		serverID, userID,
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
		serverID, userID, at,
	); err != nil {
		return fmt.Errorf("cancel pending transfer: %w", err)
	}
	return nil
}

func insertEvents(ctx context.Context, tx pgx.Tx, events []outbox.Event) error {
	for _, evt := range events {
		if err := outbox.Insert(ctx, tx, evt); err != nil {
			return err
		}
	}
	return nil
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
		return insertEvents(ctx, tx, events)
	})
}

// ListBans returns a page of serverID's bans, newest first, and the total
// number of bans. Each ban carries the banned user's replicated public
// profile, or a nil Profile if it hasn't arrived yet.
func (r *Repository) ListBans(ctx context.Context, serverID uuid.UUID, limit, offset int) ([]moderation.Ban, int, error) {
	var total int
	if err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM server_bans WHERE server_id = $1`, serverID,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count bans: %w", err)
	}

	rows, err := r.pool.Query(ctx, `
		SELECT b.server_id, b.user_id, b.reason, b.banned_by, b.banned_at,
		       p.user_id, p.name, p.avatar_url, p.description, p.status_text, p.status_emoji, p.version
		  FROM server_bans b
		  LEFT JOIN user_profiles p ON p.user_id = b.user_id
		 WHERE b.server_id = $1
		 ORDER BY b.banned_at DESC, b.user_id
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
		var (
			profileUserID                                         *string
			name, avatarURL, description, statusText, statusEmoji *string
			version                                               *int64
		)
		if err := rows.Scan(&b.ServerID, &b.UserID, &b.Reason, &b.BannedBy, &b.BannedAt,
			&profileUserID, &name, &avatarURL, &description, &statusText, &statusEmoji, &version,
		); err != nil {
			return nil, 0, fmt.Errorf("scan ban: %w", err)
		}
		if profileUserID != nil {
			b.Profile = &userprofile.Profile{
				UserID: *profileUserID, Name: *name, AvatarURL: *avatarURL, Description: *description,
				StatusText: *statusText, StatusEmoji: *statusEmoji, Version: *version,
			}
		}
		bans = append(bans, b)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("list bans: %w", err)
	}
	return bans, total, nil
}
