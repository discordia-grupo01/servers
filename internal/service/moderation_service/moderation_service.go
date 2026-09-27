// Package moderation_service implements the Moderación epic: banning members
// (#25), revoking bans (#26) and listing bans.
package moderation_service

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/events-contract/envelope"
	"github.com/discordia-grupo01/events-contract/membership"
	moderationevents "github.com/discordia-grupo01/events-contract/moderation"
	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/moderation"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/outbox"
	"github.com/discordia-grupo01/servers/internal/service"
)

type Service struct {
	servers   Servers
	standings Standings
	repo      Repository
	now       func() time.Time
}

func New(servers Servers, standings Standings, repo Repository) *Service {
	return &Service{
		servers:   servers,
		standings: standings,
		repo:      repo,
		now:       func() time.Time { return time.Now().UTC() },
	}
}

// BanMember bans a current member (#25). The actor needs BAN_MEMBERS (the
// owner and administrators hold it implicitly, CA3) and must outrank the
// target in the role hierarchy (CA2); nobody can ban the owner or themselves.
// The member is removed and can't rejoin through any invitation while the ban
// is active (CA1: the join already rejects banned users).
//
// Both servers.member_left and servers.member_banned are published, so
// consumers that only track membership don't need to know about bans.
func (s *Service) BanMember(ctx context.Context, p BanParams) (moderation.Ban, error) {
	if p.ActorUserID == "" {
		return moderation.Ban{}, model.ErrUnauthorized
	}
	target := strings.TrimSpace(p.TargetUserID)
	if target == "" {
		return moderation.Ban{}, model.ErrBanTargetRequired
	}
	reason, err := moderation.NormalizeReason(p.Reason)
	if err != nil {
		return moderation.Ban{}, err
	}

	srv, err := s.servers.GetByID(ctx, p.ServerID)
	if err != nil {
		return moderation.Ban{}, err
	}
	actor, err := service.ResolveStanding(ctx, s.standings, srv, p.ActorUserID)
	if err != nil {
		return moderation.Ban{}, err
	}
	if err := service.RequirePermission(actor, role.PermBanMembers); err != nil {
		return moderation.Ban{}, err
	}

	if target == p.ActorUserID {
		return moderation.Ban{}, model.ErrCannotBanSelf
	}
	if target == srv.OwnerID {
		return moderation.Ban{}, model.ErrCannotBanOwner
	}
	targetStanding, err := service.ResolveStanding(ctx, s.standings, srv, target)
	if err != nil {
		return moderation.Ban{}, err
	}
	if err := service.RequireOutranks(actor, targetStanding); err != nil {
		return moderation.Ban{}, err
	}

	now := s.now()
	bannedBy := p.ActorUserID
	ban := moderation.Ban{
		ServerID: p.ServerID,
		UserID:   target,
		Reason:   reason,
		BannedBy: &bannedBy,
		BannedAt: now,
	}

	memberLeft, err := outbox.New(membership.RoutingKeyMemberLeft, membership.MemberLeft{
		Meta:     envelope.Meta{EventID: uuid.NewString(), OccurredAt: now},
		ServerID: p.ServerID.String(),
		UserID:   target,
	})
	if err != nil {
		return moderation.Ban{}, err
	}
	// reason stays out of the event on purpose (data minimization).
	memberBanned, err := outbox.New(moderationevents.RoutingKeyMemberBanned, moderationevents.MemberBanned{
		Meta:     envelope.Meta{EventID: uuid.NewString(), OccurredAt: now},
		ServerID: p.ServerID.String(),
		UserID:   target,
		BannedBy: p.ActorUserID,
	})
	if err != nil {
		return moderation.Ban{}, err
	}

	if err := s.repo.BanMember(ctx, ban, memberLeft, memberBanned); err != nil {
		return moderation.Ban{}, err
	}
	return ban, nil
}

// UnbanMember revokes a ban (#26). Like banning, it needs BAN_MEMBERS (CA2),
// but there is no hierarchy check: the user is not a member anymore, so they
// have no rank. It does not make them a member again: they can rejoin with
// any valid invitation and get the default role like any new member (CA1).
func (s *Service) UnbanMember(ctx context.Context, p UnbanParams) error {
	if p.ActorUserID == "" {
		return model.ErrUnauthorized
	}
	target := strings.TrimSpace(p.TargetUserID)
	if target == "" {
		return model.ErrBanTargetRequired
	}

	srv, err := s.servers.GetByID(ctx, p.ServerID)
	if err != nil {
		return err
	}
	actor, err := service.ResolveStanding(ctx, s.standings, srv, p.ActorUserID)
	if err != nil {
		return err
	}
	if err := service.RequirePermission(actor, role.PermBanMembers); err != nil {
		return err
	}

	memberUnbanned, err := outbox.New(moderationevents.RoutingKeyMemberUnbanned, moderationevents.MemberUnbanned{
		Meta:       envelope.Meta{EventID: uuid.NewString(), OccurredAt: s.now()},
		ServerID:   p.ServerID.String(),
		UserID:     target,
		UnbannedBy: p.ActorUserID,
	})
	if err != nil {
		return err
	}
	return s.repo.UnbanMember(ctx, p.ServerID, target, memberUnbanned)
}

// ListBans returns a page of the server's bans, newest first. Only who can
// ban (BAN_MEMBERS) can see them.
func (s *Service) ListBans(ctx context.Context, p ListBansParams) (moderation.BanPage, error) {
	if p.ActorUserID == "" {
		return moderation.BanPage{}, model.ErrUnauthorized
	}
	srv, err := s.servers.GetByID(ctx, p.ServerID)
	if err != nil {
		return moderation.BanPage{}, err
	}
	actor, err := service.ResolveStanding(ctx, s.standings, srv, p.ActorUserID)
	if err != nil {
		return moderation.BanPage{}, err
	}
	if err := service.RequirePermission(actor, role.PermBanMembers); err != nil {
		return moderation.BanPage{}, err
	}

	limit := p.Limit
	switch {
	case limit <= 0:
		limit = moderation.ListDefaultLimit
	case limit > moderation.ListMaxLimit:
		limit = moderation.ListMaxLimit
	}
	offset := max(p.Offset, 0)
	bans, total, err := s.repo.ListBans(ctx, p.ServerID, limit, offset)
	if err != nil {
		return moderation.BanPage{}, err
	}
	return moderation.BanPage{Bans: bans, Total: total, Limit: limit, Offset: offset}, nil
}
