// Package service contiene la logica de negocio del servicio de servidores.
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	channelevents "github.com/discordia-grupo01/events-contract/channels"
	"github.com/discordia-grupo01/events-contract/envelope"
	"github.com/discordia-grupo01/events-contract/membership"
	serverevents "github.com/discordia-grupo01/events-contract/servers"
	"github.com/discordia-grupo01/servers/internal/iconstore"
	"github.com/discordia-grupo01/servers/internal/logging"
	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/channelperm"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/outbox"
)

type Repo interface {
	CreateServer(ctx context.Context, srv model.Server, categories []model.Category, channels []model.Channel, outboxEvents []outbox.Event) error
	ListForUser(ctx context.Context, userID string) ([]model.Server, error)
	GetByID(ctx context.Context, id uuid.UUID) (model.Server, error)
	IsMember(ctx context.Context, id uuid.UUID, userID string) (bool, error)
	UpdateServer(ctx context.Context, id uuid.UUID, name, nameKey string, iconURL, bannerURL *string, iconAnimated bool) error
	IconURLs(ctx context.Context, id uuid.UUID) (iconURL, bannerURL *string, err error)
	VisibleImageURLs(ctx context.Context, id uuid.UUID) (iconURL, bannerURL *string, err error)
	DeleteServer(ctx context.Context, id uuid.UUID, buildEvents func(memberIDs []string, channelIDs []uuid.UUID) ([]outbox.Event, error)) error
}

type IconStore interface {
	Save(id string, data []byte) (url string, err error)
	Delete(id string) error
	DeleteServerEmojis(serverID string) error
}

type Service struct {
	repo       Repo
	icons      IconStore
	standings  StandingReader
	visibility ChannelVisibility
}

func New(repo Repo, icons IconStore, standings StandingReader, visibility ChannelVisibility) *Service {
	return &Service{repo: repo, icons: icons, standings: standings, visibility: visibility}
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
		NameKey:   NameKey(name),
		OwnerID:   p.OwnerID,
		CreatedAt: now,
	}

	textCategoryID, voiceCategoryID, unassignedCategoryID := uuid.New(), uuid.New(), uuid.New()
	categories := []model.Category{
		{
			ID: textCategoryID, ServerID: id, Name: model.DefaultTextCategoryName,
			NameKey:  NameKey(model.DefaultTextCategoryName),
			Position: 0, CreatedAt: now,
		},
		{
			ID: voiceCategoryID, ServerID: id, Name: model.DefaultVoiceCategoryName,
			NameKey:  NameKey(model.DefaultVoiceCategoryName),
			Position: 1, CreatedAt: now,
		},
		{
			// El fallback de cualquier canal sin categoria, sea de texto o de
			// voz -- ver channel_service.unassignedCategoryID.
			ID: unassignedCategoryID, ServerID: id, Name: model.DefaultUnassignedCategoryName,
			NameKey:  NameKey(model.DefaultUnassignedCategoryName),
			Position: 2, IsFallback: true, CreatedAt: now,
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

	if len(p.Icon) > 0 {
		if iconstore.IsGIF(p.Icon) {
			return model.Server{}, &model.BoostLevelRequiredError{Required: model.BoostLevelAnimatedIcon}
		}
		url, err := s.icons.Save(id.String(), p.Icon)
		if err != nil {
			return model.Server{}, err
		}
		srv.IconURL = &url
	}

	memberJoinedEvent, err := outbox.New(membership.RoutingKeyMemberJoined, membership.MemberJoined{
		Meta:     envelope.Meta{EventID: uuid.NewString(), OccurredAt: now},
		ServerID: id.String(),
		UserID:   p.OwnerID,
	})
	if err != nil {
		return model.Server{}, err
	}

	outboxEvents := []outbox.Event{memberJoinedEvent}
	for _, ch := range channels {
		channelCreatedEvent, err := outbox.New(channelevents.RoutingKeyChannelCreated, channelevents.ChannelCreated{
			Meta:      envelope.Meta{EventID: uuid.NewString(), OccurredAt: now},
			ServerID:  ch.ServerID.String(),
			ChannelID: ch.ID.String(),
			Name:      ch.Name,
			Kind:      string(ch.Kind),
		})
		if err != nil {
			return model.Server{}, err
		}
		outboxEvents = append(outboxEvents, channelCreatedEvent)
	}

	if err := s.repo.CreateServer(ctx, srv, categories, channels, outboxEvents); err != nil {
		if srv.IconURL != nil {
			if hash, ok := iconstore.HashFromURL(*srv.IconURL); ok {
				_ = s.icons.Delete(iconstore.StorageKey(id.String(), hash))
			}
		}
		return model.Server{}, err
	}

	srv.Categories = categories
	srv.Channels = channels
	return srv, nil
}

// ListMyServers lists the caller's servers, each with only the channels they
// can see (#57): channels hidden from them by an override are left out. The
// standings and overrides for every server come in two queries, whatever the
// number of servers. If they can't be loaded the call fails -- it never falls
// back to an unfiltered list.
func (s *Service) ListMyServers(ctx context.Context, userID string) ([]model.Server, error) {
	if userID == "" {
		return nil, model.ErrUnauthorized
	}
	servers, err := s.repo.ListForUser(ctx, userID)
	if err != nil || len(servers) == 0 {
		return servers, err
	}

	// A suspended server shows nothing of its content, only that it is
	// suspended: the member can't open it anyway, and there is no visibility to
	// work out for it.
	ids := make([]uuid.UUID, 0, len(servers))
	for i, srv := range servers {
		if srv.Suspended() {
			servers[i].Channels, servers[i].Categories = []model.Channel{}, nil
			continue
		}
		ids = append(ids, srv.ID)
	}
	if len(ids) == 0 {
		return servers, nil
	}
	standings, err := s.visibility.MemberStandings(ctx, userID, ids)
	if err != nil {
		return nil, err
	}
	layers, err := s.visibility.ChannelLayers(ctx, userID, ids)
	if err != nil {
		return nil, err
	}
	for i, srv := range servers {
		if srv.Suspended() {
			continue
		}
		st := standings[srv.ID]
		if srv.OwnerID == userID {
			st = role.Standing{IsMember: true, IsOwner: true}
		}
		servers[i].Channels = channelperm.VisibleChannels(st, srv.Channels, layers)
	}
	return servers, nil
}

// GetServer returns a server with only the channels userID can see (#57). A
// caller who isn't a member gets the server without channels.
func (s *Service) GetServer(ctx context.Context, id uuid.UUID, userID string) (model.Server, error) {
	srv, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return model.Server{}, err
	}
	actor, err := ResolveStanding(ctx, s.standings, srv, userID)
	if err != nil {
		return model.Server{}, err
	}
	if !actor.IsMember {
		srv.Channels = []model.Channel{}
		return srv, nil
	}
	hidden, err := HiddenChannels(ctx, s.visibility, srv, actor, userID)
	if err != nil {
		return model.Server{}, err
	}
	visible := make([]model.Channel, 0, len(srv.Channels))
	for _, ch := range srv.Channels {
		if !hidden[ch.ID] {
			visible = append(visible, ch)
		}
	}
	srv.Channels = visible
	return srv, nil
}

