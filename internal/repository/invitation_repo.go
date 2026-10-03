package repository

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
	"github.com/discordia-grupo01/servers/internal/outbox"
)

const uqInvitationsCode = "invitations_code_key"

type InvitationRepository struct {
	pool *pgxpool.Pool
}

func NewInvitationRepository(pool *pgxpool.Pool) *InvitationRepository {
	return &InvitationRepository{pool: pool}
}

func (r *InvitationRepository) IsMember(ctx context.Context, serverID uuid.UUID, userID string) (bool, error) {
	var exists bool
	if err := r.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM server_members WHERE server_id = $1 AND user_id = $2)`,
		serverID, userID,
	).Scan(&exists); err != nil {
		return false, fmt.Errorf("is member: %w", err)
	}
	return exists, nil
}

func (r *InvitationRepository) IsBanned(ctx context.Context, serverID uuid.UUID, userID string) (bool, error) {
	var exists bool
	if err := r.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM server_bans WHERE server_id = $1 AND user_id = $2)`,
		serverID, userID,
	).Scan(&exists); err != nil {
		return false, fmt.Errorf("is banned: %w", err)
	}
	return exists, nil
}

func (r *InvitationRepository) CreateInvitation(ctx context.Context, inv model.Invitation) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO invitations
			(id, code, server_id, created_by, max_uses, uses, expires_at, revoked_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		inv.ID, inv.Code, inv.ServerID, inv.CreatedBy,
		inv.MaxUses, inv.Uses, inv.ExpiresAt, inv.RevokedAt, inv.CreatedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation && pgErr.ConstraintName == uqInvitationsCode {
			return model.ErrInviteCodeUnavailable
		}
		return fmt.Errorf("insert invitation: %w", err)
	}
	return nil
}

func (r *InvitationRepository) InvitationByCode(ctx context.Context, code string) (model.Invitation, error) {
	var inv model.Invitation
	err := r.pool.QueryRow(ctx, `
		SELECT id, code, server_id, created_by, max_uses, uses, expires_at, revoked_at, created_at
		  FROM invitations
		 WHERE code = $1`, code,
	).Scan(
		&inv.ID, &inv.Code, &inv.ServerID, &inv.CreatedBy,
		&inv.MaxUses, &inv.Uses, &inv.ExpiresAt, &inv.RevokedAt, &inv.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Invitation{}, model.ErrInvitationNotFound
	}
	if err != nil {
		return model.Invitation{}, fmt.Errorf("select invitation: %w", err)
	}
	return inv, nil
}

func (r *InvitationRepository) ListByServer(ctx context.Context, serverID uuid.UUID) ([]model.Invitation, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, code, server_id, created_by, max_uses, uses, expires_at, revoked_at, created_at
		  FROM invitations
		 WHERE server_id = $1
		 ORDER BY created_at DESC`, serverID,
	)
	if err != nil {
		return nil, fmt.Errorf("list invitations: %w", err)
	}
	defer rows.Close()

	invitations := make([]model.Invitation, 0)
	for rows.Next() {
		var inv model.Invitation
		if err := rows.Scan(
			&inv.ID, &inv.Code, &inv.ServerID, &inv.CreatedBy,
			&inv.MaxUses, &inv.Uses, &inv.ExpiresAt, &inv.RevokedAt, &inv.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan invitation: %w", err)
		}
		invitations = append(invitations, inv)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list invitations: %w", err)
	}
	return invitations, nil
}

func (r *InvitationRepository) RevokeInvitation(ctx context.Context, code string, revokedAt time.Time) error {
	if _, err := r.pool.Exec(ctx, `
		UPDATE invitations SET revoked_at = $2
		 WHERE code = $1 AND revoked_at IS NULL`, code, revokedAt,
	); err != nil {
		return fmt.Errorf("revoke invitation: %w", err)
	}
	return nil
}

func (r *InvitationRepository) RedeemInvitation(ctx context.Context, code, userID string, now time.Time, buildEvent func(serverID uuid.UUID) (outbox.Event, error)) (uuid.UUID, error) {
	var serverID uuid.UUID
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx, `
			UPDATE invitations
			   SET uses = uses + 1
			 WHERE code = $1
			   AND revoked_at IS NULL
			   AND (expires_at IS NULL OR expires_at > $2)
			   AND (max_uses IS NULL OR uses < max_uses)
			RETURNING server_id`, code, now,
		)
		if err := row.Scan(&serverID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return model.ErrInvitationExhausted
			}
			return fmt.Errorf("consume invitation use: %w", err)
		}

		// The service already checked the ban, but a ban can commit between
		// that check and this insert. The ban locks the server row FOR NO KEY
		// UPDATE, which conflicts with FOR SHARE: once we hold it, a ban is
		// either fully committed (and visible to the next statement) or
		// waits for us to finish.
		if _, err := tx.Exec(ctx, `SELECT 1 FROM servers WHERE id = $1 FOR SHARE`, serverID); err != nil {
			return fmt.Errorf("lock server: %w", err)
		}
		var banned bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM server_bans WHERE server_id = $1 AND user_id = $2)`,
			serverID, userID,
		).Scan(&banned); err != nil {
			return fmt.Errorf("check ban: %w", err)
		}
		if banned {
			return model.ErrUserBanned
		}

		ct, err := tx.Exec(ctx, `
			INSERT INTO server_members (server_id, user_id, is_owner, joined_at)
			VALUES ($1, $2, FALSE, $3)
			ON CONFLICT (server_id, user_id) DO NOTHING`, serverID, userID, now,
		)
		if err != nil {
			return fmt.Errorf("insert membership: %w", err)
		}
		if ct.RowsAffected() == 0 {
			return model.ErrAlreadyMember
		}

		var defaultRoleID *uuid.UUID
		if err := tx.QueryRow(ctx,
			`SELECT default_role_id FROM servers WHERE id = $1`, serverID,
		).Scan(&defaultRoleID); err != nil {
			return fmt.Errorf("select default role: %w", err)
		}
		if defaultRoleID != nil {
			if _, err := tx.Exec(ctx, `
				INSERT INTO member_roles (server_id, user_id, role_id, assigned_at)
				VALUES ($1, $2, $3, $4)
				ON CONFLICT (server_id, user_id, role_id) DO NOTHING`,
				serverID, userID, *defaultRoleID, now,
			); err != nil {
				return fmt.Errorf("assign default role: %w", err)
			}
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO member_roles (server_id, user_id, role_id, assigned_at)
			SELECT $1, $2, r.id, $3 FROM roles r WHERE r.server_id = $1 AND r.is_everyone
			ON CONFLICT (server_id, user_id, role_id) DO NOTHING`,
			serverID, userID, now,
		); err != nil {
			return fmt.Errorf("assign everyone role: %w", err)
		}

		evt, err := buildEvent(serverID)
		if err != nil {
			return fmt.Errorf("build member joined event: %w", err)
		}
		return outbox.Insert(ctx, tx, evt)
	})
	if err != nil {
		return uuid.Nil, err
	}
	return serverID, nil
}
