package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/discordia-grupo01/events-contract/envelope"
	"github.com/discordia-grupo01/events-contract/ownership"
)

type TransferSnapshot struct {
	ID         uuid.UUID
	ServerID   uuid.UUID
	FromUserID string
	ToUserID   string
	CreatedAt  time.Time
	ResolvedAt *time.Time
}

func PublishOwnershipTransfer(ctx context.Context, tx pgx.Tx, t TransferSnapshot, status string) error {
	occurredAt := t.CreatedAt
	var resolvedAt *time.Time
	if t.ResolvedAt != nil {
		occurredAt = *t.ResolvedAt
		utc := t.ResolvedAt.UTC()
		resolvedAt = &utc
	}
	return insertAccessEvent(ctx, tx, ownership.RoutingKeyTransferUpdated, ownership.TransferUpdated{
		Meta:       envelope.Meta{EventID: uuid.NewString(), OccurredAt: occurredAt.UTC()},
		TransferID: t.ID.String(),
		ServerID:   t.ServerID.String(),
		FromUserID: t.FromUserID,
		ToUserID:   t.ToUserID,
		Status:     status,
		CreatedAt:  t.CreatedAt.UTC(),
		ResolvedAt: resolvedAt,
	})
}
