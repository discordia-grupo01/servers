package channelperm_handler

import (
	"context"

	"github.com/discordia-grupo01/servers/internal/model/channelperm"
	"github.com/discordia-grupo01/servers/internal/service/channelperm_service"
)

type UseCases interface {
	SetOverride(ctx context.Context, p channelperm_service.SetOverrideParams) (channelperm.Override, error)
	DeleteOverride(ctx context.Context, p channelperm_service.DeleteOverrideParams) error
	ListOverrides(ctx context.Context, p channelperm_service.ListOverridesParams) ([]channelperm.Override, error)
	EffectivePermissions(ctx context.Context, p channelperm_service.EffectivePermissionsParams) ([]string, error)
}
