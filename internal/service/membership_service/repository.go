package membership_service

import (
	"context"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/outbox"
	"github.com/discordia-grupo01/servers/internal/service"
)

type Servers = service.ServerGetter

type Members interface {
	service.MemberChecker
	RemoveMember(ctx context.Context, serverID uuid.UUID, userID string, memberLeftEvent outbox.Event) (bool, error)
	ListMembers(ctx context.Context, serverID uuid.UUID, limit, offset int) ([]model.Member, int, error)
}
