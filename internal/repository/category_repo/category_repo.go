// Package category_repo implements Postgres access for the Category domain type.
package category_repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/discordia-grupo01/servers/internal/model"
)

const pgUniqueViolation = "23505"

func nameTakenErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation && pgErr.ConstraintName == "uq_categories_server_name_key" {
		return model.ErrCategoryNameTaken
	}
	return nil
}

type CategoryRepository struct {
	pool *pgxpool.Pool
}

func NewCategoryRepository(pool *pgxpool.Pool) *CategoryRepository {
	return &CategoryRepository{pool: pool}
}

// defaultKindString converts a *model.ChannelKind to the *string the
// default_kind column expects (nil stays nil), same pattern already used for
// Channel.Kind elsewhere in this repo layer (pgx scans/binds plain strings,
// not this package's named string types).
func defaultKindString(k *model.ChannelKind) *string {
	if k == nil {
		return nil
	}
	s := string(*k)
	return &s
}

func defaultKindFromString(s *string) *model.ChannelKind {
	if s == nil {
		return nil
	}
	k := model.ChannelKind(*s)
	return &k
}

func (r *CategoryRepository) Create(ctx context.Context, cat model.Category) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO categories (id, server_id, name, name_key, position, default_kind, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		cat.ID, cat.ServerID, cat.Name, cat.NameKey, cat.Position, defaultKindString(cat.DefaultKind), cat.CreatedAt,
	)
	if err != nil {
		if taken := nameTakenErr(err); taken != nil {
			return taken
		}
		return fmt.Errorf("insert category: %w", err)
	}
	return nil
}

func (r *CategoryRepository) GetByID(ctx context.Context, id uuid.UUID) (model.Category, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, server_id, name, position, default_kind, created_at
		  FROM categories WHERE id = $1`, id)

	var cat model.Category
	var defaultKind *string
	if err := row.Scan(&cat.ID, &cat.ServerID, &cat.Name, &cat.Position, &defaultKind, &cat.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Category{}, model.ErrCategoryNotFound
		}
		return model.Category{}, fmt.Errorf("select category: %w", err)
	}
	cat.DefaultKind = defaultKindFromString(defaultKind)
	return cat, nil
}

func (r *CategoryRepository) ListByServer(ctx context.Context, serverID uuid.UUID) ([]model.Category, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, server_id, name, position, default_kind, created_at
		  FROM categories WHERE server_id = $1 ORDER BY position`, serverID)
	if err != nil {
		return nil, fmt.Errorf("list categories: %w", err)
	}
	defer rows.Close()

	categories := make([]model.Category, 0)
	for rows.Next() {
		var cat model.Category
		var defaultKind *string
		if err := rows.Scan(&cat.ID, &cat.ServerID, &cat.Name, &cat.Position, &defaultKind, &cat.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan category: %w", err)
		}
		cat.DefaultKind = defaultKindFromString(defaultKind)
		categories = append(categories, cat)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list categories: %w", err)
	}
	return categories, nil
}

func (r *CategoryRepository) Update(ctx context.Context, id uuid.UUID, name, nameKey string) error {
	tag, err := r.pool.Exec(ctx, `UPDATE categories SET name = $1, name_key = $2 WHERE id = $3`, name, nameKey, id)
	if err != nil {
		if taken := nameTakenErr(err); taken != nil {
			return taken
		}
		return fmt.Errorf("update category: %w", err)
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
