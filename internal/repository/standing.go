package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/discordia-grupo01/servers/internal/model/role"
)

// RowQuerier is what the standing queries need, satisfied by both the pool
// and a transaction, so the same read can run inside a write transaction.
type RowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// MemberStanding reports whether userID is a member of serverID, the union of
// their roles' permissions and the position of their highest role (nil
// without roles), in a single query. It never sets IsOwner: ownership comes
// from the server row.
func MemberStanding(ctx context.Context, q RowQuerier, serverID uuid.UUID, userID string) (role.Standing, error) {
	var st role.Standing
	// @everyone's reserved position = 0 is excluded from MIN on purpose:
	// it's held by every member, so without the filter everyone would rank
	// at the top of the hierarchy, outranking every real role
	err := q.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM server_members WHERE server_id = $1 AND user_id = $2),
		       COALESCE(BIT_OR(r.permissions), 0),
		       MIN(r.position) FILTER (WHERE NOT r.is_everyone)
		  FROM member_roles mr
		  JOIN roles r ON r.id = mr.role_id
		 WHERE mr.server_id = $1 AND mr.user_id = $2`,
		serverID, userID,
	).Scan(&st.IsMember, &st.Permissions, &st.TopPosition)
	if err != nil {
		return role.Standing{}, fmt.Errorf("member standing: %w", err)
	}
	return st, nil
}

// StandingOf is MemberStanding plus ownership, the same rule as
// service.ResolveStanding: the owner (ownerID, read by the caller from the
// server row) is a member above everyone, whatever roles they hold. Meant for
// write transactions that re-check authorization under the server lock.
func StandingOf(ctx context.Context, q RowQuerier, serverID uuid.UUID, ownerID, userID string) (role.Standing, error) {
	if userID != "" && userID == ownerID {
		return role.Standing{IsMember: true, IsOwner: true}, nil
	}
	return MemberStanding(ctx, q, serverID, userID)
}

// LockServerShared takes a FOR SHARE lock on the server row inside tx. Writes
// that change who outranks whom (assigning, unassigning, editing or deleting
// roles) take it, so they wait for -- and are waited on by -- the writes that
// check the hierarchy under FOR NO KEY UPDATE (ban, role reorder, role
// creation). Shared locks don't block each other. ErrNoRows-free: a missing
// server simply locks nothing.
func LockServerShared(ctx context.Context, tx pgx.Tx, serverID uuid.UUID) error {
	if _, err := tx.Exec(ctx, `SELECT 1 FROM servers WHERE id = $1 FOR SHARE`, serverID); err != nil {
		return fmt.Errorf("lock server: %w", err)
	}
	return nil
}
