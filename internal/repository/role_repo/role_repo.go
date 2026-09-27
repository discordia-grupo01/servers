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
	return listByServer(ctx, r.pool, serverID)
}

// Reorder rewrites the positions of serverID's roles to 1..N following
// orderedIDs (highest first) and returns the roles in their new order.
//
// check receives the server's current roles (highest first) read under the
// same server-row lock that Create takes, so the validation can't race a
// concurrent create, delete or reorder: if check returns an error, nothing is
// written and that error is returned as-is.
func (r *RoleRepository) Reorder(ctx context.Context, serverID uuid.UUID, orderedIDs []uuid.UUID, check func(current []role.Role) error) ([]role.Role, error) {
	var reordered []role.Role
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT 1 FROM servers WHERE id = $1 FOR NO KEY UPDATE`, serverID); err != nil {
			return fmt.Errorf("lock server: %w", err)
		}
		current, err := listByServer(ctx, tx, serverID)
		if err != nil {
			return err
		}
		if err := check(current); err != nil {
			return err
		}
		// uq_roles_server_position is DEFERRABLE INITIALLY DEFERRED, so the
		// transient duplicates while rewriting are fine: it's checked at commit.
		for i, id := range orderedIDs {
			if _, err := tx.Exec(ctx,
				`UPDATE roles SET position = $1 WHERE id = $2 AND server_id = $3`,
				i+1, id, serverID); err != nil {
				return fmt.Errorf("update role position: %w", err)
			}
		}
		reordered, err = listByServer(ctx, tx, serverID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return reordered, nil
}

// querier is what listByServer needs, satisfied by both the pool and a tx.
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func listByServer(ctx context.Context, q querier, serverID uuid.UUID) ([]role.Role, error) {
	rows, err := q.Query(ctx, `
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
