package boost_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/emoji"
)

func TestBoostLevelFor(t *testing.T) {
	for count, want := range map[int]int{0: 0, 4: 0, 5: 1, 14: 1, 15: 2, 29: 2, 30: 3, 500: 3} {
		if got := model.BoostLevelFor(count); got != want {
			t.Errorf("BoostLevelFor(%d) = %d, want %d", count, got, want)
		}
	}
}

func TestBoostPerks_AreCumulative(t *testing.T) {
	tests := map[int][]string{
		0: {},
		1: {model.PerkExtraEmojiSlots, model.PerkAnimatedIcon},
		2: {model.PerkExtraEmojiSlots, model.PerkAnimatedIcon, model.PerkCustomBanner, model.PerkLargerAttachments},
		3: {model.PerkExtraEmojiSlots, model.PerkAnimatedIcon, model.PerkCustomBanner, model.PerkLargerAttachments, model.PerkVanityURL, model.PerkBoostedBadge},
	}
	for level, want := range tests {
		if got := model.BoostPerks(level); !slices.Equal(got, want) {
			t.Errorf("BoostPerks(%d) = %v, want %v", level, got, want)
		}
	}
}

func TestSummarizeBoosts_SaysHowManyAreMissing(t *testing.T) {
	got := model.SummarizeBoosts(7)
	if got.Level != 1 || got.Next == nil || got.Next.Level != 2 || got.Next.BoostsRequired != 15 || got.Next.BoostsMissing != 8 {
		t.Errorf("summary = %+v, next = %+v", got, got.Next)
	}
	if last := model.SummarizeBoosts(30); last.Level != 3 || last.Next != nil {
		t.Errorf("last level = %+v", last)
	}
}

func TestEmojiSlots(t *testing.T) {
	for level, want := range map[int]int{0: 50, 1: 70, 2: 70, 3: 70} {
		if got := emoji.Slots(level); got != want {
			t.Errorf("Slots(%d) = %d, want %d", level, got, want)
		}
	}
}

func TestValidateVanityCode(t *testing.T) {
	for _, code := range []string{"abc", "mi-servidor", "a1b", "abcdefghijklmnopqrstuvwxyz012345"} {
		if err := model.ValidateVanityCode(code); err != nil {
			t.Errorf("%q: %v", code, err)
		}
	}
	for _, code := range []string{"", "ab", "-abc", "abc-", "Abc", "con espacio", "abcdefghijklmnopqrstuvwxyz0123456"} {
		if err := model.ValidateVanityCode(code); !errors.Is(err, model.ErrVanityCodeInvalid) {
			t.Errorf("%q: err = %v", code, err)
		}
	}
}
