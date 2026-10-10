package moderation

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	BannedWordMaxLen   = 50
	BannedWordsMaxSize = 100
)

var (
	ErrBannedWordEmpty        = errors.New("banned words can't be empty")
	ErrBannedWordTooLong      = errors.New("banned word is too long")
	ErrBannedWordInvalidChars = errors.New("banned word contains control characters")
	ErrTooManyBannedWords     = errors.New("too many banned words")
)

// NormalizeBannedWords trims, lowercases and collapses the inner whitespace of
// every word or phrase, and drops repeated ones keeping the first. Accents are
// kept as typed: messaging ignores them when matching.
func NormalizeBannedWords(raw []string) ([]string, error) {
	words := make([]string, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	for _, w := range raw {
		word := strings.ToLower(strings.Join(strings.Fields(w), " "))
		if word == "" {
			return nil, ErrBannedWordEmpty
		}
		if utf8.RuneCountInString(word) > BannedWordMaxLen {
			return nil, ErrBannedWordTooLong
		}
		if strings.IndexFunc(word, unicode.IsControl) >= 0 {
			return nil, ErrBannedWordInvalidChars
		}
		if seen[word] {
			continue
		}
		seen[word] = true
		words = append(words, word)
	}
	if len(words) > BannedWordsMaxSize {
		return nil, ErrTooManyBannedWords
	}
	return words, nil
}
