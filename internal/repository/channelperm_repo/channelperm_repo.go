// Package channelperm_repo implements Postgres access for per-channel
// permission overrides (#57).
package channelperm_repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/channelperm"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/repository"
)

const (
	pgForeignKeyViolation = "23503"
	fkChannel             = "channel_permission_overrides_channel_id_fkey"
	fkRole                = "channel_permission_overrides_role_id_fkey"
)

// Repository's writes take a check that decides, inside the write
// transaction, whether the actor may change the overrides of a role. It gets
// the actor's standing and the role as they are under the server lock, and
// its error is returned as-is with nothing written.
type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// Upsert creates or replaces the override of o.RoleID in o.ChannelID and
// returns it as stored. o must not be empty (the table rejects it: use
// Delete). Like editing a role, it holds the server row FOR SHARE, so it
// can't race a role reorder, and runs check with the actor's standing and the
// role re-read under that lock. Members who stop seeing the channel are
// published as servers.channel_access_revoked (repository.TrackChannelAccess).
func (r *Repository) Upsert(ctx context.Context, serverID uuid.UUID, actorID string, o channelperm.Override, check func(actor role.Standing, rl role.Role) error) (channelperm.Override, error) {
	var saved channelperm.Override
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if err := lockAndCheck(ctx, tx, serverID, actorID, o.ChannelID, o.RoleID, check); err != nil {
			return err
		}
		return repository.TrackChannelAccess(ctx, tx, serverID, repository.AccessScope{ChannelID: &o.ChannelID}, func() error {
			err := tx.QueryRow(ctx, `
				INSERT INTO channel_permission_overrides (channel_id, role_id, allow, deny, updated_at)
				VALUES ($1, $2, $3, $4, $5)
				ON CONFLICT (channel_id, role_id)
				DO UPDATE SET allow = EXCLUDED.allow, deny = EXCLUDED.deny, updated_at = EXCLUDED.updated_at
				RETURNING channel_id, role_id, allow, deny, updated_at`,
				o.ChannelID, o.RoleID, o.Allow, o.Deny, o.UpdatedAt,
			).Scan(&saved.ChannelID, &saved.RoleID, &saved.Allow, &saved.Deny, &saved.UpdatedAt)
			if err != nil {
				return mapFKError(err, "upsert override")
			}
			return nil
		})
	})
	return saved, err
}

// Delete removes the override of roleID in channelID, with the same locking
// and check as Upsert (and the same channel_access_revoked: removing an allow
// can hide the channel). deleted is false (and err nil) if there was none.
func (r *Repository) Delete(ctx context.Context, serverID uuid.UUID, actorID string, channelID, roleID uuid.UUID, check func(actor role.Standing, rl role.Role) error) (deleted bool, err error) {
	err = pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if err := lockAndCheck(ctx, tx, serverID, actorID, channelID, roleID, check); err != nil {
			return err
		}
		return repository.TrackChannelAccess(ctx, tx, serverID, repository.AccessScope{ChannelID: &channelID}, func() error {
			tag, err := tx.Exec(ctx,
				`DELETE FROM channel_permission_overrides WHERE channel_id = $1 AND role_id = $2`,
				channelID, roleID,
			)
			if err != nil {
				return fmt.Errorf("delete override: %w", err)
			}
			deleted = tag.RowsAffected() > 0
			return nil
		})
	})
	return deleted, err
}

