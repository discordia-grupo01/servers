package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/role"
)

type ServerGetter interface {
	GetByID(ctx context.Context, id uuid.UUID) (model.Server, error)
}

type MemberChecker interface {
	IsMember(ctx context.Context, serverID uuid.UUID, userID string) (bool, error)
}

// StandingReader loads a user's membership and effective permissions in a
// server. It does not know about ownership -- ResolveStanding fills that in
// from the server row.
type StandingReader interface {
	MemberStanding(ctx context.Context, serverID uuid.UUID, userID string) (role.Standing, error)
}

// ResolveStanding returns userID's standing in srv. The owner short-circuits
// without a lookup: they administer the server regardless of their roles.
func ResolveStanding(ctx context.Context, r StandingReader, srv model.Server, userID string) (role.Standing, error) {
	if userID != "" && srv.OwnerID == userID {
		return role.Standing{IsMember: true, IsOwner: true}, nil
	}
	if userID == "" {
		return role.Standing{}, nil
	}
	st, err := r.MemberStanding(ctx, srv.ID, userID)
	if err != nil {
		return role.Standing{}, err
	}
	st.IsOwner = false
	return st, nil
}
