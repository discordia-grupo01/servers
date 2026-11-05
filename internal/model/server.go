package model

import (
	"time"

	"github.com/google/uuid"
)

const (
	ServerNameMinLen = 2
	ServerNameMaxLen = 100
)

const (
	ChannelNameMinLen = 1
	ChannelNameMaxLen = 100
)

type ChannelKind string

const (
	ChannelText  ChannelKind = "text"
	ChannelVoice ChannelKind = "voice"
)

const (
	DefaultTextChannelName  = "general"
	DefaultVoiceChannelName = "General"
)

const (
	DefaultTextCategoryName       = "Canales de texto"
	DefaultVoiceCategoryName      = "Canales de voz"
	DefaultUnassignedCategoryName = "Sin asignar"
)

type Server struct {
	ID            uuid.UUID
	Name          string
	NameKey       string // No se si es muy necesario pero es para el tema de la mayucula, minusculas y tilde
	IconURL       *string
	BannerURL     *string
	OwnerID       string
	CreatedAt     time.Time
	Channels      []Channel
	Categories    []Category
	DefaultRoleID *uuid.UUID
	// SuspendedAt is when the platform staff suspended the server, nil if it is
	// active. Only ListForUser fills it.
	SuspendedAt *time.Time

	BoostCount   int
	BoostLevel   int
	IconAnimated bool
	VanityCode   *string
}

// Suspended reports whether the platform staff suspended the server.
func (s Server) Suspended() bool { return s.SuspendedAt != nil }

// A perk's content is kept when the server drops below its level, so it comes
// back if the level does. These are what may be shown meanwhile; IconURL,
// BannerURL and VanityCode are what is stored.

func (s Server) VisibleIconURL() *string {
	if s.IconAnimated && s.BoostLevel < BoostLevelAnimatedIcon {
		return nil
	}
	return s.IconURL
}

func (s Server) VisibleBannerURL() *string {
	if s.BoostLevel < BoostLevelBanner {
		return nil
	}
	return s.BannerURL
}

func (s Server) VisibleVanityCode() *string {
	if s.BoostLevel < BoostLevelVanity {
		return nil
	}
	return s.VanityCode
}

type Channel struct {
	ID         uuid.UUID
	ServerID   uuid.UUID
	Name       string
	Kind       ChannelKind
	Position   int
	CategoryID *uuid.UUID
	Topic      *string
	CreatedAt  time.Time
}
