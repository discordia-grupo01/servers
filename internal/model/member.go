package model

import (
	"time"

	"github.com/discordia-grupo01/servers/internal/model/userprofile"
)

const (
	MemberListDefaultLimit = 20
	MemberListMaxLimit     = 100
)

type Member struct {
	UserID   string
	IsOwner  bool
	JoinedAt time.Time
	// Profile is the member's replicated public profile, nil until identify's
	// users.profile_updated event for them has arrived.
	Profile *userprofile.Profile
}

type MemberResponse struct {
	UserID   string    `json:"user_id"`
	IsOwner  bool      `json:"is_owner"`
	JoinedAt time.Time `json:"joined_at"`
	// Profile is null while the member's profile hasn't been replicated from
	// identify yet; clients can fall back to GET /v1/users/{id} then.
	Profile *MemberProfileResponse `json:"profile"`
}

// MemberProfileResponse is the public profile of a member, as last published
// by identify. It may lag a few seconds behind a profile edit.
type MemberProfileResponse struct {
	Name        string `json:"name"`
	AvatarURL   string `json:"avatar_url"`
	Description string `json:"description"`
	StatusText  string `json:"status_text"`
	StatusEmoji string `json:"status_emoji"`
}

type MemberListResponse struct {
	Members []MemberResponse `json:"members"`
	Total   int              `json:"total"`
	Limit   int              `json:"limit"`
	Offset  int              `json:"offset"`
}
