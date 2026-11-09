package iconstore_test

import (
	"encoding/json"
	"errors"
	"fmt"
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
	hostile := []string{"", ".", "..", "../secret", "a/b", `a\b`, ".hidden", "a\x00b", "/etc/passwd", "a b"}
	for _, key := range hostile {
		if _, _, err := store.GetEmoji("server-1", key); !errors.Is(err, model.ErrEmojiNotFound) {
			t.Errorf("GetEmoji(%q) = %v", key, err)
		}
		if _, _, err := store.GetEmoji(key, "x.gif"); !errors.Is(err, model.ErrEmojiNotFound) {
			t.Errorf("GetEmoji with %q as serverID = %v", key, err)
		}
		if _, err := store.SaveEmoji("server-1", key, gifBytes, "image/gif"); !errors.Is(err, model.ErrEmojiNotFound) {
			t.Errorf("SaveEmoji(%q) = %v", key, err)
		}
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

func TestLocalStoreDeleteServerEmojis(t *testing.T) {
	store, err := iconstore.NewLocalStore(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"a.gif", "b.png"} {
		if _, err := store.SaveEmoji("server-1", key, gifBytes, "image/gif"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.SaveEmoji("server-2", "c.gif", gifBytes, "image/gif"); err != nil {
		t.Fatal(err)
	}

	if err := store.DeleteServerEmojis("server-1"); err != nil {
		t.Fatalf("DeleteServerEmojis() error = %v", err)
	}
	if _, _, err := store.GetEmoji("server-1", "a.gif"); !errors.Is(err, model.ErrEmojiNotFound) {
		t.Errorf("a.gif should be gone: %v", err)
	}
	if _, _, err := store.GetEmoji("server-2", "c.gif"); err != nil {
		t.Errorf("another server's emoji should stay: %v", err)
	}
	if err := store.DeleteServerEmojis("server-1"); err != nil {
		t.Errorf("server without emojis: %v", err)
	}
	for _, id := range []string{"", "..", "../icons"} {
		if err := store.DeleteServerEmojis(id); err == nil {
			t.Errorf("DeleteServerEmojis(%q) should fail", id)
		}
	}
}

func TestSupabaseStoreDeleteServerEmojisPagesThroughEverything(t *testing.T) {
	const total = 250
	var offsets []float64
	var deleted []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/storage/v1/object/list/icons":
			var req struct {
				Prefix string  `json:"prefix"`
				Limit  float64 `json:"limit"`
				Offset float64 `json:"offset"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
			}
			if req.Prefix != "emojis/server-1" || req.Limit != 100 {
				t.Errorf("list request = %+v", req)
			}
			offsets = append(offsets, req.Offset)
			page := []map[string]any{}
			for i := int(req.Offset); i < min(int(req.Offset)+100, total); i++ {
				page = append(page, map[string]any{"id": "x", "name": fmt.Sprintf("%d.gif", i)})
			}
			_ = json.NewEncoder(w).Encode(page)
		case r.Method == http.MethodDelete && r.URL.Path == "/storage/v1/object/icons":
			var req struct {
				Prefixes []string `json:"prefixes"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
			}
			if len(req.Prefixes) > 100 {
				t.Errorf("batch of %d", len(req.Prefixes))
			}
			deleted = append(deleted, req.Prefixes...)
			_, _ = w.Write([]byte(`[]`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	store := iconstore.NewSupabaseStore(srv.URL, "icons", "key", 1<<20)
	if err := store.DeleteServerEmojis("server-1"); err != nil {
		t.Fatalf("DeleteServerEmojis() error = %v", err)
	}
	if len(offsets) != 3 || offsets[2] != 200 {
		t.Errorf("offsets = %v", offsets)
	}
	if len(deleted) != total || deleted[0] != "emojis/server-1/0.gif" {
		t.Errorf("deleted %d, first %q", len(deleted), deleted[0])
	}
}

func TestSupabaseStoreDeleteServerEmojisSkipsFoldersAndEmptyServers(t *testing.T) {
	deletes := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deletes++
		}
		_, _ = w.Write([]byte(`[{"id":null,"name":"subfolder"}]`))
	}))
	defer srv.Close()

	store := iconstore.NewSupabaseStore(srv.URL, "icons", "key", 1<<20)
	if err := store.DeleteServerEmojis("server-1"); err != nil {
		t.Fatalf("DeleteServerEmojis() error = %v", err)
	}
	if deletes != 0 {
		t.Errorf("deletes = %d", deletes)
	}
}

func TestSupabaseStoreDeleteServerEmojisReportsFailures(t *testing.T) {
	cases := map[string]func(w http.ResponseWriter, r *http.Request){
		"list": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		},
		"bad list body": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`not json`))
		},
		"delete": func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodDelete {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			_, _ = w.Write([]byte(`[{"id":"x","name":"a.gif"}]`))
		},
	}
	for name, h := range cases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(h))
			defer srv.Close()
			store := iconstore.NewSupabaseStore(srv.URL, "icons", "key", 1<<20)
			if err := store.DeleteServerEmojis("server-1"); err == nil {
				t.Error("expected an error")
			}
		})
	}
}
