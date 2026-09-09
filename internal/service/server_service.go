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
}

type IconStore interface {
	Save(id string, data []byte) (url string, err error)
	Delete(id string) error
}

type Service struct {
	repo  Repo
	icons IconStore
}

func New(repo Repo, icons IconStore) *Service {
	return &Service{repo: repo, icons: icons}
}

func (s *Service) CreateServer(ctx context.Context, p CreateServerParams) (model.Server, error) {
	if p.OwnerID == "" {
		return model.Server{}, model.ErrUnauthorized
	}

	name := normalizeName(p.Name)
	if name == "" {
		return model.Server{}, model.ErrNameRequired
	}
	if strings.ContainsFunc(name, unicode.IsControl) {
		return model.Server{}, model.ErrNameInvalidChars
	}

	switch n := utf8.RuneCountInString(name); {
	case n < model.ServerNameMinLen:
		return model.Server{}, model.ErrNameTooShort
	case n > model.ServerNameMaxLen:
		return model.Server{}, model.ErrNameTooLong
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

	iconSaved := false
	if len(p.Icon) > 0 {
		url, err := s.icons.Save(id.String(), p.Icon)
		if err != nil {
			return model.Server{}, err
		}
		srv.IconURL = &url
		iconSaved = true
	}

	if err := s.repo.CreateServer(ctx, srv, channels); err != nil {
		if iconSaved {
			_ = s.icons.Delete(id.String())
		}
		return model.Server{}, err
	}

	srv.Channels = channels
	return srv, nil
}
