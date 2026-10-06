package mention_handler

import (
	"context"

	"github.com/discordia-grupo01/servers/internal/model/mention"
	"github.com/discordia-grupo01/servers/internal/service/mention_service"
)

type UseCases interface {
	Resolve(ctx context.Context, p mention_service.ResolveParams) (mention.Resolution, error)
}
