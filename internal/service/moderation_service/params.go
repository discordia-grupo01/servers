package moderation_service

import "github.com/google/uuid"

type BanParams struct {
	ServerID     uuid.UUID
	ActorUserID  string
	TargetUserID string
	// Reason is optional; nil or blank means no reason.
	Reason *string
}

type UnbanParams struct {
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
