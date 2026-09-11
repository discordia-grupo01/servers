package handler_test

import (
	"bytes"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

func TestGetIcon_UnknownServer_Returns404(t *testing.T) {
	r, _ := newRouter(t, oneMB, nil)
	w := getAsUser(t, r, testUser, "/v1/servers/"+uuid.New().String()+"/icon")
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestGetIcon_InvalidServerID_Returns400(t *testing.T) {
	r, _ := newRouter(t, oneMB, nil)
	w := getAsUser(t, r, testUser, "/v1/servers/not-a-uuid/icon")
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// The default icon generated at creation time (no upload) must actually be
// servable: this is what makes GenerateDefaultIcon (and icon_url) verifiable
// end-to-end instead of a URL that 404s forever.
func TestGetIcon_DefaultIconFromCreate_IsServed(t *testing.T) {
	r, _ := newRouter(t, oneMB, nil)
	s := decodeServer(t, postCreate(t, r, testUser, "Con Default", nil))

	w := getAsUser(t, r, testUser, "/v1/servers/"+s.ID+"/icon")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("content-type = %q, want image/png", ct)
	}
	if !bytes.HasPrefix(w.Body.Bytes(), []byte("\x89PNG\r\n\x1a\n")) {
		t.Error("served body is not a PNG")
	}
}

func TestGetIcon_UploadedIcon_IsServedBack(t *testing.T) {
	r, _ := newRouter(t, oneMB, nil)
	s := decodeServer(t, postCreate(t, r, testUser, "Con Icono", pngBytes()))

	w := getAsUser(t, r, testUser, "/v1/servers/"+s.ID+"/icon")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
}
