// Package category_service contains the business logic for creating,
// editing and reordering categories.
package category_service

import (
	"context"
	"strings"
	"time"
	"unicode"
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
	GetByID(ctx context.Context, id uuid.UUID) (model.Category, error)
	ListByServer(ctx context.Context, serverID uuid.UUID) ([]model.Category, error)
	Update(ctx context.Context, id uuid.UUID, name string) error
	ReorderPositions(ctx context.Context, orderedIDs []uuid.UUID) error
}

type CategoryService struct {
	servers Servers
	repo    Repo
}

func New(servers Servers, repo Repo) *CategoryService {
	return &CategoryService{servers: servers, repo: repo}
}

func validateCategoryName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", model.ErrCategoryNameRequired
	}
	if strings.ContainsFunc(name, unicode.IsControl) {
		return "", model.ErrCategoryNameInvalidChars
	}
	if utf8.RuneCountInString(name) > model.CategoryNameMaxLen {
		return "", model.ErrCategoryNameTooLong
	}
	return name, nil
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

	name, err := validateCategoryName(p.Name)
	if err != nil {
		return model.Category{}, err
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

type UpdateCategoryParams struct {
	CategoryID uuid.UUID
	UserID     string
	Name       string
}

func (s *CategoryService) UpdateCategory(ctx context.Context, p UpdateCategoryParams) (model.Category, error) {
	cat, err := s.repo.GetByID(ctx, p.CategoryID)
	if err != nil {
		return model.Category{}, err
	}

	srv, err := s.servers.GetByID(ctx, cat.ServerID)
	if err != nil {
		return model.Category{}, err
	}
	if err := service.RequireManageChannels(srv, p.UserID); err != nil {
		return model.Category{}, err
	}

	name, err := validateCategoryName(p.Name)
	if err != nil {
		return model.Category{}, err
	}

	if err := s.repo.Update(ctx, p.CategoryID, name); err != nil {
		return model.Category{}, err
	}
	// TODO(events): publicar "channels.category_updated" en discordia.events cuando exista un consumidor real.

	cat.Name = name
	return cat, nil
}

type ReorderCategoriesParams struct {
	ServerID    uuid.UUID
	UserID      string
	CategoryIDs []uuid.UUID
}

// ReorderCategories sets the position of every category in CategoryIDs to
// match its index in that slice. CategoryIDs must be exactly the current set
// of categories for ServerID -- not a subset, not extra ids -- same
// safeguard as channel_service.ReorderChannels.
func (s *CategoryService) ReorderCategories(ctx context.Context, p ReorderCategoriesParams) error {
	srv, err := s.servers.GetByID(ctx, p.ServerID)
	if err != nil {
		return err
	}
	if err := service.RequireManageChannels(srv, p.UserID); err != nil {
		return err
	}

	current, err := s.repo.ListByServer(ctx, p.ServerID)
	if err != nil {
		return err
	}
	if !sameCategorySet(current, p.CategoryIDs) {
		return model.ErrCategoryReorderInvalid
	}

	if err := s.repo.ReorderPositions(ctx, p.CategoryIDs); err != nil {
		return err
	}
	// TODO(events): publicar "channels.categories_reordered" en discordia.events cuando exista un consumidor real.
	return nil
}

func sameCategorySet(current []model.Category, ids []uuid.UUID) bool {
	if len(current) != len(ids) {
		return false
	}
	set := make(map[uuid.UUID]bool, len(current))
	for _, cat := range current {
		set[cat.ID] = true
	}
	for _, id := range ids {
		if !set[id] {
			return false
		}
	}
	return true
}
