// Package channelperm_service implements #57 (Sobrescribir permisos de un rol
// en un canal): managing a channel's per-role permission overrides and
// computing what a member may do in a channel.
package channelperm_service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/channelperm"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/service"
)

type Service struct {
	servers  Servers
	channels Channels
	roles    Roles
	members  Members
	repo     Repository
	now      func() time.Time
}

func New(servers Servers, channels Channels, roles Roles, members Members, repo Repository) *Service {
	return &Service{
		servers:  servers,
		channels: channels,
		roles:    roles,
		members:  members,
		repo:     repo,
		now:      func() time.Time { return time.Now().UTC() },
	}
}

// SetOverride creates or replaces the override of a role in a channel (#57
// CA1): it applies only to that channel and never touches the role's
// server-wide permissions. With both lists empty the override is removed
// instead (idempotently) and the returned Override is empty.
//
// It needs MANAGE_ROLES (CA2) and a role below the actor's own (@everyone
// excepted, see service.RequireCanOverride), and the actor can only allow
// permissions they hold themselves. Denying is never restricted.
func (s *Service) SetOverride(ctx context.Context, p SetOverrideParams) (channelperm.Override, error) {
	if p.ActorUserID == "" {
		return channelperm.Override{}, model.ErrUnauthorized
	}
	ch, actor, rl, err := s.resolveForWrite(ctx, p.ChannelID, p.RoleID, p.ActorUserID)
	if err != nil {
		return channelperm.Override{}, err
	}

	allow, deny, err := channelperm.ParseMasks(p.Allow, p.Deny)
	if err != nil {
		return channelperm.Override{}, err
	}
	check := func(actor role.Standing, rl role.Role) error {
		if err := service.RequireCanOverride(actor, rl); err != nil {
			return err
		}
		return service.RequireCanGrant(actor, allow)
	}
	if err := check(actor, rl); err != nil {
		return channelperm.Override{}, err
	}

	o := channelperm.Override{ChannelID: ch.ID, RoleID: rl.ID, Allow: allow, Deny: deny, UpdatedAt: s.now()}
	if o.IsEmpty() {
		if _, err := s.repo.Delete(ctx, ch.ServerID, p.ActorUserID, ch.ID, rl.ID, check); err != nil {
			return channelperm.Override{}, err
		}
		return channelperm.Override{ChannelID: ch.ID, RoleID: rl.ID}, nil
	}
	return s.repo.Upsert(ctx, ch.ServerID, p.ActorUserID, o, check)
}

// DeleteOverride removes the override of a role in a channel, so the role
// goes back to its server-wide permissions there. Same authorization as
// SetOverride, without the grant check (removing an override grants nothing
// the role doesn't already have server-wide). ErrChannelOverrideNotFound if
// there was none.
func (s *Service) DeleteOverride(ctx context.Context, p DeleteOverrideParams) error {
	if p.ActorUserID == "" {
		return model.ErrUnauthorized
	}
	ch, actor, rl, err := s.resolveForWrite(ctx, p.ChannelID, p.RoleID, p.ActorUserID)
	if err != nil {
		return err
	}
	if err := service.RequireCanOverride(actor, rl); err != nil {
		return err
	}
	deleted, err := s.repo.Delete(ctx, ch.ServerID, p.ActorUserID, ch.ID, rl.ID, service.RequireCanOverride)
	if err != nil {
		return err
	}
	if !deleted {
		return model.ErrChannelOverrideNotFound
	}
	return nil
}

// ListOverrides lists a channel's overrides, @everyone first and then in the
// order of the role hierarchy. Only who can manage them (MANAGE_ROLES) can
// see them.
func (s *Service) ListOverrides(ctx context.Context, p ListOverridesParams) ([]channelperm.Override, error) {
	if p.ActorUserID == "" {
		return nil, model.ErrUnauthorized
	}
	ch, err := s.channels.GetByID(ctx, p.ChannelID)
	if err != nil {
		return nil, err
	}
	srv, err := s.servers.GetByID(ctx, ch.ServerID)
	if err != nil {
		return nil, err
	}
	actor, err := service.ResolveStanding(ctx, s.members, srv, p.ActorUserID)
	if err != nil {
		return nil, err
	}
	if err := service.RequireManageRoles(actor); err != nil {
		return nil, err
	}
	return s.repo.ListByChannel(ctx, ch.ID)
}

// EffectivePermissions returns the names of the permissions a member actually
// holds in a channel (channelperm.Effective): what messaging and voice check
// before letting them read, write or moderate there. Same access rule as the
// server-wide GET /servers/{id}/members/{userId}/permissions: the caller must
// be a member (ErrForbiddenNotMember) and so must the target
// (ErrMemberNotFound).
func (s *Service) EffectivePermissions(ctx context.Context, p EffectivePermissionsParams) ([]string, error) {
	if p.ActorUserID == "" {
		return nil, model.ErrUnauthorized
	}
	ch, err := s.channels.GetByID(ctx, p.ChannelID)
	if err != nil {
		return nil, err
	}
	srv, err := s.servers.GetByID(ctx, ch.ServerID)
	if err != nil {
		return nil, err
	}
	actor, err := service.ResolveStanding(ctx, s.members, srv, p.ActorUserID)
	if err != nil {
		return nil, err
	}
	if !actor.IsMember {
		return nil, model.ErrForbiddenNotMember
	}
	target, err := service.ResolveStanding(ctx, s.members, srv, p.TargetUserID)
	if err != nil {
		return nil, err
	}
	if !target.IsMember {
		return nil, model.ErrMemberNotFound
	}
	if target.IsAdmin() {
		// Overrides never apply to them: no need to load any.
		return role.PermissionNames(role.AllPermissions), nil
	}

	everyone, roles, err := s.repo.ForMember(ctx, srv.ID, ch.ID, p.TargetUserID)
	if err != nil {
		return nil, err
	}
	return role.PermissionNames(channelperm.Effective(target, everyone, roles)), nil
}

// resolveForWrite loads what SetOverride and DeleteOverride need: the
// channel, the actor's standing in its server and the role, which must belong
// to the same server. The actor must hold MANAGE_ROLES (CA2) -- checked
// before loading the role, so a caller without it can't probe which role ids
// exist; a non-member fails it like any member without the permission.
func (s *Service) resolveForWrite(ctx context.Context, channelID, roleID uuid.UUID, actorID string) (model.Channel, role.Standing, role.Role, error) {
	ch, err := s.channels.GetByID(ctx, channelID)
	if err != nil {
		return model.Channel{}, role.Standing{}, role.Role{}, err
	}
	srv, err := s.servers.GetByID(ctx, ch.ServerID)
	if err != nil {
		return model.Channel{}, role.Standing{}, role.Role{}, err
	}
	actor, err := service.ResolveStanding(ctx, s.members, srv, actorID)
	if err != nil {
		return model.Channel{}, role.Standing{}, role.Role{}, err
	}
	if err := service.RequireManageRoles(actor); err != nil {
		return model.Channel{}, role.Standing{}, role.Role{}, err
	}
	rl, err := s.roles.GetByID(ctx, roleID)
	if err != nil {
		return model.Channel{}, role.Standing{}, role.Role{}, err
	}
	if rl.ServerID != ch.ServerID {
		return model.Channel{}, role.Standing{}, role.Role{}, model.ErrRoleServerMismatch
	}
	return ch, actor, rl, nil
}
