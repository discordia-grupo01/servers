// Package member_repo implements Postgres access for server membership and
// the roles assigned to each member.
package member_repo

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/role"
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

func (r *MemberRepository) RemoveMember(ctx context.Context, serverID uuid.UUID, userID string) (bool, error) {
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM server_members WHERE server_id = $1 AND user_id = $2`,
		serverID, userID,
	)
	if err != nil {
		return false, fmt.Errorf("remove member: %w", err)
	}
	return tag.RowsAffected() > 0, nil
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
