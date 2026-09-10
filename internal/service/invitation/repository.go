package invitation

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
)

type Repository interface {
	ServerExists(ctx context.Context, serverID uuid.UUID) (bool, error)
	IsMember(ctx context.Context, serverID uuid.UUID, userID string) (bool, error)
	IsBanned(ctx context.Context, serverID uuid.UUID, userID string) (bool, error)
	CreateInvitation(ctx context.Context, inv model.Invitation) error
	InvitationByCode(ctx context.Context, code string) (model.Invitation, error)
	RevokeInvitation(ctx context.Context, code string, revokedAt time.Time) error
	RedeemInvitation(ctx context.Context, code, userID string, now time.Time) (uuid.UUID, error)
}
