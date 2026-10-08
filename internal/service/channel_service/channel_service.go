// Package channel_service contains the business logic for creating, editing,
// deleting, moving and reordering channels.
package channel_service

import (
	"context"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/events-contract/channels"
	"github.com/discordia-grupo01/events-contract/envelope"
	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/outbox"
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
	Create(ctx context.Context, ch model.Channel, channelCreatedEvent outbox.Event) error
	GetByID(ctx context.Context, id uuid.UUID) (model.Channel, error)
	Update(ctx context.Context, id uuid.UUID, name string, topic *string) error
	Delete(ctx context.Context, id uuid.UUID, channelDeletedEvent outbox.Event) error
	MoveToCategory(ctx context.Context, id uuid.UUID, categoryID *uuid.UUID) error
	// ReorderPositions reorders the (serverID, categoryID) bucket: requested
	// is the new order of its channels outside hidden. It fails with
	// model.ErrChannelReorderInvalid if requested isn't exactly those.
	ReorderPositions(ctx context.Context, serverID uuid.UUID, categoryID *uuid.UUID, requested []uuid.UUID, hidden map[uuid.UUID]bool) error
}

type ProfileNameReader interface {
	GetName(ctx context.Context, userID string) (string, error)
}

type MuteReader interface {
	IsMuted(ctx context.Context, serverID uuid.UUID, userID string, at time.Time) (bool, error)
}

type ChannelService struct {
	servers    Servers
	categories Categories
	channels   Repo
	standings  service.StandingReader
	visibility service.ChannelVisibility
	profiles   ProfileNameReader
	mutes      MuteReader
}

type VoiceAccess struct {
	ChannelID uuid.UUID
	ServerID  uuid.UUID
	Name      string
	Muted     bool
}

func New(servers Servers, categories Categories, channels Repo, standings service.StandingReader, visibility service.ChannelVisibility, muteReader MuteReader, profiles ...ProfileNameReader) *ChannelService {
	var profileReader ProfileNameReader
	if len(profiles) > 0 {
		profileReader = profiles[0]
	}
	return &ChannelService{servers: servers, categories: categories, channels: channels, standings: standings, visibility: visibility, profiles: profileReader, mutes: muteReader}
}

