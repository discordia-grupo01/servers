package moderation

import (
	"time"

	"github.com/google/uuid"
)

// Mute prevents a member from sending messages and publishing voice until
// UntilAt. It does not remove the member or close an active session.
type Mute struct {
	ServerID uuid.UUID
	UserID   string
	MutedBy  string
	MutedAt  time.Time
	UntilAt  time.Time
	Reason   string
}
