// Package word_filter_service manages a server's banned words list: members
// with MANAGE_MESSAGES read and replace it, and every change is published so
// messaging blocks the messages that contain one of them.
package word_filter_service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/events-contract/envelope"
	moderationevents "github.com/discordia-grupo01/events-contract/moderation"
	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/moderation"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/outbox"
	"github.com/discordia-grupo01/servers/internal/service"
)

type Repository interface {
	List(ctx context.Context, serverID uuid.UUID) ([]string, error)
	Replace(ctx context.Context, serverID uuid.UUID, words []string, events ...outbox.Event) error
}

type Service struct {
	servers   service.ServerGetter
	standings service.StandingReader
	repo      Repository
	now       func() time.Time
}

func New(servers service.ServerGetter, standings service.StandingReader, repo Repository) *Service {
	return &Service{
		servers:   servers,
		standings: standings,
		repo:      repo,
		now:       func() time.Time { return time.Now().UTC() },
	}
}

type ListParams struct {
	ServerID    uuid.UUID
	ActorUserID string
}

type ReplaceParams struct {
	ServerID    uuid.UUID
	ActorUserID string
	Words       []string
}

// List returns the banned words. Only who can edit them can read them.
func (s *Service) List(ctx context.Context, p ListParams) ([]string, error) {
	if err := s.authorize(ctx, p.ServerID, p.ActorUserID); err != nil {
		return nil, err
	}
	return s.repo.List(ctx, p.ServerID)
}

// Replace saves the whole list (CA2) and publishes it. It applies to the
// messages sent from then on; the ones already sent are left as they are.
func (s *Service) Replace(ctx context.Context, p ReplaceParams) ([]string, error) {
	words, err := moderation.NormalizeBannedWords(p.Words)
	if err != nil {
		return nil, err
	}
	if err := s.authorize(ctx, p.ServerID, p.ActorUserID); err != nil {
		return nil, err
	}
	event, err := outbox.New(moderationevents.RoutingKeyWordFilterUpdated, moderationevents.WordFilterUpdated{
		Meta:     envelope.Meta{EventID: uuid.NewString(), OccurredAt: s.now()},
		ServerID: p.ServerID.String(),
		Words:    words,
	})
	if err != nil {
		return nil, err
	}
	if err := s.repo.Replace(ctx, p.ServerID, words, event); err != nil {
		return nil, err
	}
	return words, nil
}

func (s *Service) authorize(ctx context.Context, serverID uuid.UUID, actorID string) error {
	if actorID == "" {
		return model.ErrUnauthorized
	}
	srv, err := s.servers.GetByID(ctx, serverID)
	if err != nil {
		return err
	}
	actor, err := service.ResolveStanding(ctx, s.standings, srv, actorID)
	if err != nil {
		return err
	}
	return service.RequirePermission(actor, role.PermManageMessages)
}
