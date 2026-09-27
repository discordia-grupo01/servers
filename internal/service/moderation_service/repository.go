package moderation_service

import (
	"context"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model/moderation"
	"github.com/discordia-grupo01/servers/internal/outbox"
	"github.com/discordia-grupo01/servers/internal/service"
)

type Servers = service.ServerGetter

type Standings = service.StandingReader

// Repository is the slice of moderation_repo.Repository this service needs.
type Repository interface {
	BanMember(ctx context.Context, ban moderation.Ban, events ...outbox.Event) error
	ListBans(ctx context.Context, serverID uuid.UUID, limit, offset int) ([]moderation.Ban, int, error)
}
