package model

import (
	"time"

	"github.com/google/uuid"
)

type Invitation struct {
	ID        uuid.UUID
	Code      string
	ServerID  uuid.UUID
	CreatedBy string
	MaxUses   *int
	Uses      int
	ExpiresAt *time.Time
	RevokedAt *time.Time
	CreatedAt time.Time
}

func (i Invitation) IsRevoked() bool { return i.RevokedAt != nil }

func (i Invitation) IsExpired(now time.Time) bool {
	return i.ExpiresAt != nil && !now.Before(*i.ExpiresAt)
}

func (i Invitation) UsesExhausted() bool {
	return i.MaxUses != nil && i.Uses >= *i.MaxUses
}

func (i Invitation) Validate(now time.Time) error {
	switch {
	case i.IsRevoked():
		return ErrInvitationRevoked
	case i.IsExpired(now):
		return ErrInvitationExpired
	case i.UsesExhausted():
		return ErrInvitationExhausted
	default:
		return nil
	}
}

type GenerateInvitationRequest struct {
	MaxUses          *int   `json:"max_uses"`
	ExpiresInSeconds *int64 `json:"expires_in_seconds"`
}

type InvitationResponse struct {
	Code      string     `json:"code"`
	URL       string     `json:"url"`
	ServerID  string     `json:"server_id"`
	CreatedBy string     `json:"created_by"`
	MaxUses   *int       `json:"max_uses"`
	Uses      int        `json:"uses"`
	ExpiresAt *time.Time `json:"expires_at"`
	RevokedAt *time.Time `json:"revoked_at"`
	CreatedAt time.Time  `json:"created_at"`
}

type JoinResponse struct {
	ServerID      string `json:"server_id"`
	AlreadyMember bool   `json:"already_member"`
}
