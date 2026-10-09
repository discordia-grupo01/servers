package channelperm

import (
	"sort"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model/role"
)

// AccessSnapshot is what deciding who sees which channel of a server takes,
// read at one point in time: its members, their roles and the overrides of its
// channels. Comparing two of them, taken before and after a write, tells who
// lost access to what (Revoked).
type AccessSnapshot struct {
	// Members maps each member to their standing (IsMember set, IsOwner for
	// the owner, Permissions the union of their roles) and their role ids,
	// @everyone included.
	Members map[string]MemberAccess
	// Channels are the channels taken into account.
	Channels []uuid.UUID
	// Overrides maps a channel to its overrides. Channels without any may be
	// absent.
	Overrides map[uuid.UUID]ChannelOverrides
}

// MemberAccess is one member of an AccessSnapshot.
type MemberAccess struct {
	Standing role.Standing
	RoleIDs  []uuid.UUID
}

// ChannelOverrides are every override of one channel: the one of @everyone
// (the zero Override if it has none) and the rest by role id.
type ChannelOverrides struct {
	Everyone Override
	Roles    map[uuid.UUID]Override
}

// layersFor keeps the overrides that apply to a member holding roleIDs, split
// the way Effective takes them.
func (c ChannelOverrides) layersFor(roleIDs []uuid.UUID) Layers {
	l := Layers{Everyone: c.Everyone}
	for _, id := range roleIDs {
		if o, ok := c.Roles[id]; ok {
			l.Roles = append(l.Roles, o)
		}
	}
	return l
}

// Viewers returns, for each channel of the snapshot, the members who can see
// it (CanView): administrators and the owner always, everyone else depending
// on their roles and the channel's overrides.
func (s AccessSnapshot) Viewers() map[uuid.UUID]map[string]bool {
	viewers := make(map[uuid.UUID]map[string]bool, len(s.Channels))
	for _, ch := range s.Channels {
		overrides := s.Overrides[ch]
		set := make(map[string]bool)
		for userID, m := range s.Members {
			l := overrides.layersFor(m.RoleIDs)
			if CanView(m.Standing, l.Everyone, l.Roles) {
				set[userID] = true
			}
		}
		viewers[ch] = set
	}
	return viewers
}

// Revoked returns, for each channel, the users who could see it before and
// can't after, sorted. Channels where nobody lost access are absent, so an
// empty result means there's nothing to publish. Gaining access isn't
// reported.
func Revoked(before, after map[uuid.UUID]map[string]bool) map[uuid.UUID][]string {
	revoked := make(map[uuid.UUID][]string)
	for ch, was := range before {
		now := after[ch]
		var lost []string
		for userID := range was {
			if !now[userID] {
				lost = append(lost, userID)
			}
		}
		if len(lost) > 0 {
			sort.Strings(lost)
			revoked[ch] = lost
		}
	}
	return revoked
}
