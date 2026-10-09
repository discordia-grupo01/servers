package moderation_service

import (
	"time"

	"github.com/google/uuid"
)

type BanParams struct {
	ServerID     uuid.UUID
	ActorUserID  string
	TargetUserID string
	// Reason is optional; nil or blank means no reason.
	Reason *string
}

type KickParams struct {
	ServerID     uuid.UUID
	ActorUserID  string
	TargetUserID string
}

type UnbanParams struct {
	ServerID     uuid.UUID
	ActorUserID  string
	TargetUserID string
}

type MuteParams struct {
	ServerID     uuid.UUID
	ActorUserID  string
	TargetUserID string
	UntilAt      time.Time
	Reason       *string
}

type UnmuteParams struct {
	ServerID     uuid.UUID
	ActorUserID  string
	TargetUserID string
}

type ListBansParams struct {
	ServerID    uuid.UUID
	ActorUserID string
	Limit       int
	Offset      int
}
