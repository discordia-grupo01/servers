// Package channel_service contains the business logic for creating, editing,
// deleting, moving and reordering channels.
package channel_service

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

// Categories is the slice of CategoryRepository this service needs.
type Categories interface {
	GetByID(ctx context.Context, id uuid.UUID) (model.Category, error)
}

// Repo is the slice of ChannelRepository this service needs.
type Repo interface {
	Create(ctx context.Context, ch model.Channel) error
	GetByID(ctx context.Context, id uuid.UUID) (model.Channel, error)
	Update(ctx context.Context, id uuid.UUID, name string, topic *string) error
	Delete(ctx context.Context, id uuid.UUID) error
	MoveToCategory(ctx context.Context, id uuid.UUID, categoryID *uuid.UUID) error
	ListByServerAndCategory(ctx context.Context, serverID uuid.UUID, categoryID *uuid.UUID) ([]model.Channel, error)
	ReorderPositions(ctx context.Context, orderedIDs []uuid.UUID) error
}

type ChannelService struct {
	servers    Servers
	categories Categories
	channels   Repo
}

func New(servers Servers, categories Categories, channels Repo) *ChannelService {
	return &ChannelService{servers: servers, categories: categories, channels: channels}
}

// resolveCategory fetches the category (if any) and confirms it belongs to serverID.
func (s *ChannelService) resolveCategory(ctx context.Context, serverID uuid.UUID, categoryID *uuid.UUID) error {
	if categoryID == nil {
		return nil
	}
	cat, err := s.categories.GetByID(ctx, *categoryID)
	if err != nil {
		return err
	}
	if cat.ServerID != serverID {
		return model.ErrCategoryServerMismatch
	}
	return nil
}

func validateChannelName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", model.ErrChannelNameRequired
	}
	if utf8.RuneCountInString(name) > model.ChannelNameMaxLen {
		return "", model.ErrChannelNameTooLong
	}
	return name, nil
}

type CreateChannelParams struct {
	ServerID   uuid.UUID
	Name       string
	Kind       string
	CategoryID *uuid.UUID
	UserID     string
}

func (s *ChannelService) CreateChannel(ctx context.Context, p CreateChannelParams) (model.Channel, error) {
	srv, err := s.servers.GetByID(ctx, p.ServerID)
	if err != nil {
		return model.Channel{}, err
	}
	if err := service.RequireManageChannels(srv, p.UserID); err != nil {
		return model.Channel{}, err
	}

	name, err := validateChannelName(p.Name)
	if err != nil {
		return model.Channel{}, err
	}

	kind := model.ChannelKind(p.Kind)
	if kind != model.ChannelText && kind != model.ChannelVoice {
		return model.Channel{}, model.ErrChannelKindInvalid
	}

	if err := s.resolveCategory(ctx, p.ServerID, p.CategoryID); err != nil {
		return model.Channel{}, err
	}

	ch := model.Channel{
		ID:         uuid.New(),
		ServerID:   p.ServerID,
		Name:       name,
		Kind:       kind,
		CategoryID: p.CategoryID,
		CreatedAt:  time.Now().UTC(),
	}
	if err := s.channels.Create(ctx, ch); err != nil {
		return model.Channel{}, err
	}
	// TODO(events): publicar "channels.channel_created" en discordia.events cuando exista un consumidor real.
	return ch, nil
}

type UpdateChannelParams struct {
	ChannelID uuid.UUID
	UserID    string
	Name      *string
	Topic     *string
}

func (s *ChannelService) UpdateChannel(ctx context.Context, p UpdateChannelParams) (model.Channel, error) {
	ch, err := s.channels.GetByID(ctx, p.ChannelID)
	if err != nil {
		return model.Channel{}, err
	}

	srv, err := s.servers.GetByID(ctx, ch.ServerID)
	if err != nil {
		return model.Channel{}, err
	}
	if err := service.RequireManageChannels(srv, p.UserID); err != nil {
		return model.Channel{}, err
	}

	name := ch.Name
	if p.Name != nil {
		name, err = validateChannelName(*p.Name)
		if err != nil {
			return model.Channel{}, err
		}
	}

	topic := ch.Topic
	if p.Topic != nil {
		topic = p.Topic
	}

	if err := s.channels.Update(ctx, p.ChannelID, name, topic); err != nil {
		return model.Channel{}, err
	}
	// TODO(events): publicar "channels.channel_updated" en discordia.events cuando exista un consumidor real.

	ch.Name = name
	ch.Topic = topic
	return ch, nil
}

