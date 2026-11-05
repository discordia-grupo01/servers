package emoji_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/emoji"
)

var (
	png  = []byte("\x89PNG\r\n\x1a\n0000")
	gif  = []byte("GIF89a0000")
	webp = []byte("RIFF0000WEBPVP8 ")
)

func TestValidateName(t *testing.T) {
	valid := []string{"ok", "party_parrot", "Emoji1", strings.Repeat("a", 32)}
	for _, name := range valid {
		if err := emoji.ValidateName(name); err != nil {
			t.Errorf("ValidateName(%q) = %v", name, err)
		}
	}
	invalid := []string{"", "a", strings.Repeat("a", 33), "con espacio", "tilde_á", "a-b", ":ok:", "ok\n"}
	for _, name := range invalid {
		if err := emoji.ValidateName(name); !errors.Is(err, model.ErrEmojiNameInvalid) {
			t.Errorf("ValidateName(%q) = %v, want ErrEmojiNameInvalid", name, err)
		}
	}
}

func TestInspectImageAccepted(t *testing.T) {
	cases := []struct {
		name      string
		data      []byte
		extension string
		animated  bool
	}{
		{"png", png, ".png", false},
		{"webp", webp, ".webp", false},
		{"gif", gif, ".gif", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			image, err := emoji.InspectImage(tc.data)
			if err != nil {
				t.Fatalf("InspectImage() error = %v", err)
			}
			if image.Extension != tc.extension || image.Animated != tc.animated {
				t.Errorf("got %+v", image)
			}
		})
	}
}

func TestInspectImageRejected(t *testing.T) {
	atLimit := append(append([]byte{}, png...), bytes.Repeat([]byte{0}, int(emoji.MaxImageBytes)-len(png))...)
	if _, err := emoji.InspectImage(atLimit); err != nil {
		t.Errorf("image at the limit: %v", err)
	}
	cases := map[string]struct {
		data []byte
		want error
	}{
		"empty":        {nil, model.ErrEmojiImageRequired},
		"over limit":   {append(atLimit, 0), model.ErrEmojiImageTooLarge},
		"jpeg":         {[]byte("\xff\xd8\xff\xe0000000"), model.ErrEmojiImageUnsupported},
		"text as .png": {[]byte("<svg></svg>"), model.ErrEmojiImageUnsupported},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := emoji.InspectImage(tc.data); !errors.Is(err, tc.want) {
				t.Errorf("error = %v, want %v", err, tc.want)
			}
		})
	}
}
