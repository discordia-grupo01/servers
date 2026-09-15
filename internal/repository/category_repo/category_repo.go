// Package category_repo implements Postgres access for the Category domain type.
package category_repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/discordia-grupo01/servers/internal/model"
)

type CategoryRepository struct {
	pool *pgxpool.Pool
}

func NewCategoryRepository(pool *pgxpool.Pool) *CategoryRepository {
	return &CategoryRepository{pool: pool}
}

func (r *CategoryRepository) Create(ctx context.Context, cat model.Category) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO categories (id, server_id, name, position, created_at)
		VALUES ($1, $2, $3, $4, $5)`,
		cat.ID, cat.ServerID, cat.Name, cat.Position, cat.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert category: %w", err)
	}
	return nil
}

func (r *CategoryRepository) GetByID(ctx context.Context, id uuid.UUID) (model.Category, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, server_id, name, position, created_at
		  FROM categories WHERE id = $1`, id)

	var cat model.Category
	if err := row.Scan(&cat.ID, &cat.ServerID, &cat.Name, &cat.Position, &cat.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Category{}, model.ErrCategoryNotFound
		}
		return model.Category{}, fmt.Errorf("select category: %w", err)
	}
	return cat, nil
}

func (r *CategoryRepository) ListByServer(ctx context.Context, serverID uuid.UUID) ([]model.Category, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, server_id, name, position, created_at
		  FROM categories WHERE server_id = $1 ORDER BY position`, serverID)
	if err != nil {
		return nil, fmt.Errorf("list categories: %w", err)
	}
	defer rows.Close()

	categories := make([]model.Category, 0)
	for rows.Next() {
		var cat model.Category
		if err := rows.Scan(&cat.ID, &cat.ServerID, &cat.Name, &cat.Position, &cat.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan category: %w", err)
		}
		categories = append(categories, cat)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list categories: %w", err)
	}
	return categories, nil
}

func (r *CategoryRepository) Update(ctx context.Context, id uuid.UUID, name string) error {
	tag, err := r.pool.Exec(ctx, `UPDATE categories SET name = $1 WHERE id = $2`, name, id)
	if err != nil {
		return fmt.Errorf("update category: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return model.ErrCategoryNotFound
	}
	return nil
}

// Delete removes a category. Channels in it are not deleted: their
// category_id is set to NULL by the FK (ON DELETE SET NULL), so they fall
// back to the "sin categoria" bucket.
func (r *CategoryRepository) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM categories WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete category: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return model.ErrCategoryNotFound
	}
	return nil
}

// ReorderPositions sets each category's position to its index in orderedIDs,
// atomically. Callers must have already validated that orderedIDs is exactly
// the current set of categories for the server.
func (r *CategoryRepository) ReorderPositions(ctx context.Context, orderedIDs []uuid.UUID) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		for i, id := range orderedIDs {
			if _, err := tx.Exec(ctx, `UPDATE categories SET position = $1 WHERE id = $2`, i, id); err != nil {
				return fmt.Errorf("update category position: %w", err)
			}
		}
		return nil
	})
}
