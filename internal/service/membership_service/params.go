package membership_service

import "github.com/google/uuid"

type LeaveParams struct {
	ServerID     uuid.UUID
	ActorUserID  string
	TargetUserID string
}
