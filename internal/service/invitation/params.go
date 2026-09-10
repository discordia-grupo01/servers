package invitation

import "github.com/google/uuid"

type GenerateParams struct {
	ServerID  string
	CreatedBy string
	MaxUses   *int
}

type RevokeParams struct {
	Code   string
	UserID string
}

type JoinParams struct {
	Code   string
	UserID string
}

type JoinResult struct {
	ServerID      uuid.UUID
	AlreadyMember bool
}
