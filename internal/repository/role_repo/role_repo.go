// Package role_repo implements Postgres access for the Role domain type.
package role_repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/role"
)

type RoleRepository struct {
	pool *pgxpool.Pool
}

func NewRoleRepository(pool *pgxpool.Pool) *RoleRepository {
	return &RoleRepository{pool: pool}
}

func (r *RoleRepository) Create(ctx context.Context, rl role.Role) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO roles (id, server_id, name, color, permissions, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		rl.ID, rl.ServerID, rl.Name, rl.Color, rl.Permissions, rl.CreatedAt, rl.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert role: %w", err)
	}
	return nil
}

func (r *RoleRepository) GetByID(ctx context.Context, id uuid.UUID) (role.Role, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, server_id, name, color, permissions, created_at, updated_at
		  FROM roles WHERE id = $1`, id)

	var rl role.Role
	if err := row.Scan(&rl.ID, &rl.ServerID, &rl.Name, &rl.Color, &rl.Permissions, &rl.CreatedAt, &rl.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return role.Role{}, model.ErrRoleNotFound
		}
		return role.Role{}, fmt.Errorf("select role: %w", err)
	}
	return rl, nil
}

func (r *RoleRepository) ListByServer(ctx context.Context, serverID uuid.UUID) ([]role.Role, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, server_id, name, color, permissions, created_at, updated_at
		  FROM roles WHERE server_id = $1 ORDER BY created_at`, serverID)
	if err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	defer rows.Close()

	roles := make([]role.Role, 0)
	for rows.Next() {
		var rl role.Role
		if err := rows.Scan(&rl.ID, &rl.ServerID, &rl.Name, &rl.Color, &rl.Permissions, &rl.CreatedAt, &rl.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan role: %w", err)
		}
		roles = append(roles, rl)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	return roles, nil
}

func (r *RoleRepository) Update(ctx context.Context, rl role.Role) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE roles SET name = $1, color = $2, permissions = $3, updated_at = $4
		 WHERE id = $5`,
		rl.Name, rl.Color, rl.Permissions, rl.UpdatedAt, rl.ID,
	)
	if err != nil {
		return fmt.Errorf("update role: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return model.ErrRoleNotFound
	}
	return nil
}
