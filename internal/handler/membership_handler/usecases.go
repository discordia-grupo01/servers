package membership_handler

import (
	"context"

	"github.com/discordia-grupo01/servers/internal/service/membership_service"
)

type UseCases interface {
	LeaveServer(ctx context.Context, p membership_service.LeaveParams) error
}
