package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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
	if e.Error.Code != http.StatusNotFound {
		t.Errorf("error.code = %d, want 404", e.Error.Code)
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
	if e.Error.Code != http.StatusMethodNotAllowed {
		t.Errorf("error.code = %d, want 405", e.Error.Code)
	}
}
