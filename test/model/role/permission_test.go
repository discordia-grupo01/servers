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
