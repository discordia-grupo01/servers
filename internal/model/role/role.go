// Package role contains the Role domain type and its permission catalog.
package role

import (
	"time"

	"github.com/google/uuid"
)

const (
	NameMinLen = 1
	NameMaxLen = 100
)

const (
	EveryoneRoleName  = "@everyone"
	EveryoneRoleColor = "#9e9e9e"
)

type Role struct {
	ID          uuid.UUID
	ServerID    uuid.UUID
	Name        string
	Color       string
	Permissions int64
	Position    int  // rank in the server's hierarchy: 1 is the highest
	IsEveryone  bool // IsEveryone marks the server's single @everyone role
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
