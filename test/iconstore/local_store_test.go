package iconstore_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/discordia-grupo01/servers/internal/iconstore"
	"github.com/discordia-grupo01/servers/internal/model"
)

func newStore(t *testing.T) *iconstore.LocalStore {
	t.Helper()
	st, err := iconstore.NewLocalStore(filepath.Join(t.TempDir(), "icons"), max)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestNewLocalStore_FailsWhenDirCannotBeUsed(t *testing.T) {
	f := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// no se puede crear un subdirectorio dentro de un archivo comun
	if _, err := iconstore.NewLocalStore(filepath.Join(f, "icons"), max); err == nil {
		t.Fatal("expected an error for an unusable directory")
	}
}

func TestLocalStore_SavesFileWithDetectedExtension(t *testing.T) {
	st := newStore(t)
	url, err := st.Save("srv1", png(64))
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	// La URL lleva un "?v=" que cambia en cada Save
	if !strings.HasPrefix(url, "/v1/servers/srv1/icon?v=") {
		t.Errorf("url = %q, want prefix /v1/servers/srv1/icon?v=", url)
	}
	_, contentType, err := st.Get("srv1")
	if err != nil || contentType != "image/png" {
		t.Errorf("Get contentType = %q err=%v", contentType, err)
	}
}

func TestLocalStore_SavesBannerWithBannerURL(t *testing.T) {
	st := newStore(t)
	url, err := st.Save("srv1-banner", png(64))
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !strings.HasPrefix(url, "/v1/servers/srv1/banner?v=") {
		t.Errorf("url = %q, want prefix /v1/servers/srv1/banner?v=", url)
	}
	_, contentType, err := st.Get("srv1-banner")
	if err != nil || contentType != "image/png" {
		t.Errorf("Get contentType = %q err=%v", contentType, err)
	}
}

func TestLocalStore_Save_ReturnsAFreshVersionWhenContentChanges(t *testing.T) {
	st := newStore(t)

	first, err := st.Save("srv1-banner", png(64))
	if err != nil {
		t.Fatalf("first Save: %v", err)
	}
	second, err := st.Save("srv1-banner", jpeg(64))
	if err != nil {
		t.Fatalf("second Save: %v", err)
	}

	if first == second {
		t.Errorf("two saves returned the same URL (%q): a client caching by URL would never fetch the new content", first)
	}

	base := "/v1/servers/srv1/banner?v="
	if !strings.HasPrefix(first, base) || !strings.HasPrefix(second, base) {
		t.Errorf("first=%q second=%q, want both to start with %q", first, second, base)
	}
}

func TestLocalStore_Save_ReturnsTheSameVersionWhenContentIsUnchanged(t *testing.T) {
	st := newStore(t)
	data := png(64)

	first, err := st.Save("srv1-banner", data)
	if err != nil {
		t.Fatalf("first Save: %v", err)
	}
	second, err := st.Save("srv1-banner", data)
	if err != nil {
		t.Fatalf("second Save: %v", err)
	}

	if first != second {
		t.Errorf("saving identical content twice produced different URLs: first=%q second=%q", first, second)
	}
}

func TestLocalStore_ReplacingAnIconRemovesTheOldExtension(t *testing.T) {
	st := newStore(t)
	if _, err := st.Save("srv1", png(64)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Save("srv1", jpeg(64)); err != nil {
		t.Fatal(err)
	}
	_, contentType, err := st.Get("srv1")
	if err != nil || contentType != "image/jpeg" {
		t.Errorf("current icon contentType = %q err=%v, want image/jpeg", contentType, err)
	}
	if _, err := os.Stat(filepath.Join(st.Dir, "srv1.png")); !os.IsNotExist(err) {
		t.Error("the old .png was not removed")
	}
}

func TestLocalStore_DeleteIsIdempotent(t *testing.T) {
	st := newStore(t)
	if _, err := st.Save("srv1", png(64)); err != nil {
		t.Fatal(err)
	}
	if err := st.Delete("srv1"); err != nil {
		t.Fatalf("first Delete: %v", err)
	}
	if err := st.Delete("srv1"); err != nil {
		t.Fatalf("second Delete should be a no-op: %v", err)
	}
	if _, _, err := st.Get("srv1"); !errors.Is(err, model.ErrIconNotFound) {
		t.Error("Get still resolves after Delete")
	}
}

func TestLocalStore_InvalidSaveWritesNoFile(t *testing.T) {
	st := newStore(t)
	if _, err := st.Save("srv2", gif(64)); !errors.Is(err, model.ErrIconUnsupportedType) {
		t.Fatalf("err = %v", err)
	}
	if _, _, err := st.Get("srv2"); !errors.Is(err, model.ErrIconNotFound) {
		t.Error("a file was written for an invalid icon")
	}
}
