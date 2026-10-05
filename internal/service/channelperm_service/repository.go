package channelperm_service

import (
	"context"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/channelperm"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/service"
)

type Servers = service.ServerGetter

// Channels is the slice of ChannelRepository this service needs.
type Channels interface {
	GetByID(ctx context.Context, id uuid.UUID) (model.Channel, error)
}

// Roles is the slice of RoleRepository this service needs.
type Roles interface {
	GetByID(ctx context.Context, id uuid.UUID) (role.Role, error)
}

// Members is the slice of MemberRepository this service needs.
type Members interface {
	service.StandingReader
	service.MemberChecker
}

// Repository is the slice of channelperm_repo.Repository this service needs.
type Repository interface {
	Upsert(ctx context.Context, serverID uuid.UUID, actorID string, o channelperm.Override, check func(actor role.Standing, rl role.Role) error) (channelperm.Override, error)
	Delete(ctx context.Context, serverID uuid.UUID, actorID string, channelID, roleID uuid.UUID, check func(actor role.Standing, rl role.Role) error) (bool, error)
	ListByChannel(ctx context.Context, channelID uuid.UUID) ([]channelperm.Override, error)
	ForMember(ctx context.Context, serverID, channelID uuid.UUID, userID string) (channelperm.Override, []channelperm.Override, error)
}
