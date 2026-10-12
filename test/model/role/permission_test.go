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

// #57: the permissions that can be overridden per channel. The number is
// repeated in the overrides_channel_scoped CHECK of migration 0025, so this
// test fails if one changes without the other.
func TestChannelScopedPermissions_MatchesTheMigrationCheck(t *testing.T) {
	want := role.PermViewChannels | role.PermSendMessages | role.PermManageMessages | role.PermAddReactions | role.PermConnect
	if role.ChannelScopedPermissions != want || role.ChannelScopedPermissions != 6659 {
		t.Errorf("ChannelScopedPermissions = %d (%v), want 6659 (VIEW_CHANNELS, SEND_MESSAGES, MANAGE_MESSAGES, ADD_REACTIONS, CONNECT)",
			role.ChannelScopedPermissions, role.PermissionNames(role.ChannelScopedPermissions))
	}
}

func TestConnect_IsInTheCatalogWithoutShiftingExistingBits(t *testing.T) {
	mask, invalid, ok := role.ParsePermissions([]string{"CONNECT"})
	if !ok {
		t.Fatalf("CONNECT should be in the catalog, invalid=%q", invalid)
	}
	if mask != role.PermConnect || role.PermConnect != 4096 {
		t.Errorf("mask = %d, PermConnect = %d, want 4096", mask, role.PermConnect)
	}
	if role.PermAddReactions != 2048 {
		t.Errorf("bits shifted: ADD_REACTIONS=%d, want 2048", role.PermAddReactions)
	}
	if role.AllPermissions != 8191 {
		t.Errorf("AllPermissions = %d, want 8191", role.AllPermissions)
	}
	if role.DefaultPermissions&role.PermConnect == 0 {
		t.Error("a new role must start with CONNECT")
	}
	if role.ChannelScopedPermissions&role.PermConnect == 0 {
		t.Error("CONNECT must be overridable per channel")
	}
	if got := role.PermissionNames(role.PermConnect); len(got) != 1 || got[0] != "CONNECT" {
		t.Errorf("PermissionNames = %v, want [CONNECT]", got)
	}
}

func TestAddReactions_IsInTheCatalogWithoutShiftingExistingBits(t *testing.T) {
	mask, invalid, ok := role.ParsePermissions([]string{"ADD_REACTIONS"})
	if !ok {
		t.Fatalf("ADD_REACTIONS should be in the catalog, invalid=%q", invalid)
	}
	if mask != role.PermAddReactions || role.PermAddReactions != 2048 {
		t.Errorf("mask = %d, PermAddReactions = %d, want 2048", mask, role.PermAddReactions)
	}
	if role.PermMentionEveryone != 1024 {
		t.Errorf("bits shifted: MENTION_EVERYONE=%d, want 1024", role.PermMentionEveryone)
	}
	if role.DefaultPermissions&role.PermAddReactions == 0 {
		t.Error("a new role must start with ADD_REACTIONS")
	}
}

func TestMentionEveryone_IsInTheCatalogWithoutShiftingExistingBits(t *testing.T) {
	mask, invalid, ok := role.ParsePermissions([]string{"MENTION_EVERYONE"})
	if !ok {
		t.Fatalf("MENTION_EVERYONE should be in the catalog, invalid=%q", invalid)
	}
	if mask != role.PermMentionEveryone {
		t.Errorf("mask = %d, want %d", mask, role.PermMentionEveryone)
	}
	if role.PermManageMessages != 512 || role.PermMentionEveryone != 1024 {
		t.Errorf("bits shifted: MANAGE_MESSAGES=%d MENTION_EVERYONE=%d, want 512 and 1024",
			role.PermManageMessages, role.PermMentionEveryone)
	}
	if role.DefaultPermissions&role.PermMentionEveryone != 0 {
		t.Error("a new role must not start with MENTION_EVERYONE")
	}
	if got := role.PermissionNames(role.PermMentionEveryone); len(got) != 1 || got[0] != "MENTION_EVERYONE" {
		t.Errorf("PermissionNames = %v, want [MENTION_EVERYONE]", got)
	}
}

func TestStanding_MentionEveryone_ImplicitForOwnerAndAdministratorsOnly(t *testing.T) {
	cases := map[string]struct {
		st   role.Standing
		want bool
	}{
		"owner":                   {role.Standing{IsMember: true, IsOwner: true}, true},
		"administrator":           {role.Standing{IsMember: true, Permissions: role.PermAdministrator}, true},
		"member with the grant":   {role.Standing{IsMember: true, Permissions: role.PermMentionEveryone}, true},
		"member with default set": {role.Standing{IsMember: true, Permissions: role.DefaultPermissions}, false},
	}
	for name, tc := range cases {
		if got := tc.st.Has(role.PermMentionEveryone); got != tc.want {
			t.Errorf("%s: Has(MENTION_EVERYONE) = %v, want %v", name, got, tc.want)
		}
	}
}
