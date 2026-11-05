package iconstore_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/discordia-grupo01/servers/internal/iconstore"
	"github.com/discordia-grupo01/servers/internal/model"
)

var gifBytes = []byte("GIF89a0000")

func TestLocalStoreEmojiRoundTrip(t *testing.T) {
	store, err := iconstore.NewLocalStore(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	url, err := store.SaveEmoji("server-1", "emoji-1.gif", gifBytes, "image/gif")
	if err != nil {
		t.Fatalf("SaveEmoji() error = %v", err)
	}
	if want := "/v1/servers/server-1/emojis/emoji-1.gif/image"; url != want {
		t.Errorf("url = %q, want %q", url, want)
	}
	data, contentType, err := store.GetEmoji("server-1", "emoji-1.gif")
	if err != nil {
		t.Fatalf("GetEmoji() error = %v", err)
	}
	if string(data) != string(gifBytes) || contentType != "image/gif" {
		t.Errorf("got %q %q", data, contentType)
	}
	if err := store.DeleteEmoji("server-1", "emoji-1.gif"); err != nil {
		t.Fatalf("DeleteEmoji() error = %v", err)
	}
	if err := store.DeleteEmoji("server-1", "emoji-1.gif"); err != nil {
		t.Errorf("second DeleteEmoji() error = %v", err)
	}
	if _, _, err := store.GetEmoji("server-1", "emoji-1.gif"); !errors.Is(err, model.ErrEmojiNotFound) {
		t.Errorf("GetEmoji() after delete = %v", err)
	}
}

func TestLocalStoreEmojiRejectsPathTraversal(t *testing.T) {
	store, err := iconstore.NewLocalStore(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"..", "../secret", "a/b"} {
		if _, _, err := store.GetEmoji("server-1", key); !errors.Is(err, model.ErrEmojiNotFound) {
			t.Errorf("GetEmoji(%q) = %v", key, err)
		}
	}
	if _, _, err := store.GetEmoji("..", "x.gif"); !errors.Is(err, model.ErrEmojiNotFound) {
		t.Errorf("GetEmoji with traversal in serverID = %v", err)
	}
}

func TestSupabaseStoreSaveEmoji(t *testing.T) {
	var gotPath, gotType, gotUpsert, gotCache string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotType = r.URL.Path, r.Header.Get("Content-Type")
		gotUpsert, gotCache = r.Header.Get("x-upsert"), r.Header.Get("Cache-Control")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := iconstore.NewSupabaseStore(srv.URL, "icons", "key", 1<<20)
	url, err := store.SaveEmoji("server-1", "emoji-1.gif", gifBytes, "image/gif")
	if err != nil {
		t.Fatalf("SaveEmoji() error = %v", err)
	}
	if want := srv.URL + "/storage/v1/object/public/icons/emojis/server-1/emoji-1.gif"; url != want {
		t.Errorf("url = %q, want %q", url, want)
	}
	if gotPath != "/storage/v1/object/icons/emojis/server-1/emoji-1.gif" {
		t.Errorf("path = %q", gotPath)
	}
	if gotType != "image/gif" || gotUpsert != "false" || gotCache != "max-age=31536000" || string(gotBody) != string(gifBytes) {
		t.Errorf("type=%q upsert=%q cache=%q body=%q", gotType, gotUpsert, gotCache, gotBody)
	}
}

func TestSupabaseStoreSaveEmojiFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_mime_type"}`))
	}))
	defer srv.Close()

	store := iconstore.NewSupabaseStore(srv.URL, "icons", "key", 1<<20)
	if _, err := store.SaveEmoji("server-1", "emoji-1.gif", gifBytes, "image/gif"); err == nil {
		t.Fatal("expected an error")
	}
}

func TestSupabaseStoreDeleteEmoji(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := iconstore.NewSupabaseStore(srv.URL, "icons", "key", 1<<20)
	if err := store.DeleteEmoji("server-1", "emoji-1.gif"); err != nil {
		t.Fatalf("DeleteEmoji() error = %v", err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/storage/v1/object/icons/emojis/server-1/emoji-1.gif" {
		t.Errorf("%s %s", gotMethod, gotPath)
	}
}
