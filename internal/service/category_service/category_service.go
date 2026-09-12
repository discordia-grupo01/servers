// Package category_service contains the business logic for creating categories.
package category_service

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/service"
)

// Servers is the slice of ServerRepository this service needs.
type Servers interface {
	GetByID(ctx context.Context, id uuid.UUID) (model.Server, error)
}

// Repo is the slice of CategoryRepository this service needs.
type Repo interface {
	Create(ctx context.Context, cat model.Category) error
}

type CategoryService struct {
	servers Servers
	repo    Repo
}

func New(servers Servers, repo Repo) *CategoryService {
	return &CategoryService{servers: servers, repo: repo}
}

type CreateCategoryParams struct {
	ServerID uuid.UUID
	Name     string
	UserID   string
}

func (s *CategoryService) CreateCategory(ctx context.Context, p CreateCategoryParams) (model.Category, error) {
	srv, err := s.servers.GetByID(ctx, p.ServerID)
	if err != nil {
		return model.Category{}, err
	}
	if err := service.RequireManageChannels(srv, p.UserID); err != nil {
		return model.Category{}, err
	}

	name := strings.TrimSpace(p.Name)
	if name == "" {
		return model.Category{}, model.ErrCategoryNameRequired
	}
	if utf8.RuneCountInString(name) > model.CategoryNameMaxLen {
		return model.Category{}, model.ErrCategoryNameTooLong
	}

	cat := model.Category{
		ID:        uuid.New(),
		ServerID:  p.ServerID,
		Name:      name,
		CreatedAt: time.Now().UTC(),
	}
	if err := s.repo.Create(ctx, cat); err != nil {
		return model.Category{}, err
	}
	// TODO(events): publicar "channels.category_created" en discordia.events cuando exista un consumidor real.
	return cat, nil
}
