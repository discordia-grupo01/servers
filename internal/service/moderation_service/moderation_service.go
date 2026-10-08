// Package moderation_service implements the Moderación epic: banning members
// (#25), revoking bans (#26), listing bans and kicking members (#59).
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
	muteRepo  MuteRepository
	now       func() time.Time
}

func New(servers Servers, standings Standings, repo Repository, muteRepos ...MuteRepository) *Service {
	var muteRepo MuteRepository
	if len(muteRepos) > 0 {
		muteRepo = muteRepos[0]
	}
	return &Service{
		servers:   servers,
		standings: standings,
		repo:      repo,
		muteRepo:  muteRepo,
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

	// The checks above fail fast with a precise error; this one decides,
	// with both standings re-read under the server lock, so a role change
	// racing the ban can't slip through.
	recheck := func(actor, target role.Standing) error {
		if err := service.RequirePermission(actor, role.PermBanMembers); err != nil {
			return err
		}
		return service.RequireOutranks(actor, target)
	}
	if err := s.repo.BanMember(ctx, ban, recheck, memberLeft, memberBanned); err != nil {
		return moderation.Ban{}, err
	}
	return ban, nil
}

// KickMember removes a current member from the server (#59). The actor needs
// KICK_MEMBERS (the owner and administrators hold it implicitly, CA3) and
// must outrank the target in the role hierarchy (CA2); nobody can kick the
// owner or themselves (to leave, there is the leave endpoint). Unlike a ban,
// nothing keeps them out: they can rejoin with any valid invitation (CA1).
//
// Both servers.member_left and servers.member_kicked are published, so
// consumers that only track membership don't need to know about kicks.
func (s *Service) KickMember(ctx context.Context, p KickParams) error {
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
	if err := service.RequirePermission(actor, role.PermKickMembers); err != nil {
		return err
	}

	if target == p.ActorUserID {
		return model.ErrCannotKickSelf
	}
	if target == srv.OwnerID {
		return model.ErrCannotKickOwner
	}
	targetStanding, err := service.ResolveStanding(ctx, s.standings, srv, target)
	if err != nil {
		return err
	}
	if err := service.RequireOutranks(actor, targetStanding); err != nil {
		return err
	}

	now := s.now()
	memberLeft, err := outbox.New(membership.RoutingKeyMemberLeft, membership.MemberLeft{
		Meta:     envelope.Meta{EventID: uuid.NewString(), OccurredAt: now},
		ServerID: p.ServerID.String(),
		UserID:   target,
	})
	if err != nil {
		return err
	}
	memberKicked, err := outbox.New(moderationevents.RoutingKeyMemberKicked, moderationevents.MemberKicked{
		Meta:     envelope.Meta{EventID: uuid.NewString(), OccurredAt: now},
		ServerID: p.ServerID.String(),
		UserID:   target,
		KickedBy: p.ActorUserID,
	})
	if err != nil {
		return err
	}

	// Same as BanMember: the checks above fail fast with a precise error;
	// this one decides, with both standings re-read under the server lock.
	recheck := func(actor, target role.Standing) error {
		if err := service.RequirePermission(actor, role.PermKickMembers); err != nil {
			return err
		}
		return service.RequireOutranks(actor, target)
	}
	kick := moderation.Kick{ServerID: p.ServerID, UserID: target, KickedBy: p.ActorUserID, KickedAt: now}
	return s.repo.KickMember(ctx, kick, recheck, memberLeft, memberKicked)
}

// MuteMember temporarily prevents a current member from sending messages and
// publishing voice. Membership and existing sessions remain intact.
func (s *Service) MuteMember(ctx context.Context, p MuteParams) error {
	if s.muteRepo == nil {
		return model.ErrMuteNotFound
	}
	if p.ActorUserID == "" {
		return model.ErrUnauthorized
	}
	target := strings.TrimSpace(p.TargetUserID)
	if target == "" {
		return model.ErrMuteTargetRequired
	}
	now := s.now()
	if !p.UntilAt.After(now) {
		return model.ErrMuteUntilInvalid
	}
	reason, err := moderation.NormalizeReason(p.Reason)
	if err != nil {
		return err
	}
	reasonValue := ""
	if reason != nil {
		reasonValue = *reason
	}

	srv, err := s.servers.GetByID(ctx, p.ServerID)
	if err != nil {
		return err
	}
	actor, err := service.ResolveStanding(ctx, s.standings, srv, p.ActorUserID)
	if err != nil {
		return err
	}
	if err := service.RequirePermission(actor, role.PermManageMessages); err != nil {
		return err
	}
	if target == p.ActorUserID {
		return model.ErrCannotMuteSelf
	}
	if target == srv.OwnerID {
		return model.ErrCannotMuteOwner
	}
	targetStanding, err := service.ResolveStanding(ctx, s.standings, srv, target)
	if err != nil {
		return err
	}
	if err := service.RequireOutranks(actor, targetStanding); err != nil {
		return err
	}

	mute := moderation.Mute{
		ServerID: p.ServerID,
		UserID:   target,
		MutedBy:  p.ActorUserID,
		MutedAt:  now,
		UntilAt:  p.UntilAt,
		Reason:   reasonValue,
	}
	event, err := outbox.New(moderationevents.RoutingKeyMemberMuted, moderationevents.MemberMuted{
		Meta:     envelope.Meta{EventID: uuid.NewString(), OccurredAt: now},
		ServerID: p.ServerID.String(),
		UserID:   target,
		MutedBy:  p.ActorUserID,
		UntilAt:  p.UntilAt,
		Reason:   reasonValue,
	})
	if err != nil {
		return err
	}
	recheck := func(actor, target role.Standing) error {
		if err := service.RequirePermission(actor, role.PermManageMessages); err != nil {
			return err
		}
		return service.RequireOutranks(actor, target)
	}
	return s.muteRepo.MuteMember(ctx, mute, recheck, event)
}

// UnmuteMember manually restores a member's ability to send and publish.
func (s *Service) UnmuteMember(ctx context.Context, p UnmuteParams) error {
	if s.muteRepo == nil {
		return model.ErrMuteNotFound
	}
	if p.ActorUserID == "" {
		return model.ErrUnauthorized
	}
	target := strings.TrimSpace(p.TargetUserID)
	if target == "" {
		return model.ErrUnmuteTargetRequired
	}

	srv, err := s.servers.GetByID(ctx, p.ServerID)
	if err != nil {
		return err
	}
	actor, err := service.ResolveStanding(ctx, s.standings, srv, p.ActorUserID)
	if err != nil {
		return err
	}
	if err := service.RequirePermission(actor, role.PermManageMessages); err != nil {
		return err
	}
	event, err := outbox.New(moderationevents.RoutingKeyMemberUnmuted, moderationevents.MemberUnmuted{
		Meta:      envelope.Meta{EventID: uuid.NewString(), OccurredAt: s.now()},
		ServerID:  p.ServerID.String(),
		UserID:    target,
		UnmutedBy: p.ActorUserID,
		Reason:    "manual",
	})
	if err != nil {
		return err
	}
	return s.muteRepo.UnmuteMember(ctx, p.ServerID, target, event)
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
