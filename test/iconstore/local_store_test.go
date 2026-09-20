package iconstore_test

import (
	"errors"
	"os"
	"path/filepath"
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
	if url != "/v1/servers/srv1/icon" {
		t.Errorf("url = %q", url)
	}
	_, contentType, err := st.Get("srv1")
	if err != nil || contentType != "image/png" {
		t.Errorf("Get contentType = %q err=%v", contentType, err)
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
