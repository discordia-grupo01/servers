package ownership_transfer_handler

import (
	"context"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/service/ownership_transfer_service"
)

type UseCases interface {
	InitiateTransfer(ctx context.Context, p ownership_transfer_service.InitiateParams) (model.OwnershipTransfer, error)
	AcceptTransfer(ctx context.Context, p ownership_transfer_service.RespondParams) (model.OwnershipTransfer, error)
	RejectTransfer(ctx context.Context, p ownership_transfer_service.RespondParams) (model.OwnershipTransfer, error)
}
