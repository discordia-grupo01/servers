package moderation_handler

import (
	"context"

	"github.com/discordia-grupo01/servers/internal/model/moderation"
	"github.com/discordia-grupo01/servers/internal/service/moderation_service"
)

type UseCases interface {
	BanMember(ctx context.Context, p moderation_service.BanParams) (moderation.Ban, error)
	ListBans(ctx context.Context, p moderation_service.ListBansParams) (moderation.BanPage, error)
}
