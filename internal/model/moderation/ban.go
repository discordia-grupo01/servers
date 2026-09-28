// Package moderation contains the domain types of the Moderación epic (#25,
// #26): server bans.
package moderation

import (
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	ReasonMaxLen = 512

	ListDefaultLimit = 20
	ListMaxLimit     = 100
)

var (
	ErrReasonTooLong      = errors.New("reason is too long")
	ErrReasonInvalidChars = errors.New("reason contains control characters")
)

// Ban keeps a user out of a server until it is revoked.
type Ban struct {
	ServerID uuid.UUID
	UserID   string
	Reason   *string
	// BannedBy is nil for bans created before who banned was recorded.
	BannedBy *string
	BannedAt time.Time
}

// NormalizeReason trims raw and validates it. A missing or blank reason is
// stored as nil. Line breaks and tabs are allowed (a reason can span lines);
// any other control character is rejected.
func NormalizeReason(raw *string) (*string, error) {
	if raw == nil {
		return nil, nil
	}
	reason := strings.TrimSpace(*raw)
	if reason == "" {
		return nil, nil
	}
	if utf8.RuneCountInString(reason) > ReasonMaxLen {
		return nil, ErrReasonTooLong
	}
	for _, r := range reason {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return nil, ErrReasonInvalidChars
		}
	}
	return &reason, nil
}

// BanPage is one page of a server's bans, with the limit and offset actually
// applied (they may differ from the requested ones after clamping).
type BanPage struct {
	Bans   []Ban
	Total  int
	Limit  int
	Offset int
}

type BanResponse struct {
	ServerID string    `json:"server_id"`
	UserID   string    `json:"user_id"`
	Reason   *string   `json:"reason"`
	BannedBy *string   `json:"banned_by"`
	BannedAt time.Time `json:"banned_at"`
}

func ToResponse(b Ban) BanResponse {
	return BanResponse{
		ServerID: b.ServerID.String(),
		UserID:   b.UserID,
		Reason:   b.Reason,
		BannedBy: b.BannedBy,
		BannedAt: b.BannedAt,
	}
}

type BanListResponse struct {
	Bans   []BanResponse `json:"bans"`
	Total  int           `json:"total"`
	Limit  int           `json:"limit"`
	Offset int           `json:"offset"`
}
