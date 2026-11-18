package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/discordia-grupo01/servers/test/errshape"
)

func TestRouter_UnknownRoute_Returns404StandardFormat(t *testing.T) {
	r, _ := newRouter(t, 1<<20, nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/no-existe", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	var e errJSON
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatalf("bad error body %q: %v", w.Body.String(), err)
	}
	if e.Error.Code != "NOT_FOUND" {
		t.Errorf("error.code = %q, want NOT_FOUND", e.Error.Code)
	}
}

func TestRouter_UnsupportedMethod_Returns405StandardFormat(t *testing.T) {
	r, _ := newRouter(t, 1<<20, nil)
	// GET /v1/channels/:id doesn't exist -- only PATCH and DELETE are registered.
	req := httptest.NewRequest(http.MethodGet, "/v1/channels/00000000-0000-0000-0000-000000000000", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	var e errJSON
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatalf("bad error body %q: %v", w.Body.String(), err)
	}
	if e.Error.Code != "METHOD_NOT_ALLOWED" {
		t.Errorf("error.code = %q, want METHOD_NOT_ALLOWED", e.Error.Code)
	}
}

func TestRouter_FrameworkErrors_UseTheEnvelope(t *testing.T) {
	r, _ := newRouter(t, 1<<20, nil)
	for _, tc := range []struct {
		name, method, path, body string
		status                   int
		code                     string
	}{
		{"ruta inexistente", http.MethodGet, "/v1/no-existe", "", 404, "NOT_FOUND"},
		{"método no permitido", http.MethodGet, "/v1/channels/00000000-0000-0000-0000-000000000000", "", 405, "METHOD_NOT_ALLOWED"},
		{"json mal formado", http.MethodPost, "/v1/servers/00000000-0000-0000-0000-000000000000/channels", `{"name":`, 400, "INVALID_BODY"},
		{"uuid de path inválido", http.MethodPost, "/v1/servers/xx/channels", `{}`, 400, "INVALID_PARAMETER"},
		{"sin credencial", http.MethodPost, "/v1/servers/00000000-0000-0000-0000-000000000000/channels", `{}`, 401, "UNAUTHORIZED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			if tc.code != "UNAUTHORIZED" {
				req.Header.Set("X-User-Id", testUser)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tc.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if got := errshape.Envelope(t, w.Body.Bytes()); got != tc.code {
				t.Errorf("code = %q, want %q", got, tc.code)
			}
		})
	}
}
