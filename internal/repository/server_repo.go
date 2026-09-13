package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

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

// GetByID fetches a server together with its channels. Used both internally
// (other feature services only care about OwnerID/DefaultRoleID, and just
// ignore Channels) and by the GET /v1/servers/:serverId handler (which needs
// the full representation).
func (r *ServerRepository) GetByID(ctx context.Context, id uuid.UUID) (model.Server, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, name, name_key, icon_url, banner_url, owner_id, created_at, default_role_id
		  FROM servers WHERE id = $1`, id)

	var s model.Server
	if err := row.Scan(&s.ID, &s.Name, &s.NameKey, &s.IconURL, &s.BannerURL, &s.OwnerID, &s.CreatedAt, &s.DefaultRoleID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Server{}, model.ErrServerNotFound
		}
		return model.Server{}, fmt.Errorf("select server: %w", err)
	}

	channels, err := r.channelsByServerID(ctx, id)
	if err != nil {
		return model.Server{}, err
	}
	s.Channels = channels

	catsByServer, err := r.categoriesByServerIDs(ctx, []uuid.UUID{id})
	if err != nil {
		return model.Server{}, err
	}
	s.Categories = catsByServer[id]
	return s, nil
}

func (r *ServerRepository) channelsByServerID(ctx context.Context, serverID uuid.UUID) ([]model.Channel, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, server_id, name, kind, position, category_id, topic, created_at
		  FROM channels WHERE server_id = $1 ORDER BY position`, serverID)
	if err != nil {
		return nil, fmt.Errorf("list channels: %w", err)
	}
	defer rows.Close()

	channels := make([]model.Channel, 0)
	for rows.Next() {
		var ch model.Channel
		var kind string
		if err := rows.Scan(&ch.ID, &ch.ServerID, &ch.Name, &kind, &ch.Position, &ch.CategoryID, &ch.Topic, &ch.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan channel: %w", err)
		}
		ch.Kind = model.ChannelKind(kind)
		channels = append(channels, ch)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list channels: %w", err)
	}
	return channels, nil
}

// categoriesByServerIDs fetches the categories of every server in serverIDs
// in a single query, grouped by server. Used both by GetByID (one id) and
// ListForUser (many), so the latter doesn't do one query per server.
func (r *ServerRepository) categoriesByServerIDs(ctx context.Context, serverIDs []uuid.UUID) (map[uuid.UUID][]model.Category, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, server_id, name, position, created_at
		  FROM categories WHERE server_id = ANY($1) ORDER BY position`, serverIDs)
	if err != nil {
		return nil, fmt.Errorf("list categories: %w", err)
	}
	defer rows.Close()

	byServer := make(map[uuid.UUID][]model.Category)
	for rows.Next() {
		var cat model.Category
		if err := rows.Scan(&cat.ID, &cat.ServerID, &cat.Name, &cat.Position, &cat.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan category: %w", err)
		}
		byServer[cat.ServerID] = append(byServer[cat.ServerID], cat)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list categories: %w", err)
	}
	return byServer, nil
}

func (r *ServerRepository) ListForUser(ctx context.Context, userID string) ([]model.Server, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT s.id, s.name, s.name_key, s.icon_url, s.banner_url, s.owner_id, s.created_at,
		       c.id, c.name, c.kind, c.position, c.category_id, c.topic, c.created_at
		  FROM servers s
		  JOIN server_members m ON m.server_id = s.id
		  LEFT JOIN channels c ON c.server_id = s.id
		 WHERE m.user_id = $1
		 ORDER BY m.joined_at DESC, c.position`, userID)
	if err != nil {
		return nil, fmt.Errorf("list servers for user: %w", err)
	}
	defer rows.Close()

	order := make([]uuid.UUID, 0)
	byID := make(map[uuid.UUID]*model.Server)

	for rows.Next() {
		var s model.Server
		var chID *uuid.UUID
		var chName, chKind *string
		var chPosition *int
		var chCategoryID *uuid.UUID
		var chTopic *string
		var chCreatedAt *time.Time

		if err := rows.Scan(
			&s.ID, &s.Name, &s.NameKey, &s.IconURL, &s.BannerURL, &s.OwnerID, &s.CreatedAt,
			&chID, &chName, &chKind, &chPosition, &chCategoryID, &chTopic, &chCreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan server row: %w", err)
		}

		srv, ok := byID[s.ID]
		if !ok {
			s.Channels = []model.Channel{}
			byID[s.ID] = &s
			srv = byID[s.ID]
			order = append(order, s.ID)
		}
		if chID != nil {
			srv.Channels = append(srv.Channels, model.Channel{
				ID: *chID, ServerID: srv.ID, Name: *chName,
				Kind: model.ChannelKind(*chKind), Position: *chPosition,
				CategoryID: chCategoryID, Topic: chTopic, CreatedAt: *chCreatedAt,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list servers for user: %w", err)
	}

	catsByServer, err := r.categoriesByServerIDs(ctx, order)
	if err != nil {
		return nil, err
	}

	servers := make([]model.Server, 0, len(order))
	for _, id := range order {
		srv := *byID[id]
		srv.Categories = catsByServer[id]
		servers = append(servers, srv)
	}
	return servers, nil
}

func (r *ServerRepository) UpdateServer(ctx context.Context, id uuid.UUID, name, nameKey string, iconURL, bannerURL *string) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE servers SET name = $1, name_key = $2, icon_url = $3, banner_url = $4
		 WHERE id = $5`,
		name, nameKey, iconURL, bannerURL, id,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation && pgErr.ConstraintName == "uq_servers_owner_name_key" {
			return model.ErrNameTaken
		}
		return fmt.Errorf("update server: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return model.ErrServerNotFound
	}
	return nil
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
