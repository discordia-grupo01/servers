package voicebackfill

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/discordia-grupo01/events-contract/channels"
	"github.com/discordia-grupo01/events-contract/envelope"
	"github.com/discordia-grupo01/events-contract/moderation"
	"github.com/discordia-grupo01/events-contract/permissions"
	"github.com/discordia-grupo01/events-contract/users"
	"github.com/discordia-grupo01/servers/internal/model/role"
)

type Publisher interface {
	Publish(ctx context.Context, routingKey string, payload any) error
}

type Counts struct {
	Channels  int
	Roles     int
	Owners    int
	Members   int
	Overrides int
	Mutes     int
	Profiles  int
}

func (c Counts) Total() int {
	return c.Channels + c.Roles + c.Owners + c.Members + c.Overrides + c.Mutes + c.Profiles
}

func Run(ctx context.Context, pool *pgxpool.Pool, publisher Publisher) (Counts, error) {
	var counts Counts

	if err := publishChannels(ctx, pool, publisher, &counts); err != nil {
		return counts, err
	}
	if err := publishRoles(ctx, pool, publisher, &counts); err != nil {
		return counts, err
	}
	if err := publishOwners(ctx, pool, publisher, &counts); err != nil {
		return counts, err
	}
	if err := publishMembers(ctx, pool, publisher, &counts); err != nil {
		return counts, err
	}
	if err := publishProfiles(ctx, pool, publisher, &counts); err != nil {
		return counts, err
	}
	if err := publishOverrides(ctx, pool, publisher, &counts); err != nil {
		return counts, err
	}
	if err := publishMutes(ctx, pool, publisher, &counts); err != nil {
		return counts, err
	}
	return counts, nil
}

