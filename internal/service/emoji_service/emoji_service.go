package emoji_service

import (
	"context"
	"log"
	"time"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/events-contract/envelope"
	"github.com/discordia-grupo01/events-contract/servers"
	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/emoji"
	"github.com/discordia-grupo01/servers/internal/outbox"
	"github.com/discordia-grupo01/servers/internal/service"
)

type Repository interface {
	Create(ctx context.Context, e emoji.Emoji, maxEmojis int, evt outbox.Event) error
	List(ctx context.Context, serverID uuid.UUID) ([]emoji.Emoji, error)
	Delete(ctx context.Context, serverID, emojiID uuid.UUID, by string, at time.Time, buildEvent func(emoji.Emoji) (outbox.Event, error)) error
}

type ImageStore interface {
	SaveEmoji(serverID, key string, data []byte, contentType string) (url string, err error)
	DeleteEmoji(serverID, key string) error
}

type Service struct {
	servers   service.ServerGetter
	standings service.StandingReader
	repo      Repository
	images    ImageStore
	now       func() time.Time
}

func New(servers service.ServerGetter, standings service.StandingReader, repo Repository, images ImageStore) *Service {
	return &Service{
		servers:   servers,
		standings: standings,
		repo:      repo,
		images:    images,
		now:       func() time.Time { return time.Now().UTC() },
	}
}

type CreateParams struct {
	ServerID    uuid.UUID
	ActorUserID string
	Name        string
	Image       []byte
}

type ListParams struct {
	ServerID    uuid.UUID
	ActorUserID string
}

type DeleteParams struct {
	ServerID    uuid.UUID
	EmojiID     uuid.UUID
	ActorUserID string
}

func (s *Service) Create(ctx context.Context, p CreateParams) (emoji.Emoji, error) {
	srv, err := s.authorizeManager(ctx, p.ServerID, p.ActorUserID)
	if err != nil {
		return emoji.Emoji{}, err
	}
	if err := emoji.ValidateName(p.Name); err != nil {
		return emoji.Emoji{}, err
	}
	image, err := emoji.InspectImage(p.Image)
	if err != nil {
		return emoji.Emoji{}, err
	}

	created := emoji.Emoji{
		ID:        uuid.New(),
		ServerID:  p.ServerID,
		Name:      p.Name,
		Animated:  image.Animated,
		CreatedBy: p.ActorUserID,
		CreatedAt: s.now(),
	}
	key := created.ID.String() + image.Extension
	created.ImageURL, err = s.images.SaveEmoji(p.ServerID.String(), key, p.Image, image.ContentType)
	if err != nil {
		return emoji.Emoji{}, err
	}

	evt, err := outbox.New(servers.RoutingKeyEmojiCreated, servers.EmojiCreated{
		Meta:     envelope.Meta{EventID: uuid.NewString(), OccurredAt: created.CreatedAt},
		EmojiID:  created.ID.String(),
		ServerID: created.ServerID.String(),
		Name:     created.Name,
		Animated: created.Animated,
		ImageURL: created.ImageURL,
	})
	if err == nil {
		err = s.repo.Create(ctx, created, emoji.Slots(srv.BoostLevel), evt)
	}
	if err != nil {
		if cleanupErr := s.images.DeleteEmoji(p.ServerID.String(), key); cleanupErr != nil {
			log.Printf("emoji_service: orphaned image %s: %v", key, cleanupErr)
		}
		return emoji.Emoji{}, err
	}
	return created, nil
}

func (s *Service) List(ctx context.Context, p ListParams) (emoji.Page, error) {
	if p.ActorUserID == "" {
		return emoji.Page{}, model.ErrUnauthorized
	}
	srv, err := s.servers.GetByID(ctx, p.ServerID)
	if err != nil {
		return emoji.Page{}, err
	}
	actor, err := service.ResolveStanding(ctx, s.standings, srv, p.ActorUserID)
	if err != nil {
		return emoji.Page{}, err
	}
	if !actor.IsMember {
		return emoji.Page{}, model.ErrForbiddenNotMember
	}
	emojis, err := s.repo.List(ctx, p.ServerID)
	if err != nil {
		return emoji.Page{}, err
	}
	return emoji.Page{Emojis: emojis, MaxEmojis: emoji.Slots(srv.BoostLevel)}, nil
}

func (s *Service) Delete(ctx context.Context, p DeleteParams) error {
	if _, err := s.authorizeManager(ctx, p.ServerID, p.ActorUserID); err != nil {
		return err
	}
	at := s.now()
	return s.repo.Delete(ctx, p.ServerID, p.EmojiID, p.ActorUserID, at, func(deleted emoji.Emoji) (outbox.Event, error) {
		return outbox.New(servers.RoutingKeyEmojiDeleted, servers.EmojiDeleted{
			Meta:     envelope.Meta{EventID: uuid.NewString(), OccurredAt: at},
			EmojiID:  deleted.ID.String(),
			ServerID: deleted.ServerID.String(),
			Name:     deleted.Name,
			Animated: deleted.Animated,
			ImageURL: deleted.ImageURL,
		})
	})
}

func (s *Service) authorizeManager(ctx context.Context, serverID uuid.UUID, actorID string) (model.Server, error) {
	if actorID == "" {
		return model.Server{}, model.ErrUnauthorized
	}
	srv, err := s.servers.GetByID(ctx, serverID)
	if err != nil {
		return model.Server{}, err
	}
	actor, err := service.ResolveStanding(ctx, s.standings, srv, actorID)
	if err != nil {
		return model.Server{}, err
	}
	if err := service.RequireManageServer(actor); err != nil {
		return model.Server{}, err
	}
	return srv, nil
}
