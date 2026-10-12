// Package role_service contains the business logic for creating, listing,
// updating and deleting roles.
package role_service

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/service"
)

var colorPattern = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

// Servers is the slice of ServerRepository this service needs.
type Servers interface {
	GetByID(ctx context.Context, id uuid.UUID) (model.Server, error)
	SetDefaultRole(ctx context.Context, serverID, roleID uuid.UUID) error
}

// Repo is the slice of RoleRepository this service needs.
type Repo interface {
	// Create persists r at the bottom of the hierarchy and returns it with
	// its assigned Position.
	Create(ctx context.Context, r role.Role) (role.Role, error)
	GetByID(ctx context.Context, id uuid.UUID) (role.Role, error)
	ListByServer(ctx context.Context, serverID uuid.UUID) ([]role.Role, error)
	Update(ctx context.Context, r role.Role) error
	Delete(ctx context.Context, id uuid.UUID) error
	Reorder(ctx context.Context, serverID uuid.UUID, actorID string, orderedIDs []uuid.UUID, check func(actor role.Standing, current []role.Role) error) ([]role.Role, error)
}

// Members is the slice of MemberRepository this service needs: the actor's
// standing for authorization, and whether they hold a given role (to stop an
// administrator from removing ADMINISTRATOR from themselves).
type Members interface {
	service.StandingReader
	HasRole(ctx context.Context, serverID uuid.UUID, userID string, roleID uuid.UUID) (bool, error)
	ListRoleMembers(ctx context.Context, serverID, roleID uuid.UUID, limit, offset int) ([]model.Member, int, error)
}

type RoleService struct {
	servers Servers
	roles   Repo
	members Members
}

func New(servers Servers, roles Repo, members Members) *RoleService {
	return &RoleService{servers: servers, roles: roles, members: members}
}

type CreateRoleParams struct {
	ServerID uuid.UUID
	Name     string
	Color    string
	UserID   string
}

