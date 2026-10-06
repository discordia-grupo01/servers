package channelperm_service

import "github.com/google/uuid"

// SetOverrideParams replaces the override of RoleID in ChannelID with Allow
// and Deny (permission names). Both empty means "no override".
type SetOverrideParams struct {
	ChannelID   uuid.UUID
	RoleID      uuid.UUID
	ActorUserID string
	Allow       []string
	Deny        []string
}

type DeleteOverrideParams struct {
	ChannelID   uuid.UUID
	RoleID      uuid.UUID
	ActorUserID string
}

type ListOverridesParams struct {
	ChannelID   uuid.UUID
	ActorUserID string
}

// EffectivePermissionsParams identifies whose permissions in ChannelID
// ActorUserID wants to see.
type EffectivePermissionsParams struct {
	ChannelID    uuid.UUID
	TargetUserID string
	ActorUserID  string
}