// CheckMembership returns nil if userID owns the server or is a member of it,
// ErrForbiddenNotMember if not and ErrServerNotFound if it doesn't exist. Unlike
// GetServer it loads nothing else, so it is cheap enough to be polled.
func (s *Service) CheckMembership(ctx context.Context, id uuid.UUID, userID string) error {
	if userID == "" {
		return model.ErrUnauthorized
	}
	member, err := s.repo.IsMember(ctx, id, userID)
	if err != nil {
		return err
	}
	if !member {
		return model.ErrForbiddenNotMember
	}
	return nil
}

func (s *Service) GetServerImageURLs(ctx context.Context, id uuid.UUID) (iconURL, bannerURL *string, err error) {
	return s.repo.VisibleImageURLs(ctx, id)
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
	actor, err := ResolveStanding(ctx, s.standings, srv, p.ActorUserID)
	if err != nil {
		return model.Server{}, err
	}
	if err := RequireManageServer(actor); err != nil {
		return model.Server{}, err
	}

	name, key := srv.Name, srv.NameKey
	if p.Name != nil {
		name, err = validateServerName(*p.Name)
		if err != nil {
			return model.Server{}, err
		}
		key = NameKey(name)
	}

	if err := checkImagePerks(srv, p); err != nil {
		return model.Server{}, err
	}

	oldIconURL, oldBannerURL := srv.IconURL, srv.BannerURL

	iconURL, iconAnimated := srv.IconURL, srv.IconAnimated
	iconChanged := len(p.Icon) > 0
	if iconChanged {
		iconAnimated = iconstore.IsGIF(p.Icon)
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

	if err := s.repo.UpdateServer(ctx, p.ServerID, name, key, iconURL, bannerURL, iconAnimated); err != nil {
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

	srv.Name, srv.NameKey, srv.IconURL, srv.BannerURL, srv.IconAnimated = name, key, iconURL, bannerURL, iconAnimated
	return srv, nil
}

// checkImagePerks runs before anything is uploaded. Removing a banner needs no
// level: a server that dropped below it can still clear what it stored.
func checkImagePerks(srv model.Server, p UpdateServerParams) error {
	if iconstore.IsGIF(p.Icon) && srv.BoostLevel < model.BoostLevelAnimatedIcon {
		return &model.BoostLevelRequiredError{Required: model.BoostLevelAnimatedIcon}
	}
	if len(p.Banner) == 0 {
		return nil
	}
	if srv.BoostLevel < model.BoostLevelBanner {
		return &model.BoostLevelRequiredError{Required: model.BoostLevelBanner}
	}
	if iconstore.IsGIF(p.Banner) {
		return fmt.Errorf("%w: a banner can't be a GIF", model.ErrIconUnsupportedType)
	}
	return nil
}

type DeleteServerParams struct {
	ServerID    uuid.UUID
	ActorUserID string
	ConfirmName string
}

func (s *Service) DeleteServer(ctx context.Context, p DeleteServerParams) error {
	if p.ActorUserID == "" {
		return model.ErrUnauthorized
	}

	srv, err := s.repo.GetByID(ctx, p.ServerID)
	if err != nil {
		return err
	}
	if srv.OwnerID != p.ActorUserID {
		return model.ErrForbiddenNotOwner
	}
	if strings.TrimSpace(p.ConfirmName) != srv.Name {
		return model.ErrDeleteConfirmationMismatch
	}

	if err := s.repo.DeleteServer(ctx, p.ServerID, deletionEvents(p.ServerID)); err != nil {
		return err
	}

	s.removeImages(srv)
	return nil
}

// DeleteServerByStaff borra el servidor por pedido de la plataforma
// (admin.server_deletion_requested). Hace lo mismo que DeleteServer, con la
// misma transaccion y los mismos eventos, pero sin pedir ser el owner ni
// confirmar el nombre: esa confirmacion ya la hizo el staff en admin.
//
// Es idempotente: si el servidor ya no existe (un pedido repetido, o el owner
// lo borro antes) devuelve nil y no publica nada.
func (s *Service) DeleteServerByStaff(ctx context.Context, serverID uuid.UUID) error {
	iconURL, bannerURL, err := s.repo.IconURLs(ctx, serverID)
	if errors.Is(err, model.ErrServerNotFound) {
		return nil
	}
	if err != nil {
		return err
	}

	err = s.repo.DeleteServer(ctx, serverID, deletionEvents(serverID))
	if errors.Is(err, model.ErrServerNotFound) {
		return nil
	}
	if err != nil {
		return err
	}

	s.removeImages(model.Server{ID: serverID, IconURL: iconURL, BannerURL: bannerURL})
	return nil
}

// deletionEvents arma los eventos de borrar un servidor: un member_left por
// miembro y servers.server_deleted. Los comparten DeleteServer y
// DeleteServerByStaff para que sean identicos sin importar quien lo pidio.
func deletionEvents(serverID uuid.UUID) func(memberIDs []string, channelIDs []uuid.UUID) ([]outbox.Event, error) {
	return func(memberIDs []string, channelIDs []uuid.UUID) ([]outbox.Event, error) {
		now := time.Now().UTC()
		events := make([]outbox.Event, 0, len(memberIDs)+1)
		for _, userID := range memberIDs {
			evt, err := outbox.New(membership.RoutingKeyMemberLeft, membership.MemberLeft{
				Meta:     envelope.Meta{EventID: uuid.NewString(), OccurredAt: now},
				ServerID: serverID.String(),
				UserID:   userID,
			})
			if err != nil {
				return nil, err
			}
			events = append(events, evt)
		}

		ids := make([]string, 0, len(channelIDs))
		for _, id := range channelIDs {
			ids = append(ids, id.String())
		}
		deleted, err := outbox.New(serverevents.RoutingKeyServerDeleted, serverevents.ServerDeleted{
			Meta:       envelope.Meta{EventID: uuid.NewString(), OccurredAt: now},
			ServerID:   serverID.String(),
			ChannelIDs: ids,
		})
		if err != nil {
			return nil, err
		}
		return append(events, deleted), nil
	}
}

func (s *Service) removeImages(srv model.Server) {
	if srv.IconURL != nil {
		if hash, ok := iconstore.HashFromURL(*srv.IconURL); ok {
			_ = s.icons.Delete(iconstore.StorageKey(srv.ID.String(), hash))
		}
	}
	if srv.BannerURL != nil {
		if hash, ok := iconstore.HashFromURL(*srv.BannerURL); ok {
			_ = s.icons.Delete(iconstore.StorageKey(BannerObjectID(srv.ID), hash))
		}
	}
	if err := s.icons.DeleteServerEmojis(srv.ID.String()); err != nil {
		logging.Warnf("server %s deleted, but its emoji images remain: %v", srv.ID, err)
	}
}
