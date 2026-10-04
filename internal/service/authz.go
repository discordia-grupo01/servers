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

// RequirePermission checks whether the actor holds p in the server, through a
// role or implicitly as an administrator (#25/#26 use BAN_MEMBERS). Like
// RequireCreateInvite, it also requires membership: permission bits without
// membership don't authorize anything.
func RequirePermission(actor role.Standing, p role.Permission) error {
	if !actor.IsMember || !actor.Has(p) {
		return model.ErrForbiddenMissingPermission
	}
	return nil
}

// RequireOutranks checks that the actor is strictly above target in the role
// hierarchy (#25 CA2: nobody can act on someone with an equal or higher role).
func RequireOutranks(actor, target role.Standing) error {
	if !actor.Outranks(target) {
		return model.ErrTargetOutranksActor
	}
	return nil
}

// RequireRoleBelow checks that the actor may act on rl (edit, delete, assign,
// unassign or make it the default role): the owner always, anyone else only
// if rl is strictly below their own highest role. It keeps the hierarchy of
// #25 CA2 and #56 CA2 from being bypassed through role management.
func RequireRoleBelow(actor role.Standing, rl role.Role) error {
	if !actor.CanManageRole(rl) {
		return model.ErrRoleAboveOwn
	}
	return nil
}

// RequireCanGrant checks that the actor holds every permission in p before
// granting it -- through editing a role's permissions (only the newly added
// ones) or assigning a role. Removing permissions is never restricted.
func RequireCanGrant(actor role.Standing, p role.Permission) error {
	missing := p &^ actor.Effective()
	if missing == 0 {
		return nil
	}
	return &model.PermissionNotHeldError{Permissions: role.PermissionNames(missing)}
}

// requirePermission is the rule shared by the RequireManageX checks: the
// actor must be a member holding p (owner and ADMINISTRATOR imply every
// permission). It reports ErrForbiddenNotAdmin, unlike RequirePermission.
func requirePermission(actor role.Standing, p role.Permission) error {
	if !actor.IsMember || !actor.Has(p) {
		return model.ErrForbiddenNotAdmin
	}
	return nil
}
