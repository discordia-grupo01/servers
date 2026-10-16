package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/discordia-grupo01/events-contract/servers"
)

// PublishServerUpdated writes the full snapshot of a server (name, owner and
// creation date) to the outbox, inside tx. It reads the row from tx, so it
// must run after the INSERT/UPDATE that changed it. Admin keeps the snapshot
// with the newest occurred_at, which accessMeta stamps with the current time.
func PublishServerUpdated(ctx context.Context, tx pgx.Tx, serverID uuid.UUID) error {
	var name, ownerID string
	var createdAt time.Time
	err := tx.QueryRow(ctx,
		`SELECT name, owner_id, created_at FROM servers WHERE id = $1`, serverID,
	).Scan(&name, &ownerID, &createdAt)
	if err != nil {
		return fmt.Errorf("read server for event: %w", err)
	}
	return insertAccessEvent(ctx, tx, servers.RoutingKeyServerUpdated, servers.ServerUpdated{
		Meta:      accessMeta(),
		ServerID:  serverID.String(),
		Name:      name,
		OwnerID:   ownerID,
		CreatedAt: createdAt.UTC(),
	})
}
