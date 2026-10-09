package moderation_service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model/moderation"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/outbox"
	"github.com/discordia-grupo01/servers/internal/service"
)

type Servers = service.ServerGetter

type Standings = service.StandingReader

// Repository is the slice of moderation_repo.Repository this service needs.
type Repository interface {
	BanMember(ctx context.Context, ban moderation.Ban, check func(actor, target role.Standing) error, events ...outbox.Event) error
	KickMember(ctx context.Context, kick moderation.Kick, check func(actor, target role.Standing) error, events ...outbox.Event) error
	UnbanMember(ctx context.Context, serverID uuid.UUID, userID string, events ...outbox.Event) error
	ListBans(ctx context.Context, serverID uuid.UUID, limit, offset int) ([]moderation.Ban, int, error)
}

type MuteRepository interface {
	MuteMember(ctx context.Context, mute moderation.Mute, check func(actor, target role.Standing) error, events ...outbox.Event) error
	UnmuteMember(ctx context.Context, serverID uuid.UUID, userID string, events ...outbox.Event) error
	IsMuted(ctx context.Context, serverID uuid.UUID, userID string, at time.Time) (bool, error)
	ExpireMutes(ctx context.Context, at time.Time) (int, error)
}
