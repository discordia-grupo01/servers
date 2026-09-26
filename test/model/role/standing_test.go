package role_test

import (
	"testing"

	"github.com/discordia-grupo01/servers/internal/model/role"
)

func TestAdministratorIsInCatalog(t *testing.T) {
	mask, _, ok := role.ParsePermissions([]string{"ADMINISTRATOR"})
	if !ok || mask != role.PermAdministrator {
		t.Fatalf("ParsePermissions(ADMINISTRATOR) = %v, %v", mask, ok)
	}
	names := role.PermissionNames(role.PermAdministrator)
	if len(names) != 1 || names[0] != "ADMINISTRATOR" {
		t.Errorf("PermissionNames = %v", names)
	}
}

func TestStanding_IsAdmin(t *testing.T) {
	cases := []struct {
		name string
		st   role.Standing
		want bool
	}{
		{"owner without roles", role.Standing{IsMember: true, IsOwner: true}, true},
		{"member with ADMINISTRATOR", role.Standing{IsMember: true, Permissions: role.PermAdministrator}, true},
		{"member with every granular perm", role.Standing{IsMember: true, Permissions: role.PermManageRoles | role.PermManageChannels | role.PermManageServer | role.PermBanMembers}, false},
		{"member without roles", role.Standing{IsMember: true}, false},
		{"non member", role.Standing{}, false},
	}
	for _, tc := range cases {
		if got := tc.st.IsAdmin(); got != tc.want {
			t.Errorf("%s: IsAdmin() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestStanding_Has(t *testing.T) {
	cases := []struct {
		name string
		st   role.Standing
		want bool
	}{
		{"owner implicitly has everything", role.Standing{IsOwner: true}, true},
		{"administrator implicitly has everything", role.Standing{IsMember: true, Permissions: role.PermAdministrator}, true},
		{"explicit permission", role.Standing{IsMember: true, Permissions: role.PermBanMembers | role.PermSendMessages}, true},
		{"missing permission", role.Standing{IsMember: true, Permissions: role.PermKickMembers}, false},
		{"no permissions", role.Standing{IsMember: true}, false},
	}
	for _, tc := range cases {
		if got := tc.st.Has(role.PermBanMembers); got != tc.want {
			t.Errorf("%s: Has(BAN_MEMBERS) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestRemovesOwnAdmin(t *testing.T) {
	admin := role.Standing{IsMember: true, Permissions: role.PermAdministrator}
	owner := role.Standing{IsMember: true, IsOwner: true}
	withAdmin := role.PermAdministrator | role.PermSendMessages

	cases := []struct {
		name          string
		actor         role.Standing
		hasRole       bool
		before, after role.Permission
		want          bool
	}{
		{"admin drops ADMINISTRATOR from own role", admin, true, withAdmin, role.PermSendMessages, true},
		{"admin deletes/unassigns own admin role", admin, true, withAdmin, 0, true},
		{"admin drops it from a role they don't hold", admin, false, withAdmin, role.PermSendMessages, false},
		{"admin keeps ADMINISTRATOR on own role", admin, true, withAdmin, role.PermAdministrator, false},
		{"own role never had ADMINISTRATOR", admin, true, role.PermSendMessages, 0, false},
		{"owner is exempt", owner, true, withAdmin, 0, false},
	}
	for _, tc := range cases {
		if got := role.RemovesOwnAdmin(tc.actor, tc.hasRole, tc.before, tc.after); got != tc.want {
			t.Errorf("%s: RemovesOwnAdmin = %v, want %v", tc.name, got, tc.want)
		}
	}
}
