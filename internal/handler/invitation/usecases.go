package invitation

import (
	"context"

	"github.com/discordia-grupo01/servers/internal/model"
	invitationsvc "github.com/discordia-grupo01/servers/internal/service/invitation"
)

type UseCases interface {
	GenerateInvitation(ctx context.Context, p invitationsvc.GenerateParams) (model.Invitation, error)
	RevokeInvitation(ctx context.Context, p invitationsvc.RevokeParams) error
	JoinServer(ctx context.Context, p invitationsvc.JoinParams) (invitationsvc.JoinResult, error)
}
