package mention_service

import (
	"context"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/service"
)

type Servers = service.ServerGetter

type Channels interface {
	GetByID(ctx context.Context, id uuid.UUID) (model.Channel, error)
}

// Members is the slice of member_repo.MemberRepository this service needs.
type Members interface {
	service.StandingReader
	FilterMembers(ctx context.Context, serverID uuid.UUID, userIDs []string) ([]string, error)
	MemberIDsWithRoles(ctx context.Context, serverID uuid.UUID, roleIDs []uuid.UUID) ([]string, error)
	ViewersAmong(ctx context.Context, serverID uuid.UUID, ownerID string, userIDs []string) ([]string, error)
	AllViewers(ctx context.Context, serverID uuid.UUID, ownerID string) ([]string, error)
}

// Roles is the slice of role_repo.RoleRepository this service needs.
type Roles interface {
	FilterMentionable(ctx context.Context, serverID uuid.UUID, roleIDs []uuid.UUID) ([]uuid.UUID, error)
}
