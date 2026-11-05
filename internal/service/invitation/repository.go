package invitation

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/outbox"
)

type Repository interface {
	IsMember(ctx context.Context, serverID uuid.UUID, userID string) (bool, error)
	IsBanned(ctx context.Context, serverID uuid.UUID, userID string) (bool, error)
	CreateInvitation(ctx context.Context, inv model.Invitation) error
	InvitationByCode(ctx context.Context, code string) (model.Invitation, error)
	ListByServer(ctx context.Context, serverID uuid.UUID) ([]model.Invitation, error)
	RevokeInvitation(ctx context.Context, code string, revokedAt time.Time) error
	// buildEvent is called with the server_id once RedeemInvitation resolves
	// it (only known once the invitation row is matched, inside the
	// transaction) -- see invitation_repo.go's doc comment on RedeemInvitation.
	RedeemInvitation(ctx context.Context, code, userID string, now time.Time, buildEvent func(serverID uuid.UUID) (outbox.Event, error)) (uuid.UUID, error)
	// ServerByVanity only finds servers whose boost level still unlocks the code.
	ServerByVanity(ctx context.Context, code string) (uuid.UUID, error)
	JoinByVanity(ctx context.Context, serverID uuid.UUID, userID string, now time.Time, buildEvent func(serverID uuid.UUID) (outbox.Event, error)) error
}
