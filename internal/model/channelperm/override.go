// Package channelperm contains the domain of #57 (Sobrescribir permisos de un
// rol en un canal): per-channel permission overrides for a role and the rule
// that turns a user's server-wide permissions into their permissions in one
// channel. It's all pure -- no database access -- so the same rule is used by
// the effective-permissions endpoint, the channel listings and messaging.
package channelperm

import (
	"time"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/role"
)

// Override is what a role gains (Allow) and loses (Deny) in one channel, on
// top of its server-wide permissions. Allow and Deny never overlap and only
// hold role.ChannelScopedPermissions; an override with both empty is not
// stored.
type Override struct {
	ChannelID uuid.UUID
	RoleID    uuid.UUID
	Allow     role.Permission
	Deny      role.Permission
	UpdatedAt time.Time
}

// IsEmpty reports whether the override changes nothing, i.e. it's the same as
// having no override at all.
func (o Override) IsEmpty() bool {
	return o.Allow == 0 && o.Deny == 0
}

// ParseMasks translates the permission names of an override into its allow
// and deny masks. Repeated names are fine. It fails with:
//
//   - *model.InvalidPermissionError for a name that isn't in the catalog,
//   - *model.PermissionNotOverridableError for permissions that only exist at
//     the server level (everything outside role.ChannelScopedPermissions),
//   - *model.PermissionConflictError for permissions both allowed and denied.
func ParseMasks(allowNames, denyNames []string) (allow, deny role.Permission, err error) {
	allow, invalid, ok := role.ParsePermissions(allowNames)
	if !ok {
		return 0, 0, &model.InvalidPermissionError{Value: invalid}
	}
	deny, invalid, ok = role.ParsePermissions(denyNames)
	if !ok {
		return 0, 0, &model.InvalidPermissionError{Value: invalid}
	}
	if notScoped := (allow | deny) &^ role.ChannelScopedPermissions; notScoped != 0 {
		return 0, 0, &model.PermissionNotOverridableError{Permissions: role.PermissionNames(notScoped)}
	}
	if both := allow & deny; both != 0 {
		return 0, 0, &model.PermissionConflictError{Permissions: role.PermissionNames(both)}
	}
	return allow, deny, nil
}

// Effective is what st may do in one channel (#57), Discord-style:
//
//  1. a non-member may do nothing;
//  2. the owner and administrators may do everything -- overrides don't apply
//     to them, so nobody can lock the server's administrators out of a
//     channel;
//  3. otherwise start from the union of their roles' permissions (@everyone
//     included), apply the @everyone override of the channel, and then the
//     overrides of the rest of their roles in the channel, all at once:
//     their denies are removed and their allows added, so within a layer
//     allow wins over deny.
//
// everyone is the channel's override for @everyone (the zero Override if it
// has none); roles are the channel's overrides for the user's other roles
// (roles without an override are simply absent).
func Effective(st role.Standing, everyone Override, roles []Override) role.Permission {
	if !st.IsMember {
		return 0
	}
	if st.IsAdmin() {
		return role.AllPermissions
	}

	perms := st.Permissions
	perms = perms&^everyone.Deny | everyone.Allow

	var allow, deny role.Permission
	for _, o := range roles {
		allow |= o.Allow
		deny |= o.Deny
	}
	return perms&^deny | allow
}

// CanView reports whether st sees the channel at all: VIEW_CHANNELS after
// applying the channel's overrides.
func CanView(st role.Standing, everyone Override, roles []Override) bool {
	return Effective(st, everyone, roles)&role.PermViewChannels != 0
}
