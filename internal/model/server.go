package model

import (
	"time"

	"github.com/google/uuid"
)

const (
	ServerNameMinLen = 2
	ServerNameMaxLen = 100
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

type Server struct {
	ID        uuid.UUID
	Name      string
	NameKey   string // No se si es muy necesario pero es para el tema de la mayucula, minusculas y tilde
	IconURL   *string
	OwnerID   string
	CreatedAt time.Time
	Channels  []Channel
}

type Channel struct {
	ID        uuid.UUID
	ServerID  uuid.UUID
	Name      string
	Kind      ChannelKind
	Position  int
	CreatedAt time.Time
}
