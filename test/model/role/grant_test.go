package role_test

import (
	"testing"

	"github.com/discordia-grupo01/servers/internal/model/role"
)

// CanGrant closes the escalation where a member with MANAGE_ROLES gives a
// role below theirs a permission they lack (ADMINISTRATOR above all) and
// assigns it to themselves.
func TestStanding_CanGrant(t *testing.T) {
	manager := role.Standing{IsMember: true, Permissions: role.PermManageRoles | role.PermSendMessages}
	cases := []struct {
		name string
		st   role.Standing
		p    role.Permission
		want bool
	}{
		{"nothing to grant", manager, 0, true},
		{"a permission they hold", manager, role.PermSendMessages, true},
		{"every permission they hold", manager, role.PermManageRoles | role.PermSendMessages, true},
		{"ADMINISTRATOR without holding it", manager, role.PermAdministrator, false},
		{"a mix with one they lack", manager, role.PermSendMessages | role.PermBanMembers, false},
		{"administrator grants anything", role.Standing{IsMember: true, Permissions: role.PermAdministrator}, role.AllPermissions, true},
		{"owner grants anything", role.Standing{IsMember: true, IsOwner: true}, role.AllPermissions, true},
	}
	for _, tc := range cases {
		if got := tc.st.CanGrant(tc.p); got != tc.want {
			t.Errorf("%s: CanGrant(%v) = %v, want %v", tc.name, role.PermissionNames(tc.p), got, tc.want)
		}
	}
}
