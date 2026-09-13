// Package service contiene la logica de negocio del servicio de servidores.
package service

import (
	"context"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
)

type Repo interface {
	CreateServer(ctx context.Context, srv model.Server, channels []model.Channel) error
	ListForUser(ctx context.Context, userID string) ([]model.Server, error)
	GetByID(ctx context.Context, id uuid.UUID) (model.Server, error)
	UpdateServer(ctx context.Context, id uuid.UUID, name, nameKey string, iconURL, bannerURL *string) error
}

type IconStore interface {
	Save(id string, data []byte) (url string, err error)
	Delete(id string) error
	GenerateDefault() (data []byte, err error)
}

type Service struct {
	repo  Repo
	icons IconStore
}

func New(repo Repo, icons IconStore) *Service {
	return &Service{repo: repo, icons: icons}
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

	channels := []model.Channel{
		{
			ID: uuid.New(), ServerID: id, Name: model.DefaultTextChannelName,
			Kind: model.ChannelText, Position: 0, CreatedAt: now,
		},
		{
			ID: uuid.New(), ServerID: id, Name: model.DefaultVoiceChannelName,
			Kind: model.ChannelVoice, Position: 0, CreatedAt: now,
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

	if err := s.repo.CreateServer(ctx, srv, channels); err != nil {
		_ = s.icons.Delete(id.String())
		return model.Server{}, err
	}

	srv.Channels = channels
	return srv, nil
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

	iconURL := srv.IconURL
	if len(p.Icon) > 0 {
		url, err := s.icons.Save(p.ServerID.String(), p.Icon)
		if err != nil {
			return model.Server{}, err
		}
		iconURL = &url
	}

	bannerURL := srv.BannerURL
	switch {
	case len(p.Banner) > 0:
		url, err := s.icons.Save(BannerObjectID(p.ServerID), p.Banner)
		if err != nil {
			return model.Server{}, err
		}
		bannerURL = &url
	case p.RemoveBanner:
		if err := s.icons.Delete(BannerObjectID(p.ServerID)); err != nil {
			return model.Server{}, err
		}
		bannerURL = nil
	}

	if err := s.repo.UpdateServer(ctx, p.ServerID, name, key, iconURL, bannerURL); err != nil {
		return model.Server{}, err
	}

	srv.Name, srv.NameKey, srv.IconURL, srv.BannerURL = name, key, iconURL, bannerURL
	return srv, nil
}