type DeleteChannelParams struct {
	ChannelID uuid.UUID
	UserID    string
}

// DeleteChannel removes the channel's own row. It does not (and today
// cannot) purge its message history: that lives in the messaging service,
// which has no consumer yet -- see the TODO(events) below, same pattern
// already used by role_service/member_role_service for other mutations.
func (s *ChannelService) DeleteChannel(ctx context.Context, p DeleteChannelParams) error {
	ch, err := s.channels.GetByID(ctx, p.ChannelID)
	if err != nil {
		return err
	}

	srv, err := s.servers.GetByID(ctx, ch.ServerID)
	if err != nil {
		return err
	}
	if err := service.RequireManageChannels(srv, p.UserID); err != nil {
		return err
	}

	if err := s.channels.Delete(ctx, p.ChannelID); err != nil {
		return err
	}
	// TODO(events): publicar "channels.channel_deleted" en discordia.events cuando exista un consumidor real.
	return nil
}

type MoveChannelToCategoryParams struct {
	ChannelID  uuid.UUID
	UserID     string
	CategoryID *uuid.UUID
}

func (s *ChannelService) MoveChannelToCategory(ctx context.Context, p MoveChannelToCategoryParams) (model.Channel, error) {
	ch, err := s.channels.GetByID(ctx, p.ChannelID)
	if err != nil {
		return model.Channel{}, err
	}

	srv, err := s.servers.GetByID(ctx, ch.ServerID)
	if err != nil {
		return model.Channel{}, err
	}
	if err := service.RequireManageChannels(srv, p.UserID); err != nil {
		return model.Channel{}, err
	}

	if err := s.resolveCategory(ctx, ch.ServerID, p.CategoryID); err != nil {
		return model.Channel{}, err
	}

	if err := s.channels.MoveToCategory(ctx, p.ChannelID, p.CategoryID); err != nil {
		return model.Channel{}, err
	}
	// TODO(events): publicar "channels.channel_moved" en discordia.events cuando exista un consumidor real.

	ch.CategoryID = p.CategoryID
	return ch, nil
}

type ReorderChannelsParams struct {
	ServerID   uuid.UUID
	CategoryID *uuid.UUID
	UserID     string
	ChannelIDs []uuid.UUID
}

// ReorderChannels sets the position of every channel in ChannelIDs to match
// its index in that slice. ChannelIDs must be exactly the current membership
// of the (ServerID, CategoryID) bucket -- not a subset, not extra ids -- so a
// client can't silently drop or smuggle in channels via a stale list.
func (s *ChannelService) ReorderChannels(ctx context.Context, p ReorderChannelsParams) error {
	srv, err := s.servers.GetByID(ctx, p.ServerID)
	if err != nil {
		return err
	}
	if err := service.RequireManageChannels(srv, p.UserID); err != nil {
		return err
	}

	if err := s.resolveCategory(ctx, p.ServerID, p.CategoryID); err != nil {
		return err
	}

	current, err := s.channels.ListByServerAndCategory(ctx, p.ServerID, p.CategoryID)
	if err != nil {
		return err
	}
	if !sameChannelSet(current, p.ChannelIDs) {
		return model.ErrChannelReorderInvalid
	}

	if err := s.channels.ReorderPositions(ctx, p.ChannelIDs); err != nil {
		return err
	}
	// TODO(events): publicar "channels.channels_reordered" en discordia.events cuando exista un consumidor real.
	return nil
}

func sameChannelSet(current []model.Channel, ids []uuid.UUID) bool {
	if len(current) != len(ids) {
		return false
	}
	set := make(map[uuid.UUID]bool, len(current))
	for _, ch := range current {
		set[ch.ID] = true
	}
	for _, id := range ids {
		if !set[id] {
			return false
		}
	}
	return true
}
