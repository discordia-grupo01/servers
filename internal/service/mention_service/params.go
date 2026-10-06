package mention_service

import "github.com/google/uuid"

// ResolveParams are the mentions found in a message the actor is sending to
// ChannelID
type ResolveParams struct {
	ServerID    uuid.UUID
	ChannelID   uuid.UUID
	ActorUserID string
	UserIDs     []string
	RoleIDs     []uuid.UUID
	Everyone    bool
}
