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

// Create inserts rl at the bottom of its server's hierarchy (MAX(position)+1,
// or 1 for the first role) and returns it with its assigned Position.
//
// The server row is locked first so two concurrent creations can't both read
// the same MAX and collide on the position. FOR NO KEY UPDATE (not FOR UPDATE)
// serializes role creations without blocking the FK checks of unrelated
// inserts that reference the server (members joining, channels, ...).
func (r *RoleRepository) Create(ctx context.Context, rl role.Role) (role.Role, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return role.Role{}, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT 1 FROM servers WHERE id = $1 FOR NO KEY UPDATE`, rl.ServerID); err != nil {
		return role.Role{}, fmt.Errorf("lock server: %w", err)
	}

	err = tx.QueryRow(ctx, `
		INSERT INTO roles (id, server_id, name, color, permissions, position, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5,
		        (SELECT COALESCE(MAX(position), 0) + 1 FROM roles WHERE server_id = $2),
		        $6, $7)
		RETURNING position`,
		rl.ID, rl.ServerID, rl.Name, rl.Color, rl.Permissions, rl.CreatedAt, rl.UpdatedAt,
	).Scan(&rl.Position)
	if err != nil {
		return role.Role{}, fmt.Errorf("insert role: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return role.Role{}, fmt.Errorf("commit: %w", err)
	}
	return rl, nil
}

func (r *RoleRepository) GetByID(ctx context.Context, id uuid.UUID) (role.Role, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, server_id, name, color, permissions, position, created_at, updated_at
		  FROM roles WHERE id = $1`, id)

	var rl role.Role
	if err := row.Scan(&rl.ID, &rl.ServerID, &rl.Name, &rl.Color, &rl.Permissions, &rl.Position, &rl.CreatedAt, &rl.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return role.Role{}, model.ErrRoleNotFound
		}
		return role.Role{}, fmt.Errorf("select role: %w", err)
	}
	return rl, nil
}

func (r *RoleRepository) ListByServer(ctx context.Context, serverID uuid.UUID) ([]role.Role, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, server_id, name, color, permissions, position, created_at, updated_at
		  FROM roles WHERE server_id = $1 ORDER BY position, id`, serverID)
	if err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	defer rows.Close()

	roles := make([]role.Role, 0)
	for rows.Next() {
		var rl role.Role
		if err := rows.Scan(&rl.ID, &rl.ServerID, &rl.Name, &rl.Color, &rl.Permissions, &rl.Position, &rl.CreatedAt, &rl.UpdatedAt); err != nil {
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

// Delete removes a role, unless it is currently its server's default role
// (checked atomically in the same statement, so a concurrent
// SetDefaultRole cannot race it into deleting the default anyway).
func (r *RoleRepository) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM roles
		 WHERE id = $1
		   AND NOT EXISTS (SELECT 1 FROM servers WHERE servers.default_role_id = roles.id)`,
		id)
	if err != nil {
		return fmt.Errorf("delete role: %w", err)
	}
	if tag.RowsAffected() > 0 {
		return nil
	}

	var isDefault bool
	err = r.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM roles JOIN servers ON servers.id = roles.server_id
			 WHERE roles.id = $1 AND servers.default_role_id = roles.id
		)`, id).Scan(&isDefault)
	if err != nil {
		return fmt.Errorf("check role default: %w", err)
	}
	if isDefault {
		return model.ErrRoleIsServerDefault
	}
	return model.ErrRoleNotFound
}
