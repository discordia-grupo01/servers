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
	IsMember(ctx context.Context, serverID uuid.UUID, userID string) (bool, error)
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
	if err := service.RequireManageRoles(srv, p.ActorUserID); err != nil {
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
	if err := service.RequireManageRoles(srv, p.ActorUserID); err != nil {
		return err
	}

	if _, err := s.resolveRole(ctx, p.ServerID, p.RoleID); err != nil {
		return err
	}

	if err := s.members.RemoveRole(ctx, p.ServerID, p.TargetUserID, p.RoleID); err != nil {
		return err
	}
	// TODO(events): publicar "roles.member_role_removed" en discordia.events cuando exista un consumidor real (ver Canales).
	return nil
}

// ListMemberRoles lists the roles assigned to a specific member. It only
// checks that the server exists, same known gap as RoleService.ListRoles
// (see IMPLEMENTATION.md).
func (s *MemberRoleService) ListMemberRoles(ctx context.Context, serverID uuid.UUID, userID string) ([]role.Role, error) {
	if _, err := s.servers.GetByID(ctx, serverID); err != nil {
		return nil, err
	}
	return s.members.ListRolesForMember(ctx, serverID, userID)
}
