package repository

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/discordia-grupo01/events-contract/channels"
	"github.com/discordia-grupo01/events-contract/envelope"
	"github.com/discordia-grupo01/servers/internal/model/channelperm"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/outbox"
)

// AccessScope narrows what an AccessSnapshot looks at, so a write that can
// only change one channel or one member doesn't load the whole server. The
// zero value takes every channel and every member.
type AccessScope struct {
	// ChannelID, if set, limits it to that channel (an override change).
	ChannelID *uuid.UUID
	// UserID, if set, limits it to that member (kick, ban, leave).
	UserID string
}

// Querier is what loading an AccessSnapshot needs, satisfied by both the pool
// and a transaction.
type Querier interface {
	RowQuerier
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// LoadAccessSnapshot reads who is in serverID, with which roles, and the
// overrides of its channels, within scope. A server that doesn't exist gives
// an empty snapshot.
func LoadAccessSnapshot(ctx context.Context, q Querier, serverID uuid.UUID, scope AccessScope) (channelperm.AccessSnapshot, error) {
	snap := channelperm.AccessSnapshot{
		Members:   make(map[string]channelperm.MemberAccess),
		Overrides: make(map[uuid.UUID]channelperm.ChannelOverrides),
	}

	var ownerID string
	err := q.QueryRow(ctx, `SELECT owner_id FROM servers WHERE id = $1`, serverID).Scan(&ownerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return snap, nil
	}
	if err != nil {
		return channelperm.AccessSnapshot{}, fmt.Errorf("access snapshot owner: %w", err)
	}

	if err := loadMembers(ctx, q, serverID, ownerID, scope.UserID, snap.Members); err != nil {
		return channelperm.AccessSnapshot{}, err
	}
	if snap.Channels, err = loadChannels(ctx, q, serverID, scope.ChannelID); err != nil {
		return channelperm.AccessSnapshot{}, err
	}
	if err := loadOverrides(ctx, q, serverID, scope.ChannelID, snap.Overrides); err != nil {
		return channelperm.AccessSnapshot{}, err
	}
	return snap, nil
}

// loadMembers fills members with the members of serverID (only userID if
// set): the union of their roles' permissions and their role ids.
func loadMembers(ctx context.Context, q Querier, serverID uuid.UUID, ownerID, userID string, members map[string]channelperm.MemberAccess) error {
	rows, err := q.Query(ctx, `
		SELECT sm.user_id, mr.role_id, COALESCE(r.permissions, 0)
		  FROM server_members sm
		  LEFT JOIN member_roles mr ON mr.server_id = sm.server_id AND mr.user_id = sm.user_id
		  LEFT JOIN roles r ON r.id = mr.role_id
		 WHERE sm.server_id = $1 AND ($2::text = '' OR sm.user_id = $2)`,
		serverID, userID)
	if err != nil {
		return fmt.Errorf("access snapshot members: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var memberID string
		var roleID *uuid.UUID
		var perms role.Permission
		if err := rows.Scan(&memberID, &roleID, &perms); err != nil {
			return fmt.Errorf("scan access member: %w", err)
		}
		m, ok := members[memberID]
		if !ok {
			m.Standing = role.Standing{IsMember: true, IsOwner: memberID == ownerID}
		}
		if roleID != nil {
			m.Standing.Permissions |= perms
			m.RoleIDs = append(m.RoleIDs, *roleID)
		}
		members[memberID] = m
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("access snapshot members: %w", err)
	}
	return nil
}

func loadChannels(ctx context.Context, q Querier, serverID uuid.UUID, channelID *uuid.UUID) ([]uuid.UUID, error) {
	rows, err := q.Query(ctx, `
		SELECT id FROM channels
		 WHERE server_id = $1 AND ($2::uuid IS NULL OR id = $2)
		 ORDER BY id`,
		serverID, channelID)
	if err != nil {
		return nil, fmt.Errorf("access snapshot channels: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, fmt.Errorf("access snapshot channels: %w", err)
	}
	return ids, nil
}

func loadOverrides(ctx context.Context, q Querier, serverID uuid.UUID, channelID *uuid.UUID, overrides map[uuid.UUID]channelperm.ChannelOverrides) error {
	rows, err := q.Query(ctx, `
		SELECT o.channel_id, o.role_id, o.allow, o.deny, o.updated_at, r.is_everyone
		  FROM channel_permission_overrides o
		  JOIN channels c ON c.id = o.channel_id
		  JOIN roles r ON r.id = o.role_id
		 WHERE c.server_id = $1 AND ($2::uuid IS NULL OR c.id = $2)`,
		serverID, channelID)
	if err != nil {
		return fmt.Errorf("access snapshot overrides: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var o channelperm.Override
		var isEveryone bool
		if err := rows.Scan(&o.ChannelID, &o.RoleID, &o.Allow, &o.Deny, &o.UpdatedAt, &isEveryone); err != nil {
			return fmt.Errorf("scan access override: %w", err)
		}
		c := overrides[o.ChannelID]
		if isEveryone {
			c.Everyone = o
		} else {
			if c.Roles == nil {
				c.Roles = make(map[uuid.UUID]channelperm.Override)
			}
			c.Roles[o.RoleID] = o
		}
		overrides[o.ChannelID] = c
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("access snapshot overrides: %w", err)
	}
	return nil
}

// TrackChannelAccess runs write inside tx and publishes, through the outbox of
// that same transaction, a servers.channel_access_revoked for every channel of
// serverID (within scope) where somebody who could see it no longer can. It
// compares who sees each channel right before and right after write, so the
// caller must already hold whatever lock its write takes on the server.
// write's error is returned as-is, with nothing published.
func TrackChannelAccess(ctx context.Context, tx pgx.Tx, serverID uuid.UUID, scope AccessScope, write func() error) error {
	before, err := LoadAccessSnapshot(ctx, tx, serverID, scope)
	if err != nil {
		return err
	}
	if err := write(); err != nil {
		return err
	}
	after, err := LoadAccessSnapshot(ctx, tx, serverID, scope)
	if err != nil {
		return err
	}

	revoked := channelperm.Revoked(before.Viewers(), after.Viewers())
	channelIDs := make([]uuid.UUID, 0, len(revoked))
	for id := range revoked {
		channelIDs = append(channelIDs, id)
	}
	sort.Slice(channelIDs, func(i, j int) bool { return channelIDs[i].String() < channelIDs[j].String() })

	now := time.Now().UTC()
	for _, channelID := range channelIDs {
		evt, err := outbox.New(channels.RoutingKeyChannelAccessRevoked, channels.ChannelAccessRevoked{
			Meta:      envelope.Meta{EventID: uuid.NewString(), OccurredAt: now},
			ServerID:  serverID.String(),
			ChannelID: channelID.String(),
			UserIDs:   revoked[channelID],
		})
		if err != nil {
			return err
		}
		if err := outbox.Insert(ctx, tx, evt); err != nil {
			return err
		}
	}
	return nil
}
