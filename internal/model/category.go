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
	ID        uuid.UUID
	ServerID  uuid.UUID
	Name      string
	Position  int
	CreatedAt time.Time
}
