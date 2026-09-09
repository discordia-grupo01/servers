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
	p, ok := st.FilePath("srv1")
	if !ok || filepath.Ext(p) != ".png" {
		t.Errorf("FilePath = %q ok=%v", p, ok)
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
	p, _ := st.FilePath("srv1")
	if filepath.Ext(p) != ".jpg" {
		t.Errorf("current icon ext = %q, want .jpg", filepath.Ext(p))
	}
	if _, err := os.Stat(p[:len(p)-len(".jpg")] + ".png"); !os.IsNotExist(err) {
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
	if _, ok := st.FilePath("srv1"); ok {
		t.Error("FilePath still resolves after Delete")
	}
}

func TestLocalStore_InvalidSaveWritesNoFile(t *testing.T) {
	st := newStore(t)
	if _, err := st.Save("srv2", gif(64)); !errors.Is(err, model.ErrIconUnsupportedType) {
		t.Fatalf("err = %v", err)
	}
	if _, ok := st.FilePath("srv2"); ok {
		t.Error("a file was written for an invalid icon")
	}
}
