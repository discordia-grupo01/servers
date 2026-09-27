package model

import (
	"time"

	"github.com/google/uuid"
)

const (
	CategoryNameMinLen = 1
	CategoryNameMaxLen = 100
)

type Category struct {
	ID          uuid.UUID
	ServerID    uuid.UUID
	Name        string
	NameKey     string
	Position    int
	DefaultKind *ChannelKind
	CreatedAt   time.Time
}
