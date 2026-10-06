package moderation

import (
	"time"

	"github.com/google/uuid"
)

// Kick removes a member from a server (#59). Unlike a Ban, nothing is kept
// once it's done: the user can rejoin with any valid invitation.
type Kick struct {
	ServerID uuid.UUID
	UserID   string
	KickedBy string
	KickedAt time.Time
}
