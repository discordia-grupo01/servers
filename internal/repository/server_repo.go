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
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/outbox"
)

const pgUniqueViolation = "23505"

type ServerRepository struct {
	pool *pgxpool.Pool
}

func NewServerRepository(pool *pgxpool.Pool) *ServerRepository {
	return &ServerRepository{pool: pool}
}

// CreateServer inserts the server together with its native categories
// (categories comes first, so channels below can FK into them), its default
// channels, the owner's membership row and the outbox event for that
// membership, all in one transaction -- so the MemberJoined event is
// durable the instant the server exists, never dependent on a subsequent
// RabbitMQ publish succeeding.
func (r *ServerRepository) CreateServer(ctx context.Context, srv model.Server, categories []model.Category, channels []model.Channel, outboxEvents []outbox.Event) error {
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

		for _, cat := range categories {
			if _, err := tx.Exec(ctx, `
				INSERT INTO categories (id, server_id, name, name_key, position, is_fallback, created_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7)`,
				cat.ID, cat.ServerID, cat.Name, cat.NameKey, cat.Position, cat.IsFallback, cat.CreatedAt,
			); err != nil {
				return fmt.Errorf("insert category %s: %w", cat.Name, err)
			}
		}

		for _, ch := range channels {
			if _, err := tx.Exec(ctx, `
				INSERT INTO channels (id, server_id, name, kind, position, category_id, created_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7)`,
				ch.ID, ch.ServerID, ch.Name, string(ch.Kind), ch.Position, ch.CategoryID, ch.CreatedAt,
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

		// @everyone: created with every new server
		everyoneRoleID := uuid.New()
		if _, err := tx.Exec(ctx, `
			INSERT INTO roles (id, server_id, name, color, permissions, position, is_everyone, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, 0, TRUE, $6, $6)`,
			everyoneRoleID, srv.ID, role.EveryoneRoleName, role.EveryoneRoleColor,
			role.DefaultPermissions|role.PermCreateInvite, srv.CreatedAt,
		); err != nil {
			return fmt.Errorf("insert everyone role: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO member_roles (server_id, user_id, role_id, assigned_at)
			VALUES ($1, $2, $3, $4)`,
			srv.ID, srv.OwnerID, everyoneRoleID, srv.CreatedAt,
		); err != nil {
			return fmt.Errorf("assign everyone role to owner: %w", err)
		}
		if err := PublishOwner(ctx, tx, srv.ID, srv.OwnerID); err != nil {
			return err
		}
		if err := PublishServerUpdated(ctx, tx, srv.ID); err != nil {
			return err
		}
		if err := PublishRoleUpdated(ctx, tx, everyoneRoleID); err != nil {
			return err
		}
		if err := PublishMemberRoles(ctx, tx, srv.ID, srv.OwnerID); err != nil {
			return err
		}

		for _, evt := range outboxEvents {
			if err := outbox.Insert(ctx, tx, evt); err != nil {
				return err
			}
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

func (r *ServerRepository) IconURLs(ctx context.Context, id uuid.UUID) (iconURL, bannerURL *string, err error) {
	err = r.pool.QueryRow(ctx, `
		SELECT icon_url, banner_url FROM servers WHERE id = $1`, id,
	).Scan(&iconURL, &bannerURL)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, model.ErrServerNotFound
		}
		return nil, nil, fmt.Errorf("select server icon urls: %w", err)
	}
	return iconURL, bannerURL, nil
}

func (r *ServerRepository) channelsByServerID(ctx context.Context, serverID uuid.UUID) ([]model.Channel, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, server_id, name, kind, position, category_id, topic, created_at
		  FROM channels WHERE server_id = $1 ORDER BY position, id`, serverID)
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
		SELECT id, server_id, name, position, is_fallback, created_at
		  FROM categories WHERE server_id = ANY($1) ORDER BY position`, serverIDs)
	if err != nil {
		return nil, fmt.Errorf("list categories: %w", err)
	}
	defer rows.Close()

	byServer := make(map[uuid.UUID][]model.Category)
	for rows.Next() {
		var cat model.Category
		if err := rows.Scan(&cat.ID, &cat.ServerID, &cat.Name, &cat.Position, &cat.IsFallback, &cat.CreatedAt); err != nil {
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
		 ORDER BY m.joined_at DESC, c.position, c.id`, userID)
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

// UpdateServer rewrites the server's editable fields. The previous name is
// read under a row lock, in the same transaction as the UPDATE, so
// servers.server_updated is written to the outbox atomically and only when the
// name actually changed -- an icon or banner change publishes nothing.
func (r *ServerRepository) UpdateServer(ctx context.Context, id uuid.UUID, name, nameKey string, iconURL, bannerURL *string) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var previousName string
		if err := tx.QueryRow(ctx, `SELECT name FROM servers WHERE id = $1 FOR UPDATE`, id).Scan(&previousName); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return model.ErrServerNotFound
			}
			return fmt.Errorf("lock server: %w", err)
		}

		if _, err := tx.Exec(ctx, `
			UPDATE servers SET name = $1, name_key = $2, icon_url = $3, banner_url = $4
			 WHERE id = $5`,
			name, nameKey, iconURL, bannerURL, id,
		); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation && pgErr.ConstraintName == "uq_servers_owner_name_key" {
				return model.ErrNameTaken
			}
			return fmt.Errorf("update server: %w", err)
		}

		if name == previousName {
			return nil
		}
		return PublishServerUpdated(ctx, tx, id)
	})
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

func (r *ServerRepository) DeleteServer(ctx context.Context, id uuid.UUID, buildEvents func(memberIDs []string, channelIDs []uuid.UUID) ([]outbox.Event, error)) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var locked uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM servers WHERE id = $1 FOR UPDATE`, id).Scan(&locked); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return model.ErrServerNotFound
			}
			return fmt.Errorf("lock server: %w", err)
		}

		memberRows, err := tx.Query(ctx, `SELECT user_id FROM server_members WHERE server_id = $1`, id)
		if err != nil {
			return fmt.Errorf("list members: %w", err)
		}
		memberIDs, err := pgx.CollectRows(memberRows, pgx.RowTo[string])
		if err != nil {
			return fmt.Errorf("scan members: %w", err)
		}

		channelRows, err := tx.Query(ctx, `SELECT id FROM channels WHERE server_id = $1`, id)
		if err != nil {
			return fmt.Errorf("list channels: %w", err)
		}
		channelIDs, err := pgx.CollectRows(channelRows, pgx.RowTo[uuid.UUID])
		if err != nil {
			return fmt.Errorf("scan channels: %w", err)
		}

		events, err := buildEvents(memberIDs, channelIDs)
		if err != nil {
			return err
		}

		if _, err := tx.Exec(ctx, `DELETE FROM servers WHERE id = $1`, id); err != nil {
			return fmt.Errorf("delete server: %w", err)
		}

		for _, evt := range events {
			if err := outbox.Insert(ctx, tx, evt); err != nil {
				return err
			}
		}
		return nil
	})
}
