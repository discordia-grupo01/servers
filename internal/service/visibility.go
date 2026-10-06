package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/channelperm"
	"github.com/discordia-grupo01/servers/internal/model/role"
)

// ChannelVisibility loads what deciding which channels a user can see takes
// (#57), for several servers at once: their standing in each server and the
// channel overrides that apply to them. Implemented by channelperm_repo.
type ChannelVisibility interface {
	MemberStandings(ctx context.Context, userID string, serverIDs []uuid.UUID) (map[uuid.UUID]role.Standing, error)
	ChannelLayers(ctx context.Context, userID string, serverIDs []uuid.UUID) (map[uuid.UUID]channelperm.Layers, error)
}

// HiddenChannels returns the ids of srv's channels that actor (userID's
// standing in srv) can't see. Administrators see everything, so no lookup is
// made for them.
func HiddenChannels(ctx context.Context, v ChannelVisibility, srv model.Server, actor role.Standing, userID string) (map[uuid.UUID]bool, error) {
	if actor.IsAdmin() {
		return map[uuid.UUID]bool{}, nil
	}
	layers, err := v.ChannelLayers(ctx, userID, []uuid.UUID{srv.ID})
	if err != nil {
		return nil, err
	}
	return channelperm.HiddenChannels(actor, srv.Channels, layers), nil
}

// RequireVisibleChannel reports a channel a member can't see as not found
// (#57): managing it would reveal it exists. Non-members are left to the
// permission checks that follow, as before.
func RequireVisibleChannel(ctx context.Context, v ChannelVisibility, srv model.Server, actor role.Standing, userID string, channelID uuid.UUID) error {
	if !actor.IsMember {
		return nil
	}
	hidden, err := HiddenChannels(ctx, v, srv, actor, userID)
	if err != nil {
		return err
	}
	if hidden[channelID] {
		return model.ErrChannelNotFound
	}
	return nil
}
