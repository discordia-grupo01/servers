package model

import "time"

const (
	MemberListDefaultLimit = 20
	MemberListMaxLimit     = 100
)

type Member struct {
	UserID   string
	IsOwner  bool
	JoinedAt time.Time
}

type MemberResponse struct {
	UserID   string    `json:"user_id"`
	IsOwner  bool      `json:"is_owner"`
	JoinedAt time.Time `json:"joined_at"`
}

type MemberListResponse struct {
	Members []MemberResponse `json:"members"`
	Total   int              `json:"total"`
	Limit   int              `json:"limit"`
	Offset  int              `json:"offset"`
}
