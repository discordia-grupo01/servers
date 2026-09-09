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
	Create(ctx context.Context, r role.Role) error
	GetByID(ctx context.Context, id uuid.UUID) (role.Role, error)
	ListByServer(ctx context.Context, serverID uuid.UUID) ([]role.Role, error)
	Update(ctx context.Context, r role.Role) error
}

type RoleService struct {
	servers Servers
	roles   Repo
}

func New(servers Servers, roles Repo) *RoleService {
	return &RoleService{servers: servers, roles: roles}
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
	if err := service.RequireManageRoles(srv, p.UserID); err != nil {
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
		Permissions: 0,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := s.roles.Create(ctx, rl); err != nil {
		return role.Role{}, err
	}
	// TODO(events): publicar "roles.role_created" en discordia.events cuando exista un consumidor real (ver Canales).
	return rl, nil
}

// ListRoles lists every role of a server. It only checks that the server
// exists; it does not (yet) check that the caller is a member of it -- there
// is no member-lookup dependency wired into this service until
// member_role_service/member_repo land in a later commit. See
// IMPLEMENTATION.md for this known gap.
func (s *RoleService) ListRoles(ctx context.Context, serverID uuid.UUID) ([]role.Role, error) {
	if _, err := s.servers.GetByID(ctx, serverID); err != nil {
		return nil, err
	}
	return s.roles.ListByServer(ctx, serverID)
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
	if err := service.RequireManageRoles(srv, p.UserID); err != nil {
		return role.Role{}, err
	}

	if p.Permissions != nil {
		mask, invalid, ok := role.ParsePermissions(p.Permissions)
		if !ok {
			return role.Role{}, &model.InvalidPermissionError{Value: invalid}
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
	rl.UpdatedAt = time.Now().UTC()

	if err := s.roles.Update(ctx, rl); err != nil {
		return role.Role{}, err
	}
	// TODO(events): publicar "roles.role_permissions_updated" en discordia.events cuando exista un consumidor real (ver Canales).
	return rl, nil
}
