package ownership_transfer_repo

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
)

const (
	pgUniqueViolation  = "23505"
	uqPendingPerServer = "uq_ownership_transfers_pending_per_server"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func scanTransfer(row pgx.Row) (model.OwnershipTransfer, error) {
	var t model.OwnershipTransfer
	var status string
	err := row.Scan(&t.ID, &t.ServerID, &t.FromUserID, &t.ToUserID, &status, &t.CreatedAt, &t.ResolvedAt)
	if err != nil {
		return model.OwnershipTransfer{}, err
	}
	t.Status = model.OwnershipTransferStatus(status)
	return t, nil
}

func (r *Repository) Create(ctx context.Context, t model.OwnershipTransfer) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO ownership_transfers (id, server_id, from_user_id, to_user_id, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		t.ID, t.ServerID, t.FromUserID, t.ToUserID, string(t.Status), t.CreatedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation && pgErr.ConstraintName == uqPendingPerServer {
			return model.ErrOwnershipTransferAlreadyPending
		}
		return fmt.Errorf("insert ownership transfer: %w", err)
	}
	return nil
}

func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (model.OwnershipTransfer, error) {
	t, err := scanTransfer(r.pool.QueryRow(ctx, `
		SELECT id, server_id, from_user_id, to_user_id, status, created_at, resolved_at
		  FROM ownership_transfers WHERE id = $1`, id,
	))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.OwnershipTransfer{}, model.ErrOwnershipTransferNotFound
		}
		return model.OwnershipTransfer{}, fmt.Errorf("select ownership transfer: %w", err)
	}
	return t, nil
}

// GetPendingByServer returns the server's pending ownership transfer, if
// any. The unique partial index on (server_id) WHERE status = 'pending'
// guarantees there is at most one.
func (r *Repository) GetPendingByServer(ctx context.Context, serverID uuid.UUID) (model.OwnershipTransfer, error) {
	t, err := scanTransfer(r.pool.QueryRow(ctx, `
		SELECT id, server_id, from_user_id, to_user_id, status, created_at, resolved_at
		  FROM ownership_transfers WHERE server_id = $1 AND status = 'pending'`, serverID,
	))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.OwnershipTransfer{}, model.ErrOwnershipTransferNotFound
		}
		return model.OwnershipTransfer{}, fmt.Errorf("select pending ownership transfer: %w", err)
	}
	return t, nil
}

func (r *Repository) Reject(ctx context.Context, id uuid.UUID, resolvedAt time.Time) (model.OwnershipTransfer, error) {
	t, err := scanTransfer(r.pool.QueryRow(ctx, `
		UPDATE ownership_transfers SET status = 'rejected', resolved_at = $2
		 WHERE id = $1 AND status = 'pending'
		RETURNING id, server_id, from_user_id, to_user_id, status, created_at, resolved_at`,
		id, resolvedAt,
	))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.OwnershipTransfer{}, model.ErrOwnershipTransferNotPending
		}
		return model.OwnershipTransfer{}, fmt.Errorf("reject ownership transfer: %w", err)
	}
	return t, nil
}

func (r *Repository) ExpireStalePending(ctx context.Context, cutoff time.Time, resolvedAt time.Time) (int, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE ownership_transfers SET status = 'rejected', resolved_at = $2
		 WHERE status = 'pending' AND created_at < $1`,
		cutoff, resolvedAt,
	)
	if err != nil {
		return 0, fmt.Errorf("expire stale ownership transfers: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

func (r *Repository) Cancel(ctx context.Context, id uuid.UUID, resolvedAt time.Time) (model.OwnershipTransfer, error) {
	t, err := scanTransfer(r.pool.QueryRow(ctx, `
		UPDATE ownership_transfers SET status = 'cancelled', resolved_at = $2
		 WHERE id = $1 AND status = 'pending'
		RETURNING id, server_id, from_user_id, to_user_id, status, created_at, resolved_at`,
		id, resolvedAt,
	))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.OwnershipTransfer{}, model.ErrOwnershipTransferNotPending
		}
		return model.OwnershipTransfer{}, fmt.Errorf("cancel ownership transfer: %w", err)
	}
	return t, nil
}

func (r *Repository) Accept(ctx context.Context, id uuid.UUID, resolvedAt time.Time) (model.OwnershipTransfer, error) {
	var t model.OwnershipTransfer
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var scanErr error
		t, scanErr = scanTransfer(tx.QueryRow(ctx, `
			UPDATE ownership_transfers SET status = 'accepted', resolved_at = $2
			 WHERE id = $1 AND status = 'pending'
			RETURNING id, server_id, from_user_id, to_user_id, status, created_at, resolved_at`,
			id, resolvedAt,
		))
		if scanErr != nil {
			if errors.Is(scanErr, pgx.ErrNoRows) {
				return model.ErrOwnershipTransferNotPending
			}
			return fmt.Errorf("resolve ownership transfer: %w", scanErr)
		}

		var stillMember bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM server_members WHERE server_id = $1 AND user_id = $2)`,
			t.ServerID, t.ToUserID,
		).Scan(&stillMember); err != nil {
			return fmt.Errorf("check target membership: %w", err)
		}
		if !stillMember {
			return model.ErrTransferTargetNotMember
		}

		if _, err := tx.Exec(ctx,
			`UPDATE server_members SET is_owner = FALSE WHERE server_id = $1 AND is_owner = TRUE`,
			t.ServerID,
		); err != nil {
			return fmt.Errorf("demote previous owner: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`UPDATE server_members SET is_owner = TRUE WHERE server_id = $1 AND user_id = $2`,
			t.ServerID, t.ToUserID,
		); err != nil {
			return fmt.Errorf("promote new owner membership: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`UPDATE servers SET owner_id = $2 WHERE id = $1`,
			t.ServerID, t.ToUserID,
		); err != nil {
			return fmt.Errorf("update server owner: %w", err)
		}
		return nil
	})
	if err != nil {
		return model.OwnershipTransfer{}, err
	}
	return t, nil
}
