// Package service contiene la logica de negocio del servicio de servidores.
package service

import (
	"context"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/events"
	"github.com/discordia-grupo01/servers/internal/iconstore"
	"github.com/discordia-grupo01/servers/internal/model"
)

type Repo interface {
	CreateServer(ctx context.Context, srv model.Server, categories []model.Category, channels []model.Channel) error
	ListForUser(ctx context.Context, userID string) ([]model.Server, error)
	GetByID(ctx context.Context, id uuid.UUID) (model.Server, error)
	UpdateServer(ctx context.Context, id uuid.UUID, name, nameKey string, iconURL, bannerURL *string) error
	IconURLs(ctx context.Context, id uuid.UUID) (iconURL, bannerURL *string, err error)
}

type IconStore interface {
	Save(id string, data []byte) (url string, err error)
	Delete(id string) error
	GenerateDefault() (data []byte, err error)
}

type Service struct {
	repo   Repo
	icons  IconStore
	events events.Publisher
}

func New(repo Repo, icons IconStore, publisher events.Publisher) *Service {
	return &Service{repo: repo, icons: icons, events: publisher}
}

func validateServerName(raw string) (string, error) {
	name := normalizeName(raw)
	if name == "" {
		return "", model.ErrNameRequired
	}
	if strings.ContainsFunc(name, unicode.IsControl) {
		return "", model.ErrNameInvalidChars
	}

	switch n := utf8.RuneCountInString(name); {
	case n < model.ServerNameMinLen:
		return "", model.ErrNameTooShort
	case n > model.ServerNameMaxLen:
		return "", model.ErrNameTooLong
	}
	return name, nil
}

func BannerObjectID(id uuid.UUID) string { return id.String() + "-banner" }

func (s *Service) CreateServer(ctx context.Context, p CreateServerParams) (model.Server, error) {
	if p.OwnerID == "" {
		return model.Server{}, model.ErrUnauthorized
	}

	name, err := validateServerName(p.Name)
	if err != nil {
		return model.Server{}, err
	}

	id := uuid.New()
	now := time.Now().UTC()

	srv := model.Server{
		ID:        id,
		Name:      name,
		NameKey:   nameKey(name),
		OwnerID:   p.OwnerID,
		CreatedAt: now,
	}

	textKind, voiceKind := model.ChannelText, model.ChannelVoice
	textCategoryID, voiceCategoryID := uuid.New(), uuid.New()
	categories := []model.Category{
		{
			ID: textCategoryID, ServerID: id, Name: model.DefaultTextCategoryName,
			Position: 0, DefaultKind: &textKind, CreatedAt: now,
		},
		{
			ID: voiceCategoryID, ServerID: id, Name: model.DefaultVoiceCategoryName,
			Position: 1, DefaultKind: &voiceKind, CreatedAt: now,
		},
	}

	channels := []model.Channel{
		{
			ID: uuid.New(), ServerID: id, Name: model.DefaultTextChannelName,
			Kind: model.ChannelText, Position: 0, CategoryID: &textCategoryID, CreatedAt: now,
		},
		{
			ID: uuid.New(), ServerID: id, Name: model.DefaultVoiceChannelName,
			Kind: model.ChannelVoice, Position: 0, CategoryID: &voiceCategoryID, CreatedAt: now,
		},
	}

	iconData := p.Icon
	if len(iconData) == 0 {
		generated, err := s.icons.GenerateDefault()
		if err != nil {
			return model.Server{}, err
		}
		iconData = generated
	}

	url, err := s.icons.Save(id.String(), iconData)
	if err != nil {
		return model.Server{}, err
	}
	srv.IconURL = &url

	if err := s.repo.CreateServer(ctx, srv, categories, channels); err != nil {
		if hash, ok := iconstore.HashFromURL(url); ok {
			_ = s.icons.Delete(iconstore.StorageKey(id.String(), hash))
		}
		return model.Server{}, err
	}

	s.publishMemberJoined(ctx, srv.ID, srv.OwnerID, srv.CreatedAt)

	srv.Categories = categories
	srv.Channels = channels
	return srv, nil
}

func (s *Service) publishMemberJoined(ctx context.Context, serverID uuid.UUID, ownerID string, occurredAt time.Time) {
	_ = s.events.Publish(ctx, events.RoutingKeyMemberJoined, events.MembershipEvent{
		EventID:    uuid.NewString(),
		ServerID:   serverID.String(),
		UserID:     ownerID,
		OccurredAt: occurredAt,
	})
}

func (s *Service) ListMyServers(ctx context.Context, userID string) ([]model.Server, error) {
	if userID == "" {
		return nil, model.ErrUnauthorized
	}
	return s.repo.ListForUser(ctx, userID)
}

func (s *Service) GetServer(ctx context.Context, id uuid.UUID) (model.Server, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *Service) GetServerImageURLs(ctx context.Context, id uuid.UUID) (iconURL, bannerURL *string, err error) {
	return s.repo.IconURLs(ctx, id)
}

type UpdateServerParams struct {
	ServerID     uuid.UUID
	ActorUserID  string
	Name         *string
	Icon         []byte
	Banner       []byte
	RemoveBanner bool
}

func (s *Service) UpdateServer(ctx context.Context, p UpdateServerParams) (model.Server, error) {
	if p.ActorUserID == "" {
		return model.Server{}, model.ErrUnauthorized
	}

	srv, err := s.repo.GetByID(ctx, p.ServerID)
	if err != nil {
		return model.Server{}, err
	}
	if err := RequireManageServer(srv, p.ActorUserID); err != nil {
		return model.Server{}, err
	}

	name, key := srv.Name, srv.NameKey
	if p.Name != nil {
		name, err = validateServerName(*p.Name)
		if err != nil {
			return model.Server{}, err
		}
		key = nameKey(name)
	}

	oldIconURL, oldBannerURL := srv.IconURL, srv.BannerURL

	iconURL := srv.IconURL
	iconChanged := len(p.Icon) > 0
	if iconChanged {
		url, err := s.icons.Save(p.ServerID.String(), p.Icon)
		if err != nil {
			return model.Server{}, err
		}
		iconURL = &url
	}

	bannerURL := srv.BannerURL
	bannerChanged := len(p.Banner) > 0 || p.RemoveBanner
	switch {
	case len(p.Banner) > 0:
		url, err := s.icons.Save(BannerObjectID(p.ServerID), p.Banner)
		if err != nil {
			return model.Server{}, err
		}
		bannerURL = &url
	case p.RemoveBanner:
		bannerURL = nil
	}

	if err := s.repo.UpdateServer(ctx, p.ServerID, name, key, iconURL, bannerURL); err != nil {
		return model.Server{}, err
	}

	if iconChanged && oldIconURL != nil {
		if hash, ok := iconstore.HashFromURL(*oldIconURL); ok {
			_ = s.icons.Delete(iconstore.StorageKey(p.ServerID.String(), hash))
		}
	}
	if bannerChanged && oldBannerURL != nil {
		if hash, ok := iconstore.HashFromURL(*oldBannerURL); ok {
			_ = s.icons.Delete(iconstore.StorageKey(BannerObjectID(p.ServerID), hash))
		}
	}

	srv.Name, srv.NameKey, srv.IconURL, srv.BannerURL = name, key, iconURL, bannerURL
	return srv, nil
}
