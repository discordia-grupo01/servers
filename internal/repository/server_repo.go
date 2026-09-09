package repository

import (
	"context"
	"errors"
	"fmt"

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
