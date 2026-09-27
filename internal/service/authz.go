package service

import (
	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/role"
)

// RequireManageRoles checks whether the actor may manage roles (and role
// assignments): the owner, or a member whose roles grant MANAGE_ROLES.
// ADMINISTRATOR implies it too, since Standing.Has treats admins as holding
// every permission -- once a role grants a permission, it must take effect
// for its members immediately, not only for full administrators.
//
// Exported because role_service and member_role_service live in their own
// subpackages and need to call it from outside package service.
func RequireManageRoles(actor role.Standing) error {
	return requirePermission(actor, role.PermManageRoles)
}

// RequireManageChannels checks whether the actor may manage channels and
// categories: the owner, or a member whose roles grant MANAGE_CHANNELS (or
// ADMINISTRATOR)
func RequireManageChannels(actor role.Standing) error {
	return requirePermission(actor, role.PermManageChannels)
}

// RequireManageServer checks whether the actor may edit the server's
// settings: the owner, or a member whose roles grant MANAGE_SERVER (or
// ADMINISTRATOR)
func RequireManageServer(actor role.Standing) error {
	return requirePermission(actor, role.PermManageServer)
}

// RequireCreateInvite checks whether the actor may generate invitations and
// list the server's invitations: administrators, or members whose roles grant
// CREATE_INVITE.
func RequireCreateInvite(actor role.Standing) error {
	if !actor.IsMember || !actor.Has(role.PermCreateInvite) {
		return model.ErrInvitePermissionDenied
	}
	return nil
}

// RequireRevokeInvite checks whether the actor may revoke inv: administrators
// revoke any invitation of the server, and a member can always revoke the
// ones they created.
func RequireRevokeInvite(actor role.Standing, actorID string, inv model.Invitation) error {
	if !actor.IsMember || (!actor.IsAdmin() && inv.CreatedBy != actorID) {
		return model.ErrInvitePermissionDenied
	}
	return nil
}

func requirePermission(actor role.Standing, p role.Permission) error {
	if !actor.IsMember || !actor.Has(p) {
		return model.ErrForbiddenNotAdmin
	}
	return nil
}
