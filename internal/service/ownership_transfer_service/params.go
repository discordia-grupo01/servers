package ownership_transfer_service

import "github.com/google/uuid"

type InitiateParams struct {
	ServerID    uuid.UUID
	ActorUserID string
	ToUserID    string
}

type RespondParams struct {
	ServerID    uuid.UUID
	TransferID  uuid.UUID
	ActorUserID string
}

type GetPendingParams struct {
	ServerID    uuid.UUID
	ActorUserID string
}
