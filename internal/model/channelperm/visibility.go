package channelperm

import (
	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/role"
)

// Layers are the overrides of one channel that apply to one user, split the
// way Effective takes them. A channel with no override for any of the user's
// roles has zero Layers.
type Layers struct {
	Everyone Override
	Roles    []Override
}

// HiddenChannels returns the ids of the channels st can't see (no
// VIEW_CHANNELS after their overrides, #57). layers maps a channel id to the
// overrides of that channel for the user's roles; channels without overrides
// may be absent. The owner and administrators see everything, so for them
// it's always empty.
func HiddenChannels(st role.Standing, channels []model.Channel, layers map[uuid.UUID]Layers) map[uuid.UUID]bool {
	hidden := make(map[uuid.UUID]bool)
	if st.IsAdmin() {
		return hidden
	}
	for _, ch := range channels {
		l := layers[ch.ID]
		if !CanView(st, l.Everyone, l.Roles) {
			hidden[ch.ID] = true
		}
	}
	return hidden
}

// VisibleChannels keeps the channels st can see, in their original order. It
// never returns nil, so an empty result serializes as [].
func VisibleChannels(st role.Standing, channels []model.Channel, layers map[uuid.UUID]Layers) []model.Channel {
	hidden := HiddenChannels(st, channels, layers)
	visible := make([]model.Channel, 0, len(channels))
	for _, ch := range channels {
		if !hidden[ch.ID] {
			visible = append(visible, ch)
		}
	}
	return visible
}

// MergeVisibleOrder reorders a category whose actor can't see every channel,
// Discord-style (#57): current is the category's full order, requested the
// new order of the channels the actor sees. requested must be exactly those
// visible channels -- no hidden one, none missing, no repeats -- or ok is
// false. The visible channels take the slots they already occupied, in the
// requested order; hidden channels keep theirs. With nothing hidden, the
// result is requested itself.
//
// E.g. current [reglas, staff, charla, memes] with staff hidden and requested
// [memes, reglas, charla] gives [memes, staff, reglas, charla].
func MergeVisibleOrder(current []uuid.UUID, hidden map[uuid.UUID]bool, requested []uuid.UUID) (merged []uuid.UUID, ok bool) {
	visible := make(map[uuid.UUID]bool, len(current))
	for _, id := range current {
		if !hidden[id] {
			visible[id] = true
		}
	}
	if len(requested) != len(visible) {
		return nil, false
	}
	seen := make(map[uuid.UUID]bool, len(requested))
	for _, id := range requested {
		if !visible[id] || seen[id] {
			return nil, false
		}
		seen[id] = true
	}

	merged = make([]uuid.UUID, len(current))
	next := 0
	for i, id := range current {
		if hidden[id] {
			merged[i] = id
			continue
		}
		merged[i] = requested[next]
		next++
	}
	return merged, true
}
