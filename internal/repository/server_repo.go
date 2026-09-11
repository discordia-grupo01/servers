package repository

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

type ServerRepository struct {
	pool *pgxpool.Pool
}

func NewServerRepository(pool *pgxpool.Pool) *ServerRepository {
	return &ServerRepository{pool: pool}
}

func (r *ServerRepository) CreateServer(ctx context.Context, srv model.Server, channels []model.Channel) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO servers (id, name, name_key, icon_url, owner_id, created_at)
			VALUES ($1, $2, $3, $4, $5, $6)`,
			srv.ID, srv.Name, srv.NameKey, srv.IconURL, srv.OwnerID, srv.CreatedAt,
		); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation &&
				pgErr.ConstraintName == "uq_servers_owner_name_key" {
				return model.ErrNameTaken
			}
			return fmt.Errorf("insert server: %w", err)
		}

		for _, ch := range channels {
			if _, err := tx.Exec(ctx, `
				INSERT INTO channels (id, server_id, name, kind, position, created_at)
				VALUES ($1, $2, $3, $4, $5, $6)`,
				ch.ID, ch.ServerID, ch.Name, string(ch.Kind), ch.Position, ch.CreatedAt,
			); err != nil {
				return fmt.Errorf("insert channel %s: %w", ch.Name, err)
			}
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO server_members (server_id, user_id, is_owner, joined_at)
			VALUES ($1, $2, TRUE, $3)`,
			srv.ID, srv.OwnerID, srv.CreatedAt,
		); err != nil {
			return fmt.Errorf("insert owner membership: %w", err)
		}
		return nil
	})
}

func (r *ServerRepository) GetByID(ctx context.Context, id uuid.UUID) (model.Server, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, name, name_key, icon_url, owner_id, created_at, default_role_id
		  FROM servers WHERE id = $1`, id)

	var s model.Server
	if err := row.Scan(&s.ID, &s.Name, &s.NameKey, &s.IconURL, &s.OwnerID, &s.CreatedAt, &s.DefaultRoleID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Server{}, model.ErrServerNotFound
		}
		return model.Server{}, fmt.Errorf("select server: %w", err)
	}
	return s, nil
}

func (r *ServerRepository) SetDefaultRole(ctx context.Context, serverID, roleID uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `UPDATE servers SET default_role_id = $1 WHERE id = $2`, roleID, serverID)
	if err != nil {
		return fmt.Errorf("set default role: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return model.ErrServerNotFound
	}
	return nil
}
