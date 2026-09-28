package role

import "github.com/google/uuid"

// Standing is what a user is allowed to do in a server: whether they are a
// member, whether they own it, the union of the permissions of every role
// assigned to them, and their rank in the role hierarchy. Authorization rules
// are written against a Standing so they stay pure (no DB access) and live in
// one place.
type Standing struct {
	IsMember    bool
	IsOwner     bool
	Permissions Permission
	// TopPosition is the position of the user's highest role (1 is the top of
	// the hierarchy); nil when they hold no role, which ranks below everyone.
	TopPosition *int
}

// IsAdmin reports whether the user administers the server: the owner always
// does, and so does any member holding ADMINISTRATOR through one of their
// roles.
func (s Standing) IsAdmin() bool {
	return s.IsOwner || s.Permissions&PermAdministrator != 0
}

// Has reports whether the user holds p. Administrators implicitly hold every
// permission.
func (s Standing) Has(p Permission) bool {
	return s.IsAdmin() || s.Permissions&p == p
}

// Effective is the set of permissions the user actually holds, by the same
// rule as Has: every permission for administrators (the owner included), and
// the union of their roles' permissions otherwise.
func (s Standing) Effective() Permission {
	if s.IsAdmin() {
		return AllPermissions
	}
	return s.Permissions
}

// CanGrant reports whether the user may hand out every permission in p, by
// adding it to a role or assigning a role that carries it: only permissions
// they hold themselves (all of them for administrators and the owner).
// Without this, a member with MANAGE_ROLES could give a role below theirs
// ADMINISTRATOR and assign it to themselves.
func (s Standing) CanGrant(p Permission) bool {
	return p&^s.Effective() == 0
}

// RemovesOwnAdmin reports whether an operation would take ADMINISTRATOR away
// from the actor through a role they hold: editing that role's permissions
// from before to after, deleting it (after = 0) or unassigning it from
// themselves (after = 0). The owner is exempt -- they administer the server
// regardless of their roles, so the server can never be left without an
// administrator.
// Outranks reports whether s is strictly above o in the server's hierarchy.
// The owner outranks everyone and nobody outranks the owner; otherwise the
// lower TopPosition wins, a user without roles ranks below any role, and two
// users at the same rank don't outrank each other.
func (s Standing) Outranks(o Standing) bool {
	switch {
	case o.IsOwner:
		return false
	case s.IsOwner:
		return true
	case s.TopPosition == nil:
		return false
	case o.TopPosition == nil:
		return true
	default:
		return *s.TopPosition < *o.TopPosition
	}
}

// CanReorder reports whether actor may turn current (the server's roles,
// highest first) into newOrder (role ids, highest first). The owner may
// reorder anything. Anyone else may only rearrange the roles strictly below
// their own highest role: every role at or above it must keep its index, so
// they can neither move those roles nor push a lower role above them (#56 CA2).
// It assumes newOrder is already a permutation of current.
func CanReorder(actor Standing, current []Role, newOrder []uuid.UUID) bool {
	if actor.IsOwner {
		return true
	}
	for i, rl := range current {
		protected := actor.TopPosition == nil || rl.Position <= *actor.TopPosition
		if protected && (i >= len(newOrder) || newOrder[i] != rl.ID) {
			return false
		}
	}
	return true
}

// CanManageRole reports whether the user may edit, delete, assign, unassign
// or make default rl: the owner can act on any role, anyone else only on
// roles strictly below their own highest role. Without this, an administrator
// could hand themselves (or take away from others) a role above their own and
// then act on members who outrank them.
func (s Standing) CanManageRole(rl Role) bool {
	if s.IsOwner {
		return true
	}
	return s.TopPosition != nil && rl.Position > *s.TopPosition
}

func RemovesOwnAdmin(actor Standing, actorHasRole bool, before, after Permission) bool {
	return !actor.IsOwner &&
		actorHasRole &&
		before&PermAdministrator != 0 &&
		after&PermAdministrator == 0
}
