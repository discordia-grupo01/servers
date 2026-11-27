package handler_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func doAsUser(t *testing.T, r http.Handler, method, user, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("X-User-Id", user)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestRouter_NeverRedirects(t *testing.T) {
	r, _ := newRouter(t, oneMB, nil)
	paths := []struct{ method, path string }{
		{http.MethodDelete, "/v1/invites/%2F"},
		{http.MethodGet, "/v1/roles/%2F"},
		{http.MethodPatch, "/v1/roles/%2F"},
		{http.MethodPatch, "/v1/channels/%2F"},
		{http.MethodGet, "/v1/servers/"},
	}
	for _, p := range paths {
		w := doAsUser(t, r, p.method, testUser, p.path)
		if w.Code >= 300 && w.Code < 400 {
			t.Errorf("%s %s answered %d with Location %q, want no redirect", p.method, p.path, w.Code, w.Header().Get("Location"))
		}
	}
}

func TestRouter_RejectsNulInPathParameters(t *testing.T) {
	r, _ := newRouter(t, oneMB, nil)
	for _, p := range []struct{ method, path string }{
		{http.MethodDelete, "/v1/invites/%00"},
		{http.MethodPost, "/v1/invites/%00/join"},
		{http.MethodDelete, "/v1/servers/" + uuid.NewString() + "/mutes/%00"},
	} {
		w := doAsUser(t, r, p.method, testUser, p.path)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s %s answered %d, want 400", p.method, p.path, w.Code)
		}
	}
}
