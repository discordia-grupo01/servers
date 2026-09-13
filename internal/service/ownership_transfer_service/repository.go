package ownership_transfer_service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/service"
)

type Servers = service.ServerGetter

type Members = service.MemberChecker

type Transfers interface {
	Create(ctx context.Context, t model.OwnershipTransfer) error
	GetByID(ctx context.Context, id uuid.UUID) (model.OwnershipTransfer, error)
	GetPendingByServer(ctx context.Context, serverID uuid.UUID) (model.OwnershipTransfer, error)
	Accept(ctx context.Context, id uuid.UUID, resolvedAt time.Time) (model.OwnershipTransfer, error)
	Reject(ctx context.Context, id uuid.UUID, resolvedAt time.Time) (model.OwnershipTransfer, error)
	Cancel(ctx context.Context, id uuid.UUID, resolvedAt time.Time) (model.OwnershipTransfer, error)
}
