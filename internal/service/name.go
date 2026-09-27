package service

import (
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

func normalizeName(s string) string {
	return norm.NFC.String(strings.TrimSpace(s))
}

var accentStripper = transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)

func NameKey(s string) string {
	stripped, _, err := transform.String(accentStripper, s)
	if err != nil {
		stripped = s
	}
	return strings.ToLower(strings.Join(strings.Fields(stripped), " "))
}
