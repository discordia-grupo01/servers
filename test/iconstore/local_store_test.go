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

func mustHash(t *testing.T, url string) string {
	t.Helper()
	hash, ok := iconstore.HashFromURL(url)
	if !ok {
		t.Fatalf("url %q has no ?v=<hash>", url)
	}
	return hash
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
	hash := mustHash(t, url)
	_, contentType, err := st.Get(iconstore.StorageKey("srv1", hash))
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
	hash := mustHash(t, url)
	_, contentType, err := st.Get(iconstore.StorageKey("srv1-banner", hash))
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

// actual fix for the race condition: the old implementation
// overwrote the same file/object on every Save, so a Get racing a Save (or,
// in production, a storage backend/CDN that isn't strictly consistent on
// overwrites -- which is how Supabase Storage is fronted) could return
// stale or half-written data. Now every Save writes a brand-new,
// content-hashed key that never existed before, so a previous version stays
// readable under its own key for as long as nothing explicitly deletes it,
// and the new version is readable the instant Save returns -- there is no
// shared mutable resource left for a concurrent Get and Save to race over.
func TestLocalStore_TwoSuccessiveSaves_BothVersionsStayIndependentlyReadable(t *testing.T) {
	st := newStore(t)

	firstURL, err := st.Save("srv1", png(64))
	if err != nil {
		t.Fatalf("first Save: %v", err)
	}
	secondURL, err := st.Save("srv1", jpeg(64))
	if err != nil {
		t.Fatalf("second Save: %v", err)
	}

	firstKey := iconstore.StorageKey("srv1", mustHash(t, firstURL))
	secondKey := iconstore.StorageKey("srv1", mustHash(t, secondURL))

	if _, ct, err := st.Get(firstKey); err != nil || ct != "image/png" {
		t.Errorf("first version no longer readable after a second Save: err=%v contentType=%q", err, ct)
	}
	if _, ct, err := st.Get(secondKey); err != nil || ct != "image/jpeg" {
		t.Errorf("second version not readable: err=%v contentType=%q", err, ct)
	}
}

func TestLocalStore_Delete_RemovesTheGivenKeyOnly(t *testing.T) {
	st := newStore(t)
	firstURL, err := st.Save("srv1", png(64))
	if err != nil {
		t.Fatal(err)
	}
	secondURL, err := st.Save("srv1", jpeg(64))
	if err != nil {
		t.Fatal(err)
	}
	firstKey := iconstore.StorageKey("srv1", mustHash(t, firstURL))
	secondKey := iconstore.StorageKey("srv1", mustHash(t, secondURL))

	if err := st.Delete(firstKey); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, _, err := st.Get(firstKey); !errors.Is(err, model.ErrIconNotFound) {
		t.Error("deleted key is still readable")
	}
	if _, _, err := st.Get(secondKey); err != nil {
		t.Errorf("Delete of one key must not remove a different key: %v", err)
	}
}

func TestLocalStore_DeleteIsIdempotent(t *testing.T) {
	st := newStore(t)
	url, err := st.Save("srv1", png(64))
	if err != nil {
		t.Fatal(err)
	}
	key := iconstore.StorageKey("srv1", mustHash(t, url))
	if err := st.Delete(key); err != nil {
		t.Fatalf("first Delete: %v", err)
	}
	if err := st.Delete(key); err != nil {
		t.Fatalf("second Delete should be a no-op: %v", err)
	}
	if _, _, err := st.Get(key); !errors.Is(err, model.ErrIconNotFound) {
		t.Error("Get still resolves after Delete")
	}
}

func TestLocalStore_InvalidSaveWritesNoFile(t *testing.T) {
	st := newStore(t)
	if _, err := st.Save("srv2", pad([]byte("BM"), 64)); !errors.Is(err, model.ErrIconUnsupportedType) {
		t.Fatalf("err = %v", err)
	}
	if _, _, err := st.Get("srv2"); !errors.Is(err, model.ErrIconNotFound) {
		t.Error("a file was written for an invalid icon")
	}
}

func TestHashFromURL_RoundTripsWithStorageKey(t *testing.T) {
	url, err := newStore(t).Save("srv1", png(64))
	if err != nil {
		t.Fatal(err)
	}
	hash, ok := iconstore.HashFromURL(url)
	if !ok {
		t.Fatalf("HashFromURL could not parse %q", url)
	}
	if !strings.Contains(url, hash) {
		t.Errorf("hash %q not found in url %q", hash, url)
	}
}

func TestHashFromURL_NoVersionQuery_ReturnsNotOK(t *testing.T) {
	if _, ok := iconstore.HashFromURL("/v1/servers/srv1/icon"); ok {
		t.Error("a URL without ?v= must not resolve to a hash")
	}
}
