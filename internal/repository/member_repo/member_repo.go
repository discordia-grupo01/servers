// Package member_repo implements Postgres access for server membership and
// the roles assigned to each member.
package member_repo

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/outbox"
)

type MemberRepository struct {
	pool *pgxpool.Pool
}

func NewMemberRepository(pool *pgxpool.Pool) *MemberRepository {
	return &MemberRepository{pool: pool}
}

func (r *MemberRepository) IsMember(ctx context.Context, serverID uuid.UUID, userID string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM server_members WHERE server_id = $1 AND user_id = $2)`,
		serverID, userID,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check membership: %w", err)
	}
	return exists, nil
}

// MemberStanding returns whether userID is a member of serverID and the union
// of the permissions of every role assigned to them, in a single query. It
// never sets IsOwner: ownership comes from the server row (see
// service.ResolveStanding).
func (r *MemberRepository) MemberStanding(ctx context.Context, serverID uuid.UUID, userID string) (role.Standing, error) {
	var st role.Standing
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM server_members WHERE server_id = $1 AND user_id = $2),
		       COALESCE(BIT_OR(r.permissions), 0)
		  FROM member_roles mr
		  JOIN roles r ON r.id = mr.role_id
		 WHERE mr.server_id = $1 AND mr.user_id = $2`,
		serverID, userID,
	).Scan(&st.IsMember, &st.Permissions)
	if err != nil {
		return role.Standing{}, fmt.Errorf("member standing: %w", err)
	}
	return st, nil
}

// HasRole reports whether userID currently has roleID assigned in serverID.
func (r *MemberRepository) HasRole(ctx context.Context, serverID uuid.UUID, userID string, roleID uuid.UUID) (bool, error) {
	var has bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM member_roles WHERE server_id = $1 AND user_id = $2 AND role_id = $3)`,
		serverID, userID, roleID,
	).Scan(&has)
	if err != nil {
		return false, fmt.Errorf("has role: %w", err)
	}
	return has, nil
}

// AssignRole is idempotent: assigning a role the member already has is a no-op.
func (r *MemberRepository) AssignRole(ctx context.Context, serverID uuid.UUID, userID string, roleID uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO member_roles (server_id, user_id, role_id, assigned_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (server_id, user_id, role_id) DO NOTHING`,
		serverID, userID, roleID,
	)
	if err != nil {
		return fmt.Errorf("assign role: %w", err)
	}
	return nil
}

// ListMembers returns one page of a server's members ordered by join date
// (oldest first, user_id as a tie-breaker), along with the total member
// count so the caller can render pagination.
func (r *MemberRepository) ListMembers(ctx context.Context, serverID uuid.UUID, limit, offset int) ([]model.Member, int, error) {
	var total int
	if err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM server_members WHERE server_id = $1`, serverID,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count members: %w", err)
	}

	rows, err := r.pool.Query(ctx, `
		SELECT user_id, is_owner, joined_at
		  FROM server_members
		 WHERE server_id = $1
		 ORDER BY joined_at ASC, user_id ASC
		 LIMIT $2 OFFSET $3`,
		serverID, limit, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list members: %w", err)
	}
	defer rows.Close()

	members := make([]model.Member, 0)
	for rows.Next() {
		var m model.Member
		if err := rows.Scan(&m.UserID, &m.IsOwner, &m.JoinedAt); err != nil {
			return nil, 0, fmt.Errorf("scan member: %w", err)
		}
		members = append(members, m)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("list members: %w", err)
	}
	return members, total, nil
}

func (r *MemberRepository) RemoveMember(ctx context.Context, serverID uuid.UUID, userID string, memberLeftEvent outbox.Event) (bool, error) {
	var removed bool
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			`DELETE FROM server_members WHERE server_id = $1 AND user_id = $2`,
			serverID, userID,
		)
		if err != nil {
			return fmt.Errorf("remove member: %w", err)
		}
		removed = tag.RowsAffected() > 0
		if !removed {
			return nil
		}
		return outbox.Insert(ctx, tx, memberLeftEvent)
	})
	if err != nil {
		return false, err
	}
	return removed, nil
}

func (r *MemberRepository) RemoveRole(ctx context.Context, serverID uuid.UUID, userID string, roleID uuid.UUID) error {
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM member_roles WHERE server_id = $1 AND user_id = $2 AND role_id = $3`,
		serverID, userID, roleID,
	)
	if err != nil {
		return fmt.Errorf("remove role: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return model.ErrMemberRoleNotFound
	}
	return nil
}

func (r *MemberRepository) ListRolesForMember(ctx context.Context, serverID uuid.UUID, userID string) ([]role.Role, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT r.id, r.server_id, r.name, r.color, r.permissions, r.created_at, r.updated_at
		  FROM roles r
		  JOIN member_roles mr ON mr.role_id = r.id
		 WHERE mr.server_id = $1 AND mr.user_id = $2
		 ORDER BY r.created_at`,
		serverID, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list member roles: %w", err)
	}
	defer rows.Close()

	roles := make([]role.Role, 0)
	for rows.Next() {
		var rl role.Role
		if err := rows.Scan(&rl.ID, &rl.ServerID, &rl.Name, &rl.Color, &rl.Permissions, &rl.CreatedAt, &rl.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan member role: %w", err)
		}
		roles = append(roles, rl)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list member roles: %w", err)
	}
	return roles, nil
}
