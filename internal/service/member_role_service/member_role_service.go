// Package member_role_service contains the business logic for assigning and
// removing roles from a server's members.
package member_role_service

import (
	"context"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/service"
)

// Servers is the slice of ServerRepository this service needs.
type Servers interface {
	GetByID(ctx context.Context, id uuid.UUID) (model.Server, error)
}

// Roles is the slice of RoleRepository this service needs.
type Roles interface {
	GetByID(ctx context.Context, id uuid.UUID) (role.Role, error)
}

// Members is the slice of MemberRepository this service needs.
type Members interface {
	service.StandingReader
	IsMember(ctx context.Context, serverID uuid.UUID, userID string) (bool, error)
	HasRole(ctx context.Context, serverID uuid.UUID, userID string, roleID uuid.UUID) (bool, error)
	AssignRole(ctx context.Context, serverID uuid.UUID, userID string, roleID uuid.UUID) error
	RemoveRole(ctx context.Context, serverID uuid.UUID, userID string, roleID uuid.UUID) error
	ListRolesForMember(ctx context.Context, serverID uuid.UUID, userID string) ([]role.Role, error)
}

type MemberRoleService struct {
	servers Servers
	roles   Roles
	members Members
}

func New(servers Servers, roles Roles, members Members) *MemberRoleService {
	return &MemberRoleService{servers: servers, roles: roles, members: members}
}

// resolveRole fetches the role and confirms it belongs to serverID.
func (s *MemberRoleService) resolveRole(ctx context.Context, serverID, roleID uuid.UUID) (role.Role, error) {
	rl, err := s.roles.GetByID(ctx, roleID)
	if err != nil {
		return role.Role{}, err
	}
	if rl.ServerID != serverID {
		return role.Role{}, model.ErrRoleServerMismatch
	}
	return rl, nil
}

type AssignRoleParams struct {
	ServerID     uuid.UUID
	TargetUserID string
	RoleID       uuid.UUID
	ActorUserID  string
}

func (s *MemberRoleService) AssignRole(ctx context.Context, p AssignRoleParams) (role.Role, error) {
	srv, err := s.servers.GetByID(ctx, p.ServerID)
	if err != nil {
		return role.Role{}, err
	}
	actor, err := service.ResolveStanding(ctx, s.members, srv, p.ActorUserID)
	if err != nil {
		return role.Role{}, err
	}
	if err := service.RequireManageRoles(actor); err != nil {
		return role.Role{}, err
	}

	rl, err := s.resolveRole(ctx, p.ServerID, p.RoleID)
	if err != nil {
		return role.Role{}, err
	}

	isMember, err := s.members.IsMember(ctx, p.ServerID, p.TargetUserID)
	if err != nil {
		return role.Role{}, err
	}
	if !isMember {
		return role.Role{}, model.ErrMemberNotFound
	}

	if err := s.members.AssignRole(ctx, p.ServerID, p.TargetUserID, p.RoleID); err != nil {
		return role.Role{}, err
	}
	// TODO(events): publicar "roles.member_role_assigned" en discordia.events cuando exista un consumidor real (ver Canales).
	return rl, nil
}

type RemoveRoleParams struct {
	ServerID     uuid.UUID
	TargetUserID string
	RoleID       uuid.UUID
	ActorUserID  string
}

func (s *MemberRoleService) RemoveRole(ctx context.Context, p RemoveRoleParams) error {
	srv, err := s.servers.GetByID(ctx, p.ServerID)
	if err != nil {
		return err
	}
	actor, err := service.ResolveStanding(ctx, s.members, srv, p.ActorUserID)
	if err != nil {
		return err
	}
	if err := service.RequireManageRoles(actor); err != nil {
		return err
	}

	rl, err := s.resolveRole(ctx, p.ServerID, p.RoleID)
	if err != nil {
		return err
	}
	if p.TargetUserID == p.ActorUserID && role.RemovesOwnAdmin(actor, true, rl.Permissions, 0) {
		has, err := s.members.HasRole(ctx, p.ServerID, p.ActorUserID, p.RoleID)
		if err != nil {
			return err
		}
		// An administrator can't unassign ADMINISTRATOR from themselves (CA2
		// of #22); if they don't hold the role, RemoveRole reports it below.
		if role.RemovesOwnAdmin(actor, has, rl.Permissions, 0) {
			return model.ErrCannotRemoveOwnAdmin
		}
	}

	if err := s.members.RemoveRole(ctx, p.ServerID, p.TargetUserID, p.RoleID); err != nil {
		return err
	}
	// TODO(events): publicar "roles.member_role_removed" en discordia.events cuando exista un consumidor real (ver Canales).
	return nil
}

// MemberLookupParams identifies whose roles or permissions ActorUserID wants
// to see.
type MemberLookupParams struct {
	ServerID     uuid.UUID
	TargetUserID string
	ActorUserID  string
}

// requireMemberLookup lets any member of the server look up another member:
// non-members get ErrForbiddenNotMember (they shouldn't learn who holds which
// roles), and a target that isn't a member is ErrMemberNotFound (instead of
// an empty list that looks like "a member with no roles").
func (s *MemberRoleService) requireMemberLookup(ctx context.Context, p MemberLookupParams) (model.Server, error) {
	srv, err := s.servers.GetByID(ctx, p.ServerID)
	if err != nil {
		return model.Server{}, err
	}
	actor, err := service.ResolveStanding(ctx, s.members, srv, p.ActorUserID)
	if err != nil {
		return model.Server{}, err
	}
	if !actor.IsMember {
		return model.Server{}, model.ErrForbiddenNotMember
	}
	isMember, err := s.members.IsMember(ctx, p.ServerID, p.TargetUserID)
	if err != nil {
		return model.Server{}, err
	}
	if !isMember {
		return model.Server{}, model.ErrMemberNotFound
	}
	return srv, nil
}

// ListMemberRoles lists the roles assigned to a member, for any member of the
// server.
func (s *MemberRoleService) ListMemberRoles(ctx context.Context, p MemberLookupParams) ([]role.Role, error) {
	if _, err := s.requireMemberLookup(ctx, p); err != nil {
		return nil, err
	}
	return s.members.ListRolesForMember(ctx, p.ServerID, p.TargetUserID)
}

// GetMemberPermissions returns the names of the permissions a member actually
// holds, by the same rule the server uses to authorize them (role.Standing):
// every permission for the owner and for members with ADMINISTRATOR, the
// union of their roles' permissions otherwise. Clients can then decide what
// to show with a single "includes" check, without knowing about the owner or
// ADMINISTRATOR. Same access rule as ListMemberRoles.
func (s *MemberRoleService) GetMemberPermissions(ctx context.Context, p MemberLookupParams) ([]string, error) {
	srv, err := s.requireMemberLookup(ctx, p)
	if err != nil {
		return nil, err
	}
	target, err := service.ResolveStanding(ctx, s.members, srv, p.TargetUserID)
	if err != nil {
		return nil, err
	}
	return role.PermissionNames(target.Effective()), nil
}
