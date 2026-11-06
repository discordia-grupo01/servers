package moderation_handler

import (
	"context"

	"github.com/discordia-grupo01/servers/internal/model/moderation"
	"github.com/discordia-grupo01/servers/internal/service/moderation_service"
)

type UseCases interface {
	BanMember(ctx context.Context, p moderation_service.BanParams) (moderation.Ban, error)
	KickMember(ctx context.Context, p moderation_service.KickParams) error
	UnbanMember(ctx context.Context, p moderation_service.UnbanParams) error
	MuteMember(ctx context.Context, p moderation_service.MuteParams) error
	UnmuteMember(ctx context.Context, p moderation_service.UnmuteParams) error
	ListBans(ctx context.Context, p moderation_service.ListBansParams) (moderation.BanPage, error)
	ListAudit(ctx context.Context, p moderation_service.ListAuditParams) (moderation.AuditPage, error)
}
