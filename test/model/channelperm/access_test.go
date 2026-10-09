package channelperm_test

import (
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model/channelperm"
	"github.com/discordia-grupo01/servers/internal/model/role"
)

// accessWorld is a server with a public and a #staff channel, @everyone and
// a Mod role, and four members: the owner, an administrator, a moderator
// (Mod) and a plain member. Everybody gets VIEW_CHANNELS from @everyone.
type accessWorld struct {
	public, staff  uuid.UUID
	everyone, mod  uuid.UUID
	snapshot       channelperm.AccessSnapshot
	everyoneMember channelperm.MemberAccess
}

func newAccessWorld() accessWorld {
	w := accessWorld{public: uuid.New(), staff: uuid.New(), everyone: uuid.New(), mod: uuid.New()}
	member := func(perms role.Permission, roles ...uuid.UUID) channelperm.MemberAccess {
		return channelperm.MemberAccess{
			Standing: role.Standing{IsMember: true, Permissions: perms | role.PermViewChannels},
			RoleIDs:  append([]uuid.UUID{w.everyone}, roles...),
		}
	}
	w.everyoneMember = member(0)
	w.snapshot = channelperm.AccessSnapshot{
		Members: map[string]channelperm.MemberAccess{
			"owner":  {Standing: role.Standing{IsMember: true, IsOwner: true}, RoleIDs: []uuid.UUID{w.everyone}},
			"admin":  member(role.PermAdministrator),
			"mod":    member(0, w.mod),
			"member": w.everyoneMember,
		},
		Channels:  []uuid.UUID{w.public, w.staff},
		Overrides: map[uuid.UUID]channelperm.ChannelOverrides{},
	}
	return w
}

// hideStaff makes #staff private: @everyone can't see it, Mod can.
func (w accessWorld) hideStaff() channelperm.AccessSnapshot {
	s := w.snapshot
	s.Overrides = map[uuid.UUID]channelperm.ChannelOverrides{
		w.staff: {
			Everyone: channelperm.Override{ChannelID: w.staff, RoleID: w.everyone, Deny: role.PermViewChannels},
			Roles: map[uuid.UUID]channelperm.Override{
				w.mod: {ChannelID: w.staff, RoleID: w.mod, Allow: role.PermViewChannels},
			},
		},
	}
	return s
}

func usersOf(set map[string]bool) []string {
	var users []string
	for _, u := range []string{"admin", "member", "mod", "owner"} {
		if set[u] {
			users = append(users, u)
		}
	}
	return users
}

func TestViewers_WithoutOverrides_EveryMemberSeesEveryChannel(t *testing.T) {
	w := newAccessWorld()
	viewers := w.snapshot.Viewers()

	for _, ch := range []uuid.UUID{w.public, w.staff} {
		if got := usersOf(viewers[ch]); !reflect.DeepEqual(got, []string{"admin", "member", "mod", "owner"}) {
			t.Errorf("viewers of %s = %v", ch, got)
		}
	}
}

func TestViewers_PrivateChannel_KeepsAdminsOwnerAndAllowedRoles(t *testing.T) {
	w := newAccessWorld()
	viewers := w.hideStaff().Viewers()

	if got := usersOf(viewers[w.staff]); !reflect.DeepEqual(got, []string{"admin", "mod", "owner"}) {
		t.Errorf("viewers of #staff = %v, want admin, mod, owner", got)
	}
	if got := usersOf(viewers[w.public]); len(got) != 4 {
		t.Errorf("viewers of the public channel = %v, want everybody", got)
	}
}

func TestViewers_MemberWithoutViewServerWide_SeesOnlyWhatAnOverrideGrants(t *testing.T) {
	w := newAccessWorld()
	s := w.snapshot
	s.Members = map[string]channelperm.MemberAccess{
		"member": {Standing: role.Standing{IsMember: true}, RoleIDs: []uuid.UUID{w.everyone, w.mod}},
	}
	s.Overrides = map[uuid.UUID]channelperm.ChannelOverrides{
		w.staff: {Roles: map[uuid.UUID]channelperm.Override{w.mod: {Allow: role.PermViewChannels}}},
	}
	viewers := s.Viewers()

	if viewers[w.public]["member"] {
		t.Error("member sees the public channel without VIEW_CHANNELS")
	}
	if !viewers[w.staff]["member"] {
		t.Error("member doesn't see #staff although Mod's override allows it")
	}
}

func TestViewers_OverrideOfARoleTheMemberLacks_DoesNotApply(t *testing.T) {
	w := newAccessWorld()
	s := w.snapshot
	s.Overrides = map[uuid.UUID]channelperm.ChannelOverrides{
		w.staff: {Roles: map[uuid.UUID]channelperm.Override{w.mod: {Deny: role.PermViewChannels}}},
	}
	viewers := s.Viewers()

	if !viewers[w.staff]["member"] {
		t.Error("Mod's deny hid #staff from a member without Mod")
	}
	if viewers[w.staff]["mod"] {
		t.Error("Mod's deny didn't hide #staff from mod")
	}
}

func TestViewers_EmptySnapshot(t *testing.T) {
	if got := (channelperm.AccessSnapshot{}).Viewers(); len(got) != 0 {
		t.Errorf("Viewers() = %v, want empty", got)
	}
}

func TestRevoked_HidingAChannel_ListsWhoLostIt(t *testing.T) {
	w := newAccessWorld()
	revoked := channelperm.Revoked(w.snapshot.Viewers(), w.hideStaff().Viewers())

	want := map[uuid.UUID][]string{w.staff: {"member"}}
	if !reflect.DeepEqual(revoked, want) {
		t.Errorf("Revoked() = %v, want %v", revoked, want)
	}
}

func TestRevoked_GainingAccess_IsNotReported(t *testing.T) {
	w := newAccessWorld()
	revoked := channelperm.Revoked(w.hideStaff().Viewers(), w.snapshot.Viewers())

	if len(revoked) != 0 {
		t.Errorf("Revoked() = %v, want empty", revoked)
	}
}

func TestRevoked_NothingChanged_IsEmpty(t *testing.T) {
	w := newAccessWorld()
	revoked := channelperm.Revoked(w.hideStaff().Viewers(), w.hideStaff().Viewers())

	if len(revoked) != 0 {
		t.Errorf("Revoked() = %v, want empty", revoked)
	}
}

func TestRevoked_MemberGone_LosesEveryChannelSorted(t *testing.T) {
	w := newAccessWorld()
	after := w.snapshot
	after.Members = map[string]channelperm.MemberAccess{"owner": w.snapshot.Members["owner"]}
	revoked := channelperm.Revoked(w.snapshot.Viewers(), after.Viewers())

	want := []string{"admin", "member", "mod"}
	for _, ch := range []uuid.UUID{w.public, w.staff} {
		if !reflect.DeepEqual(revoked[ch], want) {
			t.Errorf("revoked in %s = %v, want %v", ch, revoked[ch], want)
		}
	}
}

func TestRevoked_LosingAdministrator_HidesPrivateChannels(t *testing.T) {
	w := newAccessWorld()
	before := w.hideStaff()
	after := w.hideStaff()
	after.Members = map[string]channelperm.MemberAccess{}
	for id, m := range before.Members {
		after.Members[id] = m
	}
	after.Members["admin"] = w.everyoneMember
	revoked := channelperm.Revoked(before.Viewers(), after.Viewers())

	want := map[uuid.UUID][]string{w.staff: {"admin"}}
	if !reflect.DeepEqual(revoked, want) {
		t.Errorf("Revoked() = %v, want %v", revoked, want)
	}
}
