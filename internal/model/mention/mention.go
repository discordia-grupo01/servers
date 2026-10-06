// Package mention contains the domain types of the Menciones story (#46):
// resolving which members a message's mentions reach.
package mention

import (
	"errors"

	"github.com/google/uuid"
)

// Caps on how many distinct users and roles one message can mention, so a
// single request can't make the server expand an unbounded list.
const (
	MaxUserIDs = 50
	MaxRoleIDs = 20
)

var (
	ErrTooManyUsers = errors.New("too many mentioned users")
	ErrTooManyRoles = errors.New("too many mentioned roles")
)

// Resolution is what a message's mentions actually amount to once checked
// against the server: the mentions that hold, and who gets notified.
type Resolution struct {
	// UserIDs are the mentioned users that are members of the server.
	UserIDs []string
	// RoleIDs are the mentioned roles that belong to the server, other than
	// @everyone, and only if the author holds MENTION_EVERYONE.
	RoleIDs []uuid.UUID
	// Everyone is true only if @everyone was mentioned and the author holds
	// MENTION_EVERYONE.
	Everyone bool
	// RecipientIDs is everyone reached by the mentions above who can see the
	// channel, without the author.
	RecipientIDs []string
}

type ResolutionResponse struct {
	UserIDs      []string `json:"user_ids"`
	RoleIDs      []string `json:"role_ids"`
	Everyone     bool     `json:"everyone"`
	RecipientIDs []string `json:"recipient_ids"`
}

func ToResponse(r Resolution) ResolutionResponse {
	roleIDs := make([]string, 0, len(r.RoleIDs))
	for _, id := range r.RoleIDs {
		roleIDs = append(roleIDs, id.String())
	}
	return ResolutionResponse{
		UserIDs:      nonNil(r.UserIDs),
		RoleIDs:      roleIDs,
		Everyone:     r.Everyone,
		RecipientIDs: nonNil(r.RecipientIDs),
	}
}

func nonNil(ids []string) []string {
	if ids == nil {
		return []string{}
	}
	return ids
}
