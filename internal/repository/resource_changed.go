package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/discordia-grupo01/events-contract/envelope"
	"github.com/discordia-grupo01/events-contract/servers"
)

func PublishResourceChanged(ctx context.Context, tx pgx.Tx, serverID uuid.UUID, resource string) error {
	return insertAccessEvent(ctx, tx, servers.RoutingKeyResourceChanged, servers.ResourceChanged{
		Meta:     envelope.Meta{EventID: uuid.NewString(), OccurredAt: time.Now().UTC()},
		ServerID: serverID.String(),
		Resource: resource,
	})
}

type ChangeRecorder struct {
	pool *pgxpool.Pool
}

func NewChangeRecorder(pool *pgxpool.Pool) *ChangeRecorder {
	return &ChangeRecorder{pool: pool}
}

func (r *ChangeRecorder) RecordResourceChanged(ctx context.Context, serverID uuid.UUID, resource string) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		return PublishResourceChanged(ctx, tx, serverID, resource)
	})
}

func (r *ChangeRecorder) ServerOfChannel(ctx context.Context, id uuid.UUID) (uuid.UUID, error) {
	var serverID uuid.UUID
	err := r.pool.QueryRow(ctx, `SELECT server_id FROM channels WHERE id = $1`, id).Scan(&serverID)
	return serverID, err
}

func (r *ChangeRecorder) ServerOfCategory(ctx context.Context, id uuid.UUID) (uuid.UUID, error) {
	var serverID uuid.UUID
	err := r.pool.QueryRow(ctx, `SELECT server_id FROM categories WHERE id = $1`, id).Scan(&serverID)
	return serverID, err
}