func publishChannels(ctx context.Context, pool *pgxpool.Pool, publisher Publisher, counts *Counts) error {
	rows, err := pool.Query(ctx, `
		SELECT id, server_id, name, kind, created_at
		  FROM channels
		 WHERE kind = 'voice'
		 ORDER BY id`)
	if err != nil {
		return fmt.Errorf("backfill voice channels: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var channelID, serverID uuid.UUID
		var name, kind string
		var occurredAt time.Time
		if err := rows.Scan(&channelID, &serverID, &name, &kind, &occurredAt); err != nil {
			return fmt.Errorf("scan voice channel: %w", err)
		}
		if err := publisher.Publish(ctx, channels.RoutingKeyChannelCreated, channels.ChannelCreated{
			Meta:      meta(occurredAt),
			ServerID:  serverID.String(),
			ChannelID: channelID.String(),
			Name:      name,
			Kind:      kind,
		}); err != nil {
			return err
		}
		counts.Channels++
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read voice channels: %w", err)
	}
	return nil
}

func publishRoles(ctx context.Context, pool *pgxpool.Pool, publisher Publisher, counts *Counts) error {
	rows, err := pool.Query(ctx, `
		SELECT id, server_id, permissions, is_everyone, updated_at
		  FROM roles
		 ORDER BY id`)
	if err != nil {
		return fmt.Errorf("backfill roles: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var roleID, serverID uuid.UUID
		var mask role.Permission
		var isEveryone bool
		var occurredAt time.Time
		if err := rows.Scan(&roleID, &serverID, &mask, &isEveryone, &occurredAt); err != nil {
			return fmt.Errorf("scan role: %w", err)
		}
		if err := publisher.Publish(ctx, permissions.RoutingKeyRoleUpdated, permissions.RoleUpdated{
			Meta:        meta(occurredAt),
			ServerID:    serverID.String(),
			RoleID:      roleID.String(),
			Permissions: role.PermissionNames(mask),
			IsEveryone:  isEveryone,
		}); err != nil {
			return err
		}
		counts.Roles++
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read roles: %w", err)
	}
	return nil
}

func publishOwners(ctx context.Context, pool *pgxpool.Pool, publisher Publisher, counts *Counts) error {
	rows, err := pool.Query(ctx, `
		SELECT s.id, s.owner_id,
		       COALESCE(MAX(ot.resolved_at) FILTER (WHERE ot.status = 'accepted'), s.created_at)
		  FROM servers s
		  LEFT JOIN ownership_transfers ot ON ot.server_id = s.id
		 GROUP BY s.id, s.owner_id, s.created_at
		 ORDER BY s.id`)
	if err != nil {
		return fmt.Errorf("backfill owners: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var serverID, ownerID string
		var occurredAt time.Time
		if err := rows.Scan(&serverID, &ownerID, &occurredAt); err != nil {
			return fmt.Errorf("scan owner: %w", err)
		}
		if err := publisher.Publish(ctx, permissions.RoutingKeyOwnerUpdated, permissions.OwnerUpdated{
			Meta:     meta(occurredAt),
			ServerID: serverID,
			OwnerID:  ownerID,
		}); err != nil {
			return err
		}
		counts.Owners++
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read owners: %w", err)
	}
	return nil
}

func publishMembers(ctx context.Context, pool *pgxpool.Pool, publisher Publisher, counts *Counts) error {
	rows, err := pool.Query(ctx, `
		SELECT sm.server_id, sm.user_id,
		       COALESCE(array_agg(mr.role_id::text ORDER BY mr.role_id)
		                FILTER (WHERE mr.role_id IS NOT NULL), '{}'),
		       COALESCE(MAX(mr.assigned_at), sm.joined_at)
		  FROM server_members sm
		  LEFT JOIN member_roles mr
		    ON mr.server_id = sm.server_id AND mr.user_id = sm.user_id
		 GROUP BY sm.server_id, sm.user_id, sm.joined_at
		 ORDER BY sm.server_id, sm.user_id`)
	if err != nil {
		return fmt.Errorf("backfill member roles: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var serverID, userID string
		var roleIDs []string
		var occurredAt time.Time
		if err := rows.Scan(&serverID, &userID, &roleIDs, &occurredAt); err != nil {
			return fmt.Errorf("scan member roles: %w", err)
		}
		if roleIDs == nil {
			roleIDs = []string{}
		}
		if err := publisher.Publish(ctx, permissions.RoutingKeyMemberRolesUpdated, permissions.MemberRolesUpdated{
			Meta:     meta(occurredAt),
			ServerID: serverID,
			UserID:   userID,
			RoleIDs:  roleIDs,
		}); err != nil {
			return err
		}
		counts.Members++
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read member roles: %w", err)
	}
	return nil
}

func publishProfiles(ctx context.Context, pool *pgxpool.Pool, publisher Publisher, counts *Counts) error {
	rows, err := pool.Query(ctx, `
		SELECT user_id, name, avatar_url, description, status_text, status_emoji, version, updated_at
		  FROM user_profiles
		 ORDER BY user_id`)
	if err != nil {
		return fmt.Errorf("backfill user profiles: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var userID, name, avatarURL, description, statusText, statusEmoji string
		var version int64
		var occurredAt time.Time
		if err := rows.Scan(
			&userID, &name, &avatarURL, &description, &statusText, &statusEmoji,
			&version, &occurredAt,
		); err != nil {
			return fmt.Errorf("scan user profile: %w", err)
		}
		if err := publisher.Publish(ctx, users.RoutingKeyProfileUpdated, users.ProfileUpdated{
			Meta:        meta(occurredAt),
			UserID:      userID,
			Version:     version,
			Name:        name,
			AvatarURL:   avatarURL,
			Description: description,
			StatusText:  statusText,
			StatusEmoji: statusEmoji,
		}); err != nil {
			return err
		}
		counts.Profiles++
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read user profiles: %w", err)
	}
	return nil
}

func publishOverrides(ctx context.Context, pool *pgxpool.Pool, publisher Publisher, counts *Counts) error {
	rows, err := pool.Query(ctx, `
		SELECT c.id, c.server_id, c.created_at, o.role_id,
		       COALESCE(o.allow, 0), COALESCE(o.deny, 0), o.updated_at
		  FROM channels c
		  LEFT JOIN channel_permission_overrides o ON o.channel_id = c.id
		 WHERE c.kind = 'voice'
		 ORDER BY c.id, o.role_id`)
	if err != nil {
		return fmt.Errorf("backfill channel overrides: %w", err)
	}
	defer rows.Close()

	type snapshot struct {
		serverID   uuid.UUID
		occurredAt time.Time
		overrides  []permissions.RoleOverride
	}
	snapshots := make(map[uuid.UUID]*snapshot)
	for rows.Next() {
		var channelID uuid.UUID
		var serverID uuid.UUID
		var createdAt time.Time
		var roleID *uuid.UUID
		var allow, deny role.Permission
		var updatedAt *time.Time
		if err := rows.Scan(&channelID, &serverID, &createdAt, &roleID, &allow, &deny, &updatedAt); err != nil {
			return fmt.Errorf("scan channel override: %w", err)
		}
		current := snapshots[channelID]
		if current == nil {
			current = &snapshot{serverID: serverID, occurredAt: createdAt, overrides: []permissions.RoleOverride{}}
			snapshots[channelID] = current
		}
		if roleID != nil {
			current.overrides = append(current.overrides, permissions.RoleOverride{
				RoleID: roleID.String(),
				Allow:  role.PermissionNames(allow),
				Deny:   role.PermissionNames(deny),
			})
			if updatedAt != nil && updatedAt.After(current.occurredAt) {
				current.occurredAt = *updatedAt
			}
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read channel overrides: %w", err)
	}

	channelIDs := make([]uuid.UUID, 0, len(snapshots))
	for channelID := range snapshots {
		channelIDs = append(channelIDs, channelID)
	}
	sort.Slice(channelIDs, func(i, j int) bool {
		return channelIDs[i].String() < channelIDs[j].String()
	})
	for _, channelID := range channelIDs {
		current := snapshots[channelID]
		if err := publisher.Publish(ctx, permissions.RoutingKeyChannelOverridesUpdated, permissions.ChannelOverridesUpdated{
			Meta:      meta(current.occurredAt),
			ServerID:  current.serverID.String(),
			ChannelID: channelID.String(),
			Overrides: current.overrides,
		}); err != nil {
			return err
		}
		counts.Overrides++
	}
	return nil
}

func publishMutes(ctx context.Context, pool *pgxpool.Pool, publisher Publisher, counts *Counts) error {
	rows, err := pool.Query(ctx, `
		SELECT server_id, user_id, muted_by, muted_at, until_at, reason
		  FROM server_mutes
		 WHERE until_at > now()
		 ORDER BY server_id, user_id`)
	if err != nil {
		return fmt.Errorf("backfill mutes: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var serverID, userID, mutedBy, reason string
		var mutedAt, untilAt time.Time
		if err := rows.Scan(&serverID, &userID, &mutedBy, &mutedAt, &untilAt, &reason); err != nil {
			return fmt.Errorf("scan mute: %w", err)
		}
		if err := publisher.Publish(ctx, moderation.RoutingKeyMemberMuted, moderation.MemberMuted{
			Meta:     meta(mutedAt),
			ServerID: serverID,
			UserID:   userID,
			MutedBy:  mutedBy,
			UntilAt:  untilAt,
			Reason:   reason,
		}); err != nil {
			return err
		}
		counts.Mutes++
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read mutes: %w", err)
	}
	return nil
}

func meta(occurredAt time.Time) envelope.Meta {
	return envelope.Meta{EventID: uuid.NewString(), OccurredAt: occurredAt.UTC()}
}
