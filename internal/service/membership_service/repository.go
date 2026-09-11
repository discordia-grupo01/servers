package membership_service

import (
	"context"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/service"
)

type Servers = service.ServerGetter

type Members interface {
	RemoveMember(ctx context.Context, serverID uuid.UUID, userID string) (bool, error)
}
