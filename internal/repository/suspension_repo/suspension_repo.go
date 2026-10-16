// Package suspension_repo implements Postgres access for the replica of
// platform-level suspensions (suspended_users).
package suspension_repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SuspensionRepository struct {
	pool *pgxpool.Pool
}

func NewSuspensionRepository(pool *pgxpool.Pool) *SuspensionRepository {
	return &SuspensionRepository{pool: pool}
}

// Apply stores the suspension state of userID unless a state with the same or
// a higher version is already stored, and reports whether it was applied.
// Redelivered and out-of-order events are therefore harmless no-ops, in a
// single atomic statement. A user with no previous row is inserted as is.
func (r *SuspensionRepository) Apply(ctx context.Context, userID string, suspended bool, version int64) (bool, error) {
	tag, err := r.pool.Exec(ctx, `
		INSERT INTO suspended_users (user_id, suspended, version, updated_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (user_id) DO UPDATE SET
			suspended  = EXCLUDED.suspended,
			version    = EXCLUDED.version,
			updated_at = now()
		WHERE suspended_users.version < EXCLUDED.version`,
		userID, suspended, version,
	)
	if err != nil {
		return false, fmt.Errorf("apply suspension: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// IsSuspended reports whether userID is currently suspended. A user with no
// row is not.
func (r *SuspensionRepository) IsSuspended(ctx context.Context, userID string) (bool, error) {
	var suspended bool
	err := r.pool.QueryRow(ctx, `SELECT suspended FROM suspended_users WHERE user_id = $1`, userID).Scan(&suspended)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("check suspension: %w", err)
	}
	return suspended, nil
}
