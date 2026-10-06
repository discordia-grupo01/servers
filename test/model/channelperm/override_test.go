package channelperm_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/channelperm"
	"github.com/discordia-grupo01/servers/internal/model/role"
)

const (
	view   = role.PermViewChannels
	send   = role.PermSendMessages
	manage = role.PermManageMessages
)

func rank(p int) *int { return &p }

func member(perms role.Permission) role.Standing {
	return role.Standing{IsMember: true, Permissions: perms, TopPosition: rank(2)}
}

func ov(allow, deny role.Permission) channelperm.Override {
	return channelperm.Override{Allow: allow, Deny: deny}
}

var none channelperm.Override

// #57 regla 10: cada caso del cálculo de permisos efectivos.
func TestEffective(t *testing.T) {
	cases := map[string]struct {
		st       role.Standing
		everyone channelperm.Override
		roles    []channelperm.Override
		want     role.Permission
	}{
		"non-member gets nothing, overrides or not": {
			st: role.Standing{Permissions: view | send}, everyone: ov(view|send, 0), want: 0,
		},
		"no overrides: the server-wide permissions": {
			st: member(view | send), want: view | send,
		},
		"owner ignores every deny": {
			st:       role.Standing{IsMember: true, IsOwner: true},
			everyone: ov(0, view|send), roles: []channelperm.Override{ov(0, manage)},
			want: role.AllPermissions,
		},
		"administrator ignores every deny": {
			st:       member(role.PermAdministrator),
			everyone: ov(0, view|send),
			want:     role.AllPermissions,
		},
		"@everyone deny removes the permission (read-only channel)": {
			st: member(view | send), everyone: ov(0, send), want: view,
		},
		"@everyone allow adds a permission": {
			st: member(view), everyone: ov(manage, 0), want: view | manage,
		},
		"role deny removes the permission": {
			st: member(view | send), roles: []channelperm.Override{ov(0, send)}, want: view,
		},
		"role allow adds a permission only the override grants": {
			st: member(view), roles: []channelperm.Override{ov(send, 0)}, want: view | send,
		},
		"role allow beats @everyone deny (moderators write in #anuncios)": {
			st:       member(view | send),
			everyone: ov(0, send), roles: []channelperm.Override{ov(send, 0)},
			want: view | send,
		},
		"role deny beats @everyone allow": {
			st:       member(view),
			everyone: ov(send, 0), roles: []channelperm.Override{ov(0, send)},
			want: view,
		},
		"within the roles layer, allow wins over deny": {
			st:    member(view | send),
			roles: []channelperm.Override{ov(0, send), ov(send, 0)},
			want:  view | send,
		},
		"several roles: denies and allows are unions": {
			st:    member(view | send),
			roles: []channelperm.Override{ov(0, send), ov(manage, 0), ov(0, view)},
			want:  manage,
		},
		"private channel: hidden from @everyone, visible to a role": {
			st:       member(view | send),
			everyone: ov(0, view), roles: []channelperm.Override{ov(view, 0)},
			want: view | send,
		},
		"private channel: a plain member doesn't see it": {
			st: member(view | send), everyone: ov(0, view), want: send,
		},
		"server-level permissions are untouched": {
			st:       member(view | role.PermKickMembers | role.PermCreateInvite),
			everyone: ov(0, view),
			want:     role.PermKickMembers | role.PermCreateInvite,
		},
	}
	for name, tc := range cases {
		if got := channelperm.Effective(tc.st, tc.everyone, tc.roles); got != tc.want {
			t.Errorf("%s: got %v, want %v", name, role.PermissionNames(got), role.PermissionNames(tc.want))
		}
	}
}

func TestCanView(t *testing.T) {
	if !channelperm.CanView(member(view), none, nil) {
		t.Error("a member with VIEW_CHANNELS and no overrides should see the channel")
	}
	if channelperm.CanView(member(view|send), ov(0, view), nil) {
		t.Error("@everyone deny VIEW_CHANNELS should hide the channel")
	}
	if !channelperm.CanView(role.Standing{IsMember: true, IsOwner: true}, ov(0, view), nil) {
		t.Error("the owner should see every channel")
	}
	if channelperm.CanView(role.Standing{}, none, nil) {
		t.Error("a non-member should see nothing")
	}
}

func TestParseMasks_Valid(t *testing.T) {
	allow, deny, err := channelperm.ParseMasks(
		[]string{"SEND_MESSAGES", "SEND_MESSAGES"}, // repeated: deduplicated
		[]string{"VIEW_CHANNELS", "MANAGE_MESSAGES"},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allow != send || deny != view|manage {
		t.Errorf("allow=%v deny=%v", role.PermissionNames(allow), role.PermissionNames(deny))
	}

	allow, deny, err = channelperm.ParseMasks(nil, nil)
	if err != nil || allow != 0 || deny != 0 {
		t.Errorf("empty lists: allow=%d deny=%d err=%v", allow, deny, err)
	}
}

func TestParseMasks_UnknownName(t *testing.T) {
	for _, lists := range [][2][]string{{{"FLY"}, nil}, {nil, {"SEND_MESSAGES", "FLY"}}} {
		_, _, err := channelperm.ParseMasks(lists[0], lists[1])
		var ipe *model.InvalidPermissionError
		if !errors.As(err, &ipe) || ipe.Value != "FLY" || !errors.Is(err, model.ErrInvalidPermission) {
			t.Errorf("%v: err = %v", lists, err)
		}
	}
}

func TestParseMasks_ServerLevelPermissionsAreNotOverridable(t *testing.T) {
	_, _, err := channelperm.ParseMasks([]string{"ADMINISTRATOR", "SEND_MESSAGES"}, []string{"KICK_MEMBERS"})
	var e *model.PermissionNotOverridableError
	if !errors.As(err, &e) || !errors.Is(err, model.ErrPermissionNotOverridable) {
		t.Fatalf("err = %v", err)
	}
	if want := []string{"ADMINISTRATOR", "KICK_MEMBERS"}; !reflect.DeepEqual(e.Permissions, want) {
		t.Errorf("permissions = %v, want %v", e.Permissions, want)
	}
}

func TestParseMasks_AllowAndDenyTheSamePermission(t *testing.T) {
	_, _, err := channelperm.ParseMasks([]string{"SEND_MESSAGES", "VIEW_CHANNELS"}, []string{"SEND_MESSAGES"})
	var e *model.PermissionConflictError
	if !errors.As(err, &e) || !errors.Is(err, model.ErrPermissionConflict) {
		t.Fatalf("err = %v", err)
	}
	if want := []string{"SEND_MESSAGES"}; !reflect.DeepEqual(e.Permissions, want) {
		t.Errorf("permissions = %v, want %v", e.Permissions, want)
	}
}

func TestOverride_IsEmpty(t *testing.T) {
	if !none.IsEmpty() || ov(send, 0).IsEmpty() || ov(0, view).IsEmpty() {
		t.Error("IsEmpty should be true only with both masks at zero")
	}
}