// ListByChannel returns every override of channelID, in the order of the
// role hierarchy (@everyone first, since it has position 0).
func (r *Repository) ListByChannel(ctx context.Context, channelID uuid.UUID) ([]channelperm.Override, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT o.channel_id, o.role_id, o.allow, o.deny, o.updated_at
		  FROM channel_permission_overrides o
		  JOIN roles r ON r.id = o.role_id
		 WHERE o.channel_id = $1
		 ORDER BY r.position, r.id`, channelID)
	if err != nil {
		return nil, fmt.Errorf("list overrides: %w", err)
	}
	defer rows.Close()

	overrides := make([]channelperm.Override, 0)
	for rows.Next() {
		var o channelperm.Override
		if err := rows.Scan(&o.ChannelID, &o.RoleID, &o.Allow, &o.Deny, &o.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan override: %w", err)
		}
		overrides = append(overrides, o)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list overrides: %w", err)
	}
	return overrides, nil
}

// ForMember returns the overrides of channelID that apply to userID in
// serverID, split the way channelperm.Effective takes them: the one of
// @everyone (the zero Override if it has none) and the ones of the user's
// other roles. A non-member gets none.
func (r *Repository) ForMember(ctx context.Context, serverID, channelID uuid.UUID, userID string) (everyone channelperm.Override, roles []channelperm.Override, err error) {
	rows, err := r.pool.Query(ctx, `
		SELECT o.channel_id, o.role_id, o.allow, o.deny, o.updated_at, r.is_everyone
		  FROM channel_permission_overrides o
		  JOIN member_roles mr ON mr.role_id = o.role_id AND mr.server_id = $1 AND mr.user_id = $3
		  JOIN roles r ON r.id = o.role_id
		 WHERE o.channel_id = $2`,
		serverID, channelID, userID)
	if err != nil {
		return channelperm.Override{}, nil, fmt.Errorf("member overrides: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var o channelperm.Override
		var isEveryone bool
		if err := rows.Scan(&o.ChannelID, &o.RoleID, &o.Allow, &o.Deny, &o.UpdatedAt, &isEveryone); err != nil {
			return channelperm.Override{}, nil, fmt.Errorf("scan override: %w", err)
		}
		if isEveryone {
			everyone = o
		} else {
			roles = append(roles, o)
		}
	}
	if err := rows.Err(); err != nil {
		return channelperm.Override{}, nil, fmt.Errorf("member overrides: %w", err)
	}
	return everyone, roles, nil
}

// MemberStandings is repository.MemberStanding for several servers at once,
// in a single query: whether userID is a member of each of serverIDs, the
// union of their roles' permissions there and their highest role. It never
// sets IsOwner (ownership comes from the server row). Servers that don't
// exist are absent from the result.
func (r *Repository) MemberStandings(ctx context.Context, userID string, serverIDs []uuid.UUID) (map[uuid.UUID]role.Standing, error) {
	// @everyone's reserved position 0 is left out of MIN, like in
	// repository.MemberStanding.
	rows, err := r.pool.Query(ctx, `
		SELECT s.id,
		       EXISTS (SELECT 1 FROM server_members sm WHERE sm.server_id = s.id AND sm.user_id = $2),
		       COALESCE(BIT_OR(r.permissions), 0),
		       MIN(r.position) FILTER (WHERE NOT r.is_everyone)
		  FROM servers s
		  LEFT JOIN member_roles mr ON mr.server_id = s.id AND mr.user_id = $2
		  LEFT JOIN roles r ON r.id = mr.role_id
		 WHERE s.id = ANY($1)
		 GROUP BY s.id`,
		serverIDs, userID)
	if err != nil {
		return nil, fmt.Errorf("member standings: %w", err)
	}
	defer rows.Close()

	standings := make(map[uuid.UUID]role.Standing, len(serverIDs))
	for rows.Next() {
		var serverID uuid.UUID
		var st role.Standing
		if err := rows.Scan(&serverID, &st.IsMember, &st.Permissions, &st.TopPosition); err != nil {
			return nil, fmt.Errorf("scan standing: %w", err)
		}
		standings[serverID] = st
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("member standings: %w", err)
	}
	return standings, nil
}

// ChannelLayers is ForMember for every channel of serverIDs at once, in a
// single query: for each channel with an override for one of userID's roles,
// those overrides split into @everyone's and the rest. Channels without any
// are absent (their zero Layers mean "no override").
func (r *Repository) ChannelLayers(ctx context.Context, userID string, serverIDs []uuid.UUID) (map[uuid.UUID]channelperm.Layers, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT o.channel_id, o.role_id, o.allow, o.deny, o.updated_at, r.is_everyone
		  FROM channel_permission_overrides o
		  JOIN channels c ON c.id = o.channel_id
		  JOIN member_roles mr ON mr.role_id = o.role_id AND mr.server_id = c.server_id AND mr.user_id = $2
		  JOIN roles r ON r.id = o.role_id
		 WHERE c.server_id = ANY($1)`,
		serverIDs, userID)
	if err != nil {
		return nil, fmt.Errorf("channel layers: %w", err)
	}
	defer rows.Close()

	layers := make(map[uuid.UUID]channelperm.Layers)
	for rows.Next() {
		var o channelperm.Override
		var isEveryone bool
		if err := rows.Scan(&o.ChannelID, &o.RoleID, &o.Allow, &o.Deny, &o.UpdatedAt, &isEveryone); err != nil {
			return nil, fmt.Errorf("scan override: %w", err)
		}
		l := layers[o.ChannelID]
		if isEveryone {
			l.Everyone = o
		} else {
			l.Roles = append(l.Roles, o)
		}
		layers[o.ChannelID] = l
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("channel layers: %w", err)
	}
	return layers, nil
}

// lockAndCheck takes the server row FOR SHARE (the lock role edits take, see
// repository.LockServerShared), re-reads the actor's standing and the role,
// confirms the channel and the role still belong to serverID and runs check.
func lockAndCheck(ctx context.Context, tx pgx.Tx, serverID uuid.UUID, actorID string, channelID, roleID uuid.UUID, check func(actor role.Standing, rl role.Role) error) error {
	var ownerID string
	err := tx.QueryRow(ctx, `SELECT owner_id FROM servers WHERE id = $1 FOR SHARE`, serverID).Scan(&ownerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.ErrServerNotFound
	}
	if err != nil {
		return fmt.Errorf("lock server: %w", err)
	}

	var channelServer uuid.UUID
	err = tx.QueryRow(ctx, `SELECT server_id FROM channels WHERE id = $1`, channelID).Scan(&channelServer)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.ErrChannelNotFound
	}
	if err != nil {
		return fmt.Errorf("select channel: %w", err)
	}
	if channelServer != serverID {
		return model.ErrChannelServerMismatch
	}

	var rl role.Role
	err = tx.QueryRow(ctx, `
		SELECT id, server_id, name, color, permissions, position, is_everyone, created_at, updated_at
		  FROM roles WHERE id = $1`, roleID,
	).Scan(&rl.ID, &rl.ServerID, &rl.Name, &rl.Color, &rl.Permissions, &rl.Position, &rl.IsEveryone, &rl.CreatedAt, &rl.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.ErrRoleNotFound
	}
	if err != nil {
		return fmt.Errorf("select role: %w", err)
	}
	if rl.ServerID != serverID {
		return model.ErrRoleServerMismatch
	}

	actor, err := repository.StandingOf(ctx, tx, serverID, ownerID, actorID)
	if err != nil {
		return err
	}
	return check(actor, rl)
}

// mapFKError turns the channel or the role being deleted concurrently (after
// lockAndCheck read them) into the same 404 as if they were already gone.
func mapFKError(err error, op string) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgForeignKeyViolation {
		switch pgErr.ConstraintName {
		case fkChannel:
			return model.ErrChannelNotFound
		case fkRole:
			return model.ErrRoleNotFound
		}
	}
	return fmt.Errorf("%s: %w", op, err)
}
