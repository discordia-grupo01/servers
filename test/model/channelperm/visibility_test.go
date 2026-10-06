package channelperm_test

import (
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/channelperm"
	"github.com/discordia-grupo01/servers/internal/model/role"
)

// #staff hidden from @everyone and visible to a moderator role; #general
// without overrides.
func staffFixture() (general, staff model.Channel, everyoneLayer, modLayer map[uuid.UUID]channelperm.Layers) {
	general = model.Channel{ID: uuid.New(), Name: "general"}
	staff = model.Channel{ID: uuid.New(), Name: "staff"}
	everyoneLayer = map[uuid.UUID]channelperm.Layers{staff.ID: {Everyone: ov(0, view)}}
	modLayer = map[uuid.UUID]channelperm.Layers{staff.ID: {Everyone: ov(0, view), Roles: []channelperm.Override{ov(view, 0)}}}
	return general, staff, everyoneLayer, modLayer
}

func ids(chs []model.Channel) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(chs))
	for _, ch := range chs {
		out = append(out, ch.ID)
	}
	return out
}

func TestVisibleChannels(t *testing.T) {
	general, staff, plain, mod := staffFixture()
	channels := []model.Channel{general, staff}

	cases := map[string]struct {
		st     role.Standing
		layers map[uuid.UUID]channelperm.Layers
		want   []uuid.UUID
	}{
		"plain member: #staff is hidden": {member(view | send), plain, []uuid.UUID{general.ID}},
		"moderator: sees both":           {member(view | send), mod, []uuid.UUID{general.ID, staff.ID}},
		"owner: sees everything":         {role.Standing{IsMember: true, IsOwner: true}, plain, []uuid.UUID{general.ID, staff.ID}},
		"administrator: sees everything": {member(role.PermAdministrator), plain, []uuid.UUID{general.ID, staff.ID}},
		"no overrides: same as before":   {member(view), nil, []uuid.UUID{general.ID, staff.ID}},
		"no VIEW_CHANNELS at all: none":  {member(send), nil, []uuid.UUID{}},
		"non-member: none":               {role.Standing{Permissions: view}, nil, []uuid.UUID{}},
	}
	for name, tc := range cases {
		got := channelperm.VisibleChannels(tc.st, channels, tc.layers)
		if got == nil || !reflect.DeepEqual(ids(got), tc.want) {
			t.Errorf("%s: got %v, want %v", name, ids(got), tc.want)
		}
	}
}

func TestHiddenChannels(t *testing.T) {
	general, staff, plain, _ := staffFixture()
	hidden := channelperm.HiddenChannels(member(view), []model.Channel{general, staff}, plain)
	if !hidden[staff.ID] || hidden[general.ID] || len(hidden) != 1 {
		t.Errorf("hidden = %v, want only #staff", hidden)
	}
	if h := channelperm.HiddenChannels(member(role.PermAdministrator), []model.Channel{general, staff}, plain); len(h) != 0 {
		t.Errorf("administrator: hidden = %v, want none", h)
	}
}

// The example of the plan: [reglas, staff*, charla, memes] with staff hidden
// and [memes, reglas, charla] requested.
func TestMergeVisibleOrder_HiddenChannelsKeepTheirPlace(t *testing.T) {
	reglas, staff, charla, memes := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	current := []uuid.UUID{reglas, staff, charla, memes}
	hidden := map[uuid.UUID]bool{staff: true}

	got, ok := channelperm.MergeVisibleOrder(current, hidden, []uuid.UUID{memes, reglas, charla})
	if !ok || !reflect.DeepEqual(got, []uuid.UUID{memes, staff, reglas, charla}) {
		t.Fatalf("got %v ok=%v", got, ok)
	}
}

func TestMergeVisibleOrder_NothingHiddenIsTheRequestedOrder(t *testing.T) {
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	got, ok := channelperm.MergeVisibleOrder([]uuid.UUID{a, b, c}, nil, []uuid.UUID{c, a, b})
	if !ok || !reflect.DeepEqual(got, []uuid.UUID{c, a, b}) {
		t.Fatalf("got %v ok=%v", got, ok)
	}
}

func TestMergeVisibleOrder_EverythingHidden_OnlyTheEmptyListIsValid(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	hidden := map[uuid.UUID]bool{a: true, b: true}
	got, ok := channelperm.MergeVisibleOrder([]uuid.UUID{a, b}, hidden, nil)
	if !ok || !reflect.DeepEqual(got, []uuid.UUID{a, b}) {
		t.Errorf("empty request: got %v ok=%v", got, ok)
	}
	if _, ok := channelperm.MergeVisibleOrder([]uuid.UUID{a, b}, hidden, []uuid.UUID{a}); ok {
		t.Error("a hidden id was accepted")
	}
}

func TestMergeVisibleOrder_InvalidRequests(t *testing.T) {
	a, hiddenID, c, stranger := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	current := []uuid.UUID{a, hiddenID, c}
	hidden := map[uuid.UUID]bool{hiddenID: true}

	cases := map[string][]uuid.UUID{
		"a visible channel is missing":    {a},
		"includes the hidden channel":     {a, hiddenID, c},
		"hidden instead of a visible one": {a, hiddenID},
		"id from elsewhere":               {a, stranger},
		"repeated id":                     {a, a},
	}
	for name, requested := range cases {
		if got, ok := channelperm.MergeVisibleOrder(current, hidden, requested); ok {
			t.Errorf("%s: accepted, got %v", name, got)
		}
	}
}
