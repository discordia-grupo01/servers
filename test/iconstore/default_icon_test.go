package iconstore_test

import (
	"bytes"
	pngcodec "image/png"
	"testing"

	"github.com/discordia-grupo01/servers/internal/iconstore"
)

func TestGenerateDefaultIcon_ReturnsAValidPNG(t *testing.T) {
	data, err := iconstore.GenerateDefaultIcon()
	if err != nil {
		t.Fatalf("GenerateDefaultIcon: %v", err)
	}
	if !bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")) {
		t.Error("output does not start with the PNG signature")
	}
	if _, err := pngcodec.Decode(bytes.NewReader(data)); err != nil {
		t.Errorf("output does not decode as PNG: %v", err)
	}
	if _, _, err := iconstore.ValidateIcon(data, int64(len(data))); err != nil {
		t.Errorf("ValidateIcon rejected the generated icon: %v", err)
	}
}

func TestGenerateDefaultIcon_PicksFromThePalette(t *testing.T) {
	// Not a strict randomness test, just a smoke check that repeated calls
	// don't all collapse to the exact same bytes (they'd all be the same
	// solid color only if the palette had one entry, which it doesn't).
	first, err := iconstore.GenerateDefaultIcon()
	if err != nil {
		t.Fatal(err)
	}
	sawDifferent := false
	for i := 0; i < 20; i++ {
		next, err := iconstore.GenerateDefaultIcon()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first, next) {
			sawDifferent = true
			break
		}
	}
	if !sawDifferent {
		t.Error("20 calls all produced identical bytes; expected some color variation")
	}
}

func TestLocalStore_GenerateDefault_SaveableAndServable(t *testing.T) {
	st := newStore(t)
	data, err := st.GenerateDefault()
	if err != nil {
		t.Fatalf("GenerateDefault: %v", err)
	}

	if _, err := st.Save("srv-default", data); err != nil {
		t.Fatalf("Save(generated default): %v", err)
	}
	p, ok := st.FilePath("srv-default")
	if !ok || p == "" {
		t.Error("the generated default icon was not saved")
	}
}
