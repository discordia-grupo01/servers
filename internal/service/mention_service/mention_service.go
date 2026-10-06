// Package mention_service implements the server side of Menciones (#46):
// given the mentions in a message, it decides which ones hold and who gets
// notified. messaging calls it before saving a message with mentions.
package mention_service

import (
	"context"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/mention"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/service"
)

type Service struct {
	servers  Servers
	channels Channels
	members  Members
	roles    Roles
}

func New(servers Servers, channels Channels, members Members, roles Roles) *Service {
	return &Service{servers: servers, channels: channels, members: members, roles: roles}
}

// Resolve checks the mentions of a message the actor is sending to a channel
// of the server:
//   - CA1: a mentioned user counts if they are a member of the server.
//   - CA2/CA3: roles and @everyone count only if the actor holds
//     MENTION_EVERYONE (the owner and administrators hold it implicitly).
//   - CA4: without it they are dropped, so the message goes out with them as
//     plain text instead of being rejected.
//
// Recipients are the members reached who can see the channel, without the
// actor. The actor only has to be a member: whether they may post in the
// channel at all is checked by messaging (SEND_MESSAGES).
func (s *Service) Resolve(ctx context.Context, p ResolveParams) (mention.Resolution, error) {
	if p.ActorUserID == "" {
		return mention.Resolution{}, model.ErrUnauthorized
	}
	userIDs := uniqueUserIDs(p.UserIDs)
	if len(userIDs) > mention.MaxUserIDs {
		return mention.Resolution{}, mention.ErrTooManyUsers
	}
	roleIDs := uniqueRoleIDs(p.RoleIDs)
	if len(roleIDs) > mention.MaxRoleIDs {
		return mention.Resolution{}, mention.ErrTooManyRoles
	}

	srv, err := s.servers.GetByID(ctx, p.ServerID)
	if err != nil {
		return mention.Resolution{}, err
	}
	if err := s.requireChannelOf(ctx, srv, p.ChannelID); err != nil {
		return mention.Resolution{}, err
	}
	actor, err := service.ResolveStanding(ctx, s.members, srv, p.ActorUserID)
	if err != nil {
		return mention.Resolution{}, err
	}
	if !actor.IsMember {
		return mention.Resolution{}, model.ErrForbiddenNotMember
	}

	res := mention.Resolution{}
	if res.UserIDs, err = s.mentionedMembers(ctx, srv.ID, userIDs); err != nil {
		return mention.Resolution{}, err
	}
	if actor.Has(role.PermMentionEveryone) {
		res.Everyone = p.Everyone
		if res.RoleIDs, err = s.mentionedRoles(ctx, srv.ID, roleIDs); err != nil {
			return mention.Resolution{}, err
		}
	}
	recipients, err := s.recipients(ctx, srv, res)
	if err != nil {
		return mention.Resolution{}, err
	}
	res.RecipientIDs = slices.DeleteFunc(recipients, func(id string) bool { return id == p.ActorUserID })
	return res, nil
}

func (s *Service) requireChannelOf(ctx context.Context, srv model.Server, channelID uuid.UUID) error {
	ch, err := s.channels.GetByID(ctx, channelID)
	if err != nil {
		return err
	}
	if ch.ServerID != srv.ID {
		return model.ErrChannelServerMismatch
	}
	return nil
}

func (s *Service) mentionedMembers(ctx context.Context, serverID uuid.UUID, userIDs []string) ([]string, error) {
	if len(userIDs) == 0 {
		return nil, nil
	}
	return s.members.FilterMembers(ctx, serverID, userIDs)
}

func (s *Service) mentionedRoles(ctx context.Context, serverID uuid.UUID, roleIDs []uuid.UUID) ([]uuid.UUID, error) {
	if len(roleIDs) == 0 {
		return nil, nil
	}
	return s.roles.FilterMentionable(ctx, serverID, roleIDs)
}

// recipients expands res into the members who can see the channel. @everyone
// already reaches every member, so the other mentions don't add anyone then.
func (s *Service) recipients(ctx context.Context, srv model.Server, res mention.Resolution) ([]string, error) {
	if res.Everyone {
		return s.members.AllViewers(ctx, srv.ID, srv.OwnerID)
	}
	candidates := slices.Clone(res.UserIDs)
	if len(res.RoleIDs) > 0 {
		holders, err := s.members.MemberIDsWithRoles(ctx, srv.ID, res.RoleIDs)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, holders...)
	}
	candidates = uniqueUserIDs(candidates)
	if len(candidates) == 0 {
		return nil, nil
	}
	return s.members.ViewersAmong(ctx, srv.ID, srv.OwnerID, candidates)
}

func uniqueUserIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func uniqueRoleIDs(ids []uuid.UUID) []uuid.UUID {
	out := slices.Clone(ids)
	slices.SortFunc(out, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	return slices.Compact(out)
}
