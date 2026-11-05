package boost_service

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/service"
)

type VanityRepository interface {
	SetVanity(ctx context.Context, serverID uuid.UUID, code *string) error
}

type Service struct {
	servers   service.ServerGetter
	standings service.StandingReader
	vanity    VanityRepository
}

func New(servers service.ServerGetter, standings service.StandingReader, vanity VanityRepository) *Service {
	return &Service{servers: servers, standings: standings, vanity: vanity}
}

// Summary is open to every member, not just to whoever manages the server: any
// of them can boost it.
func (s *Service) Summary(ctx context.Context, serverID uuid.UUID, actorID string) (model.BoostSummary, error) {
	if actorID == "" {
		return model.BoostSummary{}, model.ErrUnauthorized
	}
	srv, err := s.servers.GetByID(ctx, serverID)
	if err != nil {
		return model.BoostSummary{}, err
	}
	actor, err := service.ResolveStanding(ctx, s.standings, srv, actorID)
	if err != nil {
		return model.BoostSummary{}, err
	}
	if !actor.IsMember {
		return model.BoostSummary{}, model.ErrForbiddenNotMember
	}
	return model.SummarizeBoosts(srv.BoostCount), nil
}

// SetVanity sets the server's vanity code; nil clears it. Setting one needs boost
// level 3, clearing doesn't: a server below it can still free the code it stored.
func (s *Service) SetVanity(ctx context.Context, serverID uuid.UUID, actorID string, code *string) (*string, error) {
	if actorID == "" {
		return nil, model.ErrUnauthorized
	}
	srv, err := s.servers.GetByID(ctx, serverID)
	if err != nil {
		return nil, err
	}
	actor, err := service.ResolveStanding(ctx, s.standings, srv, actorID)
	if err != nil {
		return nil, err
	}
	if err := service.RequireManageServer(actor); err != nil {
		return nil, err
	}
	if code == nil {
		return nil, s.vanity.SetVanity(ctx, serverID, nil)
	}
	normalized := strings.ToLower(strings.TrimSpace(*code))
	if err := model.ValidateVanityCode(normalized); err != nil {
		return nil, err
	}
	if srv.BoostLevel < model.BoostLevelVanity {
		return nil, &model.BoostLevelRequiredError{Required: model.BoostLevelVanity}
	}
	if err := s.vanity.SetVanity(ctx, serverID, &normalized); err != nil {
		return nil, err
	}
	return &normalized, nil
}
