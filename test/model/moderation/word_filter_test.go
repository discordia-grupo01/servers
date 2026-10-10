package moderation_test

import (
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/discordia-grupo01/servers/internal/model/moderation"
)

func TestNormalizeBannedWords_TrimsLowercasesAndDropsRepeated(t *testing.T) {
	got, err := moderation.NormalizeBannedWords([]string{"  Tonto ", "NO   sirve", "tonto", "Pésimo"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"tonto", "no sirve", "pésimo"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestNormalizeBannedWords_EmptyListIsNoFilter(t *testing.T) {
	got, err := moderation.NormalizeBannedWords([]string{})
	if err != nil || got == nil || len(got) != 0 {
		t.Errorf("got %q, %v; want an empty non-nil list", got, err)
	}
}

func TestNormalizeBannedWords_RejectsInvalidEntries(t *testing.T) {
	cases := map[string]struct {
		words []string
		want  error
	}{
		"blank":         {[]string{"ok", "   "}, moderation.ErrBannedWordEmpty},
		"too long":      {[]string{strings.Repeat("a", moderation.BannedWordMaxLen+1)}, moderation.ErrBannedWordTooLong},
		"control chars": {[]string{"ho\x00la"}, moderation.ErrBannedWordInvalidChars},
		"more than 100": {uniqueWords(moderation.BannedWordsMaxSize + 1), moderation.ErrTooManyBannedWords},
	}
	for name, tc := range cases {
		if _, err := moderation.NormalizeBannedWords(tc.words); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", name, err, tc.want)
		}
	}
}

func TestNormalizeBannedWords_RepeatedOnesDontCountTowardsTheLimit(t *testing.T) {
	words := append(uniqueWords(moderation.BannedWordsMaxSize), "palabra-0", "PALABRA-1")
	got, err := moderation.NormalizeBannedWords(words)
	if err != nil || len(got) != moderation.BannedWordsMaxSize {
		t.Errorf("len = %d, err = %v", len(got), err)
	}
}

func TestNormalizeBannedWords_AcceptsExactlyTheMaxLength(t *testing.T) {
	word := strings.Repeat("ñ", moderation.BannedWordMaxLen)
	if _, err := moderation.NormalizeBannedWords([]string{word}); err != nil {
		t.Errorf("a %d-rune word should be accepted: %v", moderation.BannedWordMaxLen, err)
	}
}

func uniqueWords(n int) []string {
	words := make([]string, n)
	for i := range words {
		words[i] = "palabra-" + strconv.Itoa(i)
	}
	return words
}
