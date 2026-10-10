package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/discordia-grupo01/events-contract/envelope"
	"github.com/discordia-grupo01/events-contract/permissions"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/outbox"
)

func accessMeta() envelope.Meta {
	return envelope.Meta{EventID: uuid.NewString(), OccurredAt: time.Now().UTC()}
}

func PublishRoleUpdated(ctx context.Context, tx pgx.Tx, roleID uuid.UUID) error {
	var serverID uuid.UUID
	var mask role.Permission
	var isEveryone bool
	err := tx.QueryRow(ctx,
		`SELECT server_id, permissions, is_everyone FROM roles WHERE id = $1`, roleID,
	).Scan(&serverID, &mask, &isEveryone)
	if err != nil {
		return fmt.Errorf("read role for event: %w", err)
	}
	return insertAccessEvent(ctx, tx, permissions.RoutingKeyRoleUpdated, permissions.RoleUpdated{
		Meta:        accessMeta(),
		ServerID:    serverID.String(),
		RoleID:      roleID.String(),
		Permissions: role.PermissionNames(mask),
		IsEveryone:  isEveryone,
	})
}

func PublishRoleDeleted(ctx context.Context, tx pgx.Tx, serverID, roleID uuid.UUID) error {
	return insertAccessEvent(ctx, tx, permissions.RoutingKeyRoleDeleted, permissions.RoleDeleted{
		Meta:     accessMeta(),
		ServerID: serverID.String(),
		RoleID:   roleID.String(),
	})
}

func PublishMemberRoles(ctx context.Context, tx pgx.Tx, serverID uuid.UUID, userID string) error {
	rows, err := tx.Query(ctx, `
		SELECT role_id::text FROM member_roles
		 WHERE server_id = $1 AND user_id = $2
		 ORDER BY role_id`, serverID, userID)
	if err != nil {
		return fmt.Errorf("read member roles for event: %w", err)
	}
	roleIDs, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return fmt.Errorf("read member roles for event: %w", err)
	}
	return insertAccessEvent(ctx, tx, permissions.RoutingKeyMemberRolesUpdated, permissions.MemberRolesUpdated{
		Meta:     accessMeta(),
		ServerID: serverID.String(),
		UserID:   userID,
		RoleIDs:  roleIDs,
	})
}

func PublishChannelOverrides(ctx context.Context, tx pgx.Tx, serverID, channelID uuid.UUID) error {
	rows, err := tx.Query(ctx, `
		SELECT role_id, allow, deny FROM channel_permission_overrides
		 WHERE channel_id = $1
		 ORDER BY role_id`, channelID)
	if err != nil {
		return fmt.Errorf("read channel overrides for event: %w", err)
	}
	defer rows.Close()

	overrides := make([]permissions.RoleOverride, 0)
	for rows.Next() {
		var roleID uuid.UUID
		var allow, deny role.Permission
		if err := rows.Scan(&roleID, &allow, &deny); err != nil {
			return fmt.Errorf("scan channel override for event: %w", err)
		}
		overrides = append(overrides, permissions.RoleOverride{
			RoleID: roleID.String(),
			Allow:  role.PermissionNames(allow),
			Deny:   role.PermissionNames(deny),
		})
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read channel overrides for event: %w", err)
	}
	rows.Close()

	return insertAccessEvent(ctx, tx, permissions.RoutingKeyChannelOverridesUpdated, permissions.ChannelOverridesUpdated{
		Meta:      accessMeta(),
		ServerID:  serverID.String(),
		ChannelID: channelID.String(),
		Overrides: overrides,
	})
}

func PublishOwner(ctx context.Context, tx pgx.Tx, serverID uuid.UUID, ownerID string) error {
	return insertAccessEvent(ctx, tx, permissions.RoutingKeyOwnerUpdated, permissions.OwnerUpdated{
		Meta:     accessMeta(),
		ServerID: serverID.String(),
		OwnerID:  ownerID,
	})
}

func insertAccessEvent(ctx context.Context, tx pgx.Tx, routingKey string, payload any) error {
	evt, err := outbox.New(routingKey, payload)
	if err != nil {
		return err
	}
	return outbox.Insert(ctx, tx, evt)
}
