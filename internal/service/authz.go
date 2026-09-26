package service

import (
	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/role"
)

// RequireManageRoles checks whether the actor may manage roles (and role
// assignments). It is named after the capability it represents, not after
// who holds it today, so that making it more granular (e.g. letting
// MANAGE_ROLES alone suffice) only means changing this function -- not every
// call site.
//
// Exported because role_service and member_role_service live in their own
// subpackages and need to call it from outside package service.
func RequireManageRoles(actor role.Standing) error {
	return requireAdmin(actor)
}

// RequireManageChannels checks whether the actor may manage channels and
// categories. Same "administrators only" scope as RequireManageRoles.
func RequireManageChannels(actor role.Standing) error {
	return requireAdmin(actor)
}

// RequireManageServer checks whether the actor may edit the server's
// settings. Same "administrators only" scope as RequireManageRoles.
func RequireManageServer(actor role.Standing) error {
	return requireAdmin(actor)
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

// requireAdmin is the rule every "as an administrator" user story shares:
// the owner, or a member holding ADMINISTRATOR.
func requireAdmin(actor role.Standing) error {
	if !actor.IsAdmin() {
		return model.ErrForbiddenNotAdmin
	}
	return nil
}