func (s *RoleService) CreateRole(ctx context.Context, p CreateRoleParams) (role.Role, error) {
	srv, err := s.servers.GetByID(ctx, p.ServerID)
	if err != nil {
		return role.Role{}, err
	}
	actor, err := service.ResolveStanding(ctx, s.members, srv, p.UserID)
	if err != nil {
		return role.Role{}, err
	}
	if err := service.RequireManageRoles(actor); err != nil {
		return role.Role{}, err
	}

	name := strings.TrimSpace(p.Name)
	if name == "" {
		return role.Role{}, model.ErrRoleNameRequired
	}
	if len(name) > role.NameMaxLen {
		return role.Role{}, model.ErrRoleNameTooLong
	}
	if !colorPattern.MatchString(p.Color) {
		return role.Role{}, model.ErrColorInvalid
	}

	now := time.Now().UTC()
	rl := role.Role{
		ID:          uuid.New(),
		ServerID:    p.ServerID,
		Name:        name,
		Color:       p.Color,
		Permissions: role.DefaultPermissions,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	rl, err = s.roles.Create(ctx, rl)
	if err != nil {
		return role.Role{}, err
	}
	// TODO(events): publicar "roles.role_created" en discordia.events cuando exista un consumidor real (ver Canales).
	return rl, nil
}

// requireMember lets any member of srv (the owner included) read its roles;
// anyone else gets ErrForbiddenNotMember, so outsiders can't learn how the
// server is organized (who can ban, who is an administrator).
func (s *RoleService) requireMember(ctx context.Context, srv model.Server, userID string) error {
	actor, err := service.ResolveStanding(ctx, s.members, srv, userID)
	if err != nil {
		return err
	}
	if !actor.IsMember {
		return model.ErrForbiddenNotMember
	}
	return nil
}

// ListRoles lists every role of a server, highest first, for any member of
// the server.
func (s *RoleService) ListRoles(ctx context.Context, serverID uuid.UUID, userID string) ([]role.Role, error) {
	srv, err := s.servers.GetByID(ctx, serverID)
	if err != nil {
		return nil, err
	}
	if err := s.requireMember(ctx, srv, userID); err != nil {
		return nil, err
	}
	return s.roles.ListByServer(ctx, serverID)
}

// GetRole fetches a single role by id, for any member of the role's server.
func (s *RoleService) GetRole(ctx context.Context, roleID uuid.UUID, userID string) (role.Role, error) {
	rl, err := s.roles.GetByID(ctx, roleID)
	if err != nil {
		return role.Role{}, err
	}
	srv, err := s.servers.GetByID(ctx, rl.ServerID)
	if err != nil {
		return role.Role{}, err
	}
	if err := s.requireMember(ctx, srv, userID); err != nil {
		return role.Role{}, err
	}
	return rl, nil
}

type ListRoleMembersParams struct {
	RoleID uuid.UUID
	UserID string
	Limit  int
	Offset int
}

// ListRoleMembers lists who holds a role (#58), to audit who can do what.
// Requires MANAGE_ROLES, like the rest of role management. For @everyone it
// is every member.
func (s *RoleService) ListRoleMembers(ctx context.Context, p ListRoleMembersParams) ([]model.Member, int, error) {
	if p.UserID == "" {
		return nil, 0, model.ErrUnauthorized
	}
	rl, err := s.roles.GetByID(ctx, p.RoleID)
	if err != nil {
		return nil, 0, err
	}
	srv, err := s.servers.GetByID(ctx, rl.ServerID)
	if err != nil {
		return nil, 0, err
	}
	actor, err := service.ResolveStanding(ctx, s.members, srv, p.UserID)
	if err != nil {
		return nil, 0, err
	}
	if err := service.RequireManageRoles(actor); err != nil {
		return nil, 0, err
	}
	limit, offset := model.ClampMemberPage(p.Limit, p.Offset)
	return s.members.ListRoleMembers(ctx, rl.ServerID, rl.ID, limit, offset)
}

// UpdateRolePermissionsParams. Permissions == nil means "leave the existing
// permissions untouched" (a caller that only wants to rename/recolor a role
// doesn't have to resend its full permission list); Name/Color == nil means
// the same for those two fields.
type UpdateRolePermissionsParams struct {
	RoleID      uuid.UUID
	UserID      string
	Permissions []string
	Name        *string
	Color       *string
}

func (s *RoleService) UpdateRolePermissions(ctx context.Context, p UpdateRolePermissionsParams) (role.Role, error) {
	rl, err := s.roles.GetByID(ctx, p.RoleID)
	if err != nil {
		return role.Role{}, err
	}

	srv, err := s.servers.GetByID(ctx, rl.ServerID)
	if err != nil {
		return role.Role{}, err
	}
	actor, err := service.ResolveStanding(ctx, s.members, srv, p.UserID)
	if err != nil {
		return role.Role{}, err
	}
	if err := service.RequireManageRoles(actor); err != nil {
		return role.Role{}, err
	}

	if rl.IsEveryone && (p.Name != nil || p.Color != nil) {
		return role.Role{}, model.ErrEveryoneRoleIsAutomatic
	}

	if p.Permissions != nil {
		mask, invalid, ok := role.ParsePermissions(p.Permissions)
		if !ok {
			return role.Role{}, &model.InvalidPermissionError{Value: invalid}
		}
		if err := s.requireNotRemovingOwnAdmin(ctx, actor, p.UserID, rl, mask); err != nil {
			return role.Role{}, err
		}
		// Only the permissions being added count: permissions the role already
		// had stay untouched even if the actor lacks them.
		if err := service.RequireCanGrant(actor, mask&^rl.Permissions); err != nil {
			return role.Role{}, err
		}
		rl.Permissions = mask
	}
	if p.Name != nil {
		name := strings.TrimSpace(*p.Name)
		if name == "" {
			return role.Role{}, model.ErrRoleNameRequired
		}
		if len(name) > role.NameMaxLen {
			return role.Role{}, model.ErrRoleNameTooLong
		}
		rl.Name = name
	}
	if p.Color != nil {
		if !colorPattern.MatchString(*p.Color) {
			return role.Role{}, model.ErrColorInvalid
		}
		rl.Color = *p.Color
	}
	if err := service.RequireRoleBelow(actor, rl); err != nil {
		return role.Role{}, err
	}
	rl.UpdatedAt = time.Now().UTC()

	if err := s.roles.Update(ctx, rl); err != nil {
		return role.Role{}, err
	}
	// TODO(events): publicar "roles.role_permissions_updated" en discordia.events cuando exista un consumidor real (ver Canales).
	return rl, nil
}

type SetDefaultRoleParams struct {
	ServerID uuid.UUID
	RoleID   uuid.UUID
	UserID   string
}

func (s *RoleService) SetDefaultRole(ctx context.Context, p SetDefaultRoleParams) error {
	srv, err := s.servers.GetByID(ctx, p.ServerID)
	if err != nil {
		return err
	}
	actor, err := service.ResolveStanding(ctx, s.members, srv, p.UserID)
	if err != nil {
		return err
	}
	if err := service.RequireManageRoles(actor); err != nil {
		return err
	}

	rl, err := s.roles.GetByID(ctx, p.RoleID)
	if err != nil {
		return err
	}
	if rl.ServerID != p.ServerID {
		return model.ErrRoleServerMismatch
	}
	// Every new member gets the default role, so making a role above your
	// own the default would hand it out on your behalf.
	if err := service.RequireRoleBelow(actor, rl); err != nil {
		return err
	}

	if err := s.servers.SetDefaultRole(ctx, p.ServerID, p.RoleID); err != nil {
		return err
	}
	// TODO(events): publicar "roles.default_role_set" en discordia.events cuando exista un consumidor real (ver Canales).
	return nil
}

type ReorderRolesParams struct {
	ServerID uuid.UUID
	UserID   string
	// RoleIDs is the full new hierarchy, highest first.
	RoleIDs []uuid.UUID
}

// ReorderRoles sets the server's role hierarchy to RoleIDs (#56). RoleIDs must
// be exactly the server's current roles -- not a subset, not extra or repeated
// ids -- so a stale client list can't silently drop or reshuffle roles. A
// non-owner administrator may only rearrange the roles below their own
// highest role (CA2). Both checks run inside the repository's transaction,
// against the roles and the actor's rank as they are at write time.
func (s *RoleService) ReorderRoles(ctx context.Context, p ReorderRolesParams) ([]role.Role, error) {
	srv, err := s.servers.GetByID(ctx, p.ServerID)
	if err != nil {
		return nil, err
	}
	actor, err := service.ResolveStanding(ctx, s.members, srv, p.UserID)
	if err != nil {
		return nil, err
	}
	if err := service.RequireManageRoles(actor); err != nil {
		return nil, err
	}

	// The check above fails fast; this one decides, with the actor's rank
	// and the roles re-read under the server lock.
	roles, err := s.roles.Reorder(ctx, p.ServerID, p.UserID, p.RoleIDs, func(actor role.Standing, current []role.Role) error {
		if err := service.RequireManageRoles(actor); err != nil {
			return err
		}
		manageable := excludeEveryone(current)
		if !sameRoleSet(manageable, p.RoleIDs) {
			return model.ErrRoleReorderInvalid
		}
		if !role.CanReorder(actor, manageable, p.RoleIDs) {
			return model.ErrRoleReorderAboveOwn
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	// TODO(events): publicar "roles.roles_reordered" en discordia.events cuando exista un consumidor real (ver Canales).
	return roles, nil
}

func excludeEveryone(roles []role.Role) []role.Role {
	out := make([]role.Role, 0, len(roles))
	for _, rl := range roles {
		if !rl.IsEveryone {
			out = append(out, rl)
		}
	}
	return out
}

// sameRoleSet reports whether ids is a permutation of current's ids.
func sameRoleSet(current []role.Role, ids []uuid.UUID) bool {
	if len(current) != len(ids) {
		return false
	}
	pending := make(map[uuid.UUID]bool, len(current))
	for _, rl := range current {
		pending[rl.ID] = true
	}
	for _, id := range ids {
		if !pending[id] {
			return false // unknown or repeated
		}
		delete(pending, id)
	}
	return true
}

type DeleteRoleParams struct {
	RoleID uuid.UUID
	UserID string
}

func (s *RoleService) DeleteRole(ctx context.Context, p DeleteRoleParams) error {
	rl, err := s.roles.GetByID(ctx, p.RoleID)
	if err != nil {
		return err
	}

	srv, err := s.servers.GetByID(ctx, rl.ServerID)
	if err != nil {
		return err
	}
	actor, err := service.ResolveStanding(ctx, s.members, srv, p.UserID)
	if err != nil {
		return err
	}
	if err := service.RequireManageRoles(actor); err != nil {
		return err
	}
	// @everyone can't be deleted
	if rl.IsEveryone {
		return model.ErrEveryoneRoleIsAutomatic
	}
	if srv.DefaultRoleID != nil && *srv.DefaultRoleID == p.RoleID {
		return model.ErrRoleIsServerDefault
	}
	if err := s.requireNotRemovingOwnAdmin(ctx, actor, p.UserID, rl, 0); err != nil {
		return err
	}
	if err := service.RequireRoleBelow(actor, rl); err != nil {
		return err
	}

	if err := s.roles.Delete(ctx, p.RoleID); err != nil {
		return err
	}
	// TODO(events): publicar "roles.role_deleted" en discordia.events cuando exista un consumidor real (ver Canales).
	return nil
}

// requireNotRemovingOwnAdmin stops an administrator from taking ADMINISTRATOR
// away from themselves through rl -- by changing its permissions to after, or
// by deleting it (after = 0) -- so they can't lock themselves out by mistake
// (CA2 of #22). HasRole is only looked up when the change would actually drop
// ADMINISTRATOR from a role and the actor is not the owner.
func (s *RoleService) requireNotRemovingOwnAdmin(ctx context.Context, actor role.Standing, userID string, rl role.Role, after role.Permission) error {
	if !role.RemovesOwnAdmin(actor, true, rl.Permissions, after) {
		return nil
	}
	has, err := s.members.HasRole(ctx, rl.ServerID, userID, rl.ID)
	if err != nil {
		return err
	}
	if role.RemovesOwnAdmin(actor, has, rl.Permissions, after) {
		return model.ErrCannotRemoveOwnAdmin
	}
	return nil
}