func (s *ChannelService) AuthorizeVoice(ctx context.Context, channelID uuid.UUID, userID string) (VoiceAccess, error) {
	ch, err := s.channels.GetByID(ctx, channelID)
	if err != nil {
		return VoiceAccess{}, err
	}
	if ch.Kind != model.ChannelVoice {
		return VoiceAccess{}, model.ErrVoiceChannelRequired
	}

	srv, err := s.servers.GetByID(ctx, ch.ServerID)
	if err != nil {
		return VoiceAccess{}, err
	}
	standing, err := service.ResolveStanding(ctx, s.standings, srv, userID)
	if err != nil {
		return VoiceAccess{}, err
	}
	if err := service.RequirePermission(standing, role.PermViewChannels); err != nil {
		return VoiceAccess{}, err
	}

	name := userID
	if s.profiles != nil {
		profileName, err := s.profiles.GetName(ctx, userID)
		if err != nil {
			return VoiceAccess{}, err
		}
		if profileName != "" {
			name = profileName
		}
	}

	muted := false
	if s.mutes != nil {
		muted, err = s.mutes.IsMuted(ctx, ch.ServerID, userID, time.Now().UTC())
		if err != nil {
			return VoiceAccess{}, err
		}
	}

	return VoiceAccess{ChannelID: ch.ID, ServerID: ch.ServerID, Name: name, Muted: muted}, nil
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

// unassignedCategoryID returns the id of the server's fallback category
// ("Sin asignar", IsFallback == true) -- the same one for every channel
// kind -- or nil if it was deleted or never existed -- callers fall back to
// leaving the channel uncategorized in that case.
func unassignedCategoryID(categories []model.Category) *uuid.UUID {
	for _, cat := range categories {
		if cat.IsFallback {
			id := cat.ID
			return &id
		}
	}
	return nil
}

func validateChannelName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", model.ErrChannelNameRequired
	}
	if strings.ContainsFunc(name, unicode.IsControl) {
		return "", model.ErrChannelNameInvalidChars
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
	actor, err := service.ResolveStanding(ctx, s.standings, srv, p.UserID)
	if err != nil {
		return model.Channel{}, err
	}
	if err := service.RequireManageChannels(actor); err != nil {
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

	categoryID := p.CategoryID
	if categoryID == nil {
		categoryID = unassignedCategoryID(srv.Categories)
	}

	ch := model.Channel{
		ID:         uuid.New(),
		ServerID:   p.ServerID,
		Name:       name,
		Kind:       kind,
		CategoryID: categoryID,
		CreatedAt:  time.Now().UTC(),
	}

	evt, err := outbox.New(channels.RoutingKeyChannelCreated, channels.ChannelCreated{
		Meta:      envelope.Meta{EventID: uuid.NewString(), OccurredAt: ch.CreatedAt},
		ServerID:  ch.ServerID.String(),
		ChannelID: ch.ID.String(),
		Name:      ch.Name,
		Kind:      string(ch.Kind),
	})
	if err != nil {
		return model.Channel{}, err
	}
	if err := s.channels.Create(ctx, ch, evt); err != nil {
		return model.Channel{}, err
	}
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
	actor, err := service.ResolveStanding(ctx, s.standings, srv, p.UserID)
	if err != nil {
		return model.Channel{}, err
	}
	// #57: a channel the actor can't see is reported as not found.
	if err := service.RequireVisibleChannel(ctx, s.visibility, srv, actor, p.UserID, ch.ID); err != nil {
		return model.Channel{}, err
	}
	if err := service.RequireManageChannels(actor); err != nil {
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

func (s *ChannelService) DeleteChannel(ctx context.Context, p DeleteChannelParams) error {
	ch, err := s.channels.GetByID(ctx, p.ChannelID)
	if err != nil {
		return err
	}

	srv, err := s.servers.GetByID(ctx, ch.ServerID)
	if err != nil {
		return err
	}
	actor, err := service.ResolveStanding(ctx, s.standings, srv, p.UserID)
	if err != nil {
		return err
	}
	// #57: a channel the actor can't see is reported as not found.
	if err := service.RequireVisibleChannel(ctx, s.visibility, srv, actor, p.UserID, ch.ID); err != nil {
		return err
	}
	if err := service.RequireManageChannels(actor); err != nil {
		return err
	}

	evt, err := outbox.New(channels.RoutingKeyChannelDeleted, channels.ChannelDeleted{
		Meta:      envelope.Meta{EventID: uuid.NewString(), OccurredAt: time.Now().UTC()},
		ServerID:  ch.ServerID.String(),
		ChannelID: ch.ID.String(),
	})
	if err != nil {
		return err
	}
	if err := s.channels.Delete(ctx, p.ChannelID, evt); err != nil {
		return err
	}
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
	actor, err := service.ResolveStanding(ctx, s.standings, srv, p.UserID)
	if err != nil {
		return model.Channel{}, err
	}
	// #57: a channel the actor can't see is reported as not found.
	if err := service.RequireVisibleChannel(ctx, s.visibility, srv, actor, p.UserID, ch.ID); err != nil {
		return model.Channel{}, err
	}
	if err := service.RequireManageChannels(actor); err != nil {
		return model.Channel{}, err
	}

	if err := s.resolveCategory(ctx, ch.ServerID, p.CategoryID); err != nil {
		return model.Channel{}, err
	}

	categoryID := p.CategoryID
	if categoryID == nil {
		categoryID = unassignedCategoryID(srv.Categories)
	}

	if err := s.channels.MoveToCategory(ctx, p.ChannelID, categoryID); err != nil {
		return model.Channel{}, err
	}
	// TODO(events): publicar "channels.channel_moved" en discordia.events cuando exista un consumidor real.

	ch.CategoryID = categoryID
	return ch, nil
}

type ReorderChannelsParams struct {
	ServerID   uuid.UUID
	CategoryID *uuid.UUID
	UserID     string
	ChannelIDs []uuid.UUID
}

// ReorderChannels reorders the channels of the (ServerID, CategoryID) bucket.
// ChannelIDs must be exactly the channels of that bucket the actor can see --
// not a subset, not extra ids -- so a client can't silently drop or smuggle in
// channels via a stale list. Channels hidden from the actor (#57) keep their
// place and the visible ones take the remaining places in the requested
// order; for an actor who sees everything that is simply ChannelIDs.
func (s *ChannelService) ReorderChannels(ctx context.Context, p ReorderChannelsParams) error {
	srv, err := s.servers.GetByID(ctx, p.ServerID)
	if err != nil {
		return err
	}
	actor, err := service.ResolveStanding(ctx, s.standings, srv, p.UserID)
	if err != nil {
		return err
	}
	if err := service.RequireManageChannels(actor); err != nil {
		return err
	}

	if err := s.resolveCategory(ctx, p.ServerID, p.CategoryID); err != nil {
		return err
	}

	hidden, err := service.HiddenChannels(ctx, s.visibility, srv, actor, p.UserID)
	if err != nil {
		return err
	}
	if err := s.channels.ReorderPositions(ctx, p.ServerID, p.CategoryID, p.ChannelIDs, hidden); err != nil {
		return err
	}
	// TODO(events): publicar "channels.channels_reordered" en discordia.events cuando exista un consumidor real.
	return nil
}
