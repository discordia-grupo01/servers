package role_test

import (
	"testing"

	"github.com/discordia-grupo01/servers/internal/model/role"
)

func TestParsePermissions_KnownNames_ReturnsMask(t *testing.T) {
	mask, invalid, ok := role.ParsePermissions([]string{"SEND_MESSAGES", "MANAGE_CHANNELS"})
	if !ok {
		t.Fatalf("expected ok, got invalid=%q", invalid)
	}
	if mask&role.PermSendMessages == 0 || mask&role.PermManageChannels == 0 {
		t.Errorf("mask = %d, missing expected bits", mask)
	}
	if mask&role.PermManageRoles != 0 {
		t.Errorf("mask = %d, has an unrequested bit set", mask)
	}
}

func TestParsePermissions_UnknownName_ReturnsInvalid(t *testing.T) {
	_, invalid, ok := role.ParsePermissions([]string{"SEND_MESSAGES", "FLY_TO_MOON"})
	if ok {
		t.Fatal("expected ok=false for an unknown permission")
	}
	if invalid != "FLY_TO_MOON" {
		t.Errorf("invalid = %q, want %q", invalid, "FLY_TO_MOON")
	}
}

func TestParsePermissions_Empty_ReturnsZeroMask(t *testing.T) {
	mask, _, ok := role.ParsePermissions(nil)
	if !ok || mask != 0 {
		t.Errorf("mask = %d ok=%v, want 0/true", mask, ok)
	}
}

func TestPermissionNames_RoundTripsWithParsePermissions(t *testing.T) {
	names := []string{"BAN_MEMBERS", "KICK_MEMBERS", "VIEW_CHANNELS"}
	mask, _, ok := role.ParsePermissions(names)
	if !ok {
		t.Fatal("unexpected invalid permission")
	}
	got := role.PermissionNames(mask)
	if len(got) != len(names) {
		t.Fatalf("got %v, want a permutation of %v", got, names)
	}
	want := map[string]bool{"BAN_MEMBERS": true, "KICK_MEMBERS": true, "VIEW_CHANNELS": true}
	for _, n := range got {
		if !want[n] {
			t.Errorf("unexpected name %q in %v", n, got)
		}
	}
}

func TestPermissionNames_ZeroMask_ReturnsEmpty(t *testing.T) {
	got := role.PermissionNames(0)
	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

func TestManageMessages_IsInTheCatalogWithoutShiftingExistingBits(t *testing.T) {
	mask, invalid, ok := role.ParsePermissions([]string{"MANAGE_MESSAGES"})
	if !ok {
		t.Fatalf("MANAGE_MESSAGES should be in the catalog, invalid=%q", invalid)
	}
	if mask != role.PermManageMessages {
		t.Errorf("mask = %d, want %d", mask, role.PermManageMessages)
	}
	if role.PermCreateInvite != 256 || role.PermManageMessages != 512 {
		t.Errorf("bits shifted: CREATE_INVITE=%d MANAGE_MESSAGES=%d, want 256 and 512",
			role.PermCreateInvite, role.PermManageMessages)
	}
	if role.DefaultPermissions&role.PermManageMessages != 0 {
		t.Error("a new role must not start with MANAGE_MESSAGES")
	}
}

func TestStanding_ManageMessages_ImplicitForOwnerAndAdministratorsOnly(t *testing.T) {
	cases := map[string]struct {
		st   role.Standing
		want bool
	}{
		"owner":                   {role.Standing{IsMember: true, IsOwner: true}, true},
		"administrator":           {role.Standing{IsMember: true, Permissions: role.PermAdministrator}, true},
		"member with the grant":   {role.Standing{IsMember: true, Permissions: role.PermManageMessages}, true},
		"member with default set": {role.Standing{IsMember: true, Permissions: role.DefaultPermissions}, false},
	}
	for name, tc := range cases {
		if got := tc.st.Has(role.PermManageMessages); got != tc.want {
			t.Errorf("%s: Has(MANAGE_MESSAGES) = %v, want %v", name, got, tc.want)
		}
	}
}
