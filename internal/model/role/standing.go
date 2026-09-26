package role

// Standing is what a user is allowed to do in a server: whether they are a
// member, whether they own it, and the union of the permissions of every role
// assigned to them. Authorization rules are written against a Standing so
// they stay pure (no DB access) and live in one place.
type Standing struct {
	IsMember    bool
	IsOwner     bool
	Permissions Permission
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

// RemovesOwnAdmin reports whether an operation would take ADMINISTRATOR away
// from the actor through a role they hold: editing that role's permissions
// from before to after, deleting it (after = 0) or unassigning it from
// themselves (after = 0). The owner is exempt -- they administer the server
// regardless of their roles, so the server can never be left without an
// administrator.
func RemovesOwnAdmin(actor Standing, actorHasRole bool, before, after Permission) bool {
	return !actor.IsOwner &&
		actorHasRole &&
		before&PermAdministrator != 0 &&
		after&PermAdministrator == 0
}
