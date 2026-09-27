package moderation_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/discordia-grupo01/servers/internal/model/moderation"
)

func str(s string) *string { return &s }

func TestNormalizeReason(t *testing.T) {
	cases := []struct {
		name    string
		in      *string
		want    *string
		wantErr error
	}{
		{"missing", nil, nil, nil},
		{"blank is no reason", str("   \n "), nil, nil},
		{"trimmed", str("  spam  "), str("spam"), nil},
		{"multi-line is fine", str("spam\nand flood\ttoo"), str("spam\nand flood\ttoo"), nil},
		{"exactly the max", str(strings.Repeat("a", moderation.ReasonMaxLen)), str(strings.Repeat("a", moderation.ReasonMaxLen)), nil},
		{"counted in characters, not bytes", str(strings.Repeat("ñ", moderation.ReasonMaxLen)), str(strings.Repeat("ñ", moderation.ReasonMaxLen)), nil},
		{"too long", str(strings.Repeat("a", moderation.ReasonMaxLen+1)), nil, moderation.ErrReasonTooLong},
		{"control character", str("spam\x00"), nil, moderation.ErrReasonInvalidChars},
		{"escape character", str("\x1b[31mred"), nil, moderation.ErrReasonInvalidChars},
	}
	for _, tc := range cases {
		got, err := moderation.NormalizeReason(tc.in)
		if !errors.Is(err, tc.wantErr) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.wantErr)
			continue
		}
		switch {
		case tc.want == nil && got != nil:
			t.Errorf("%s: got %q, want nil", tc.name, *got)
		case tc.want != nil && (got == nil || *got != *tc.want):
			t.Errorf("%s: got %v, want %q", tc.name, got, *tc.want)
		}
	}
}
