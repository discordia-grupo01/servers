package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
)

func deleteAs(t *testing.T, r http.Handler, user, serverID, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(http.MethodDelete, "/v1/servers/"+serverID, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if user != "" {
		req.Header.Set("X-User-Id", user)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func serverOwnedBy(owner, name string) model.Server {
	return model.Server{ID: uuid.New(), Name: name, NameKey: strings.ToLower(name), OwnerID: owner}
}

func TestDeleteServer_OwnerWithConfirmation_Returns204(t *testing.T) {
	r, repo := newRouter(t, oneMB, nil)
	srv := serverOwnedBy(testUser, "Mi Comunidad")
	repo.getResult = srv

	w := deleteAs(t, r, testUser, srv.ID.String(), `{"confirm_name":"Mi Comunidad"}`)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	if repo.deleteCalls != 1 {
		t.Errorf("DeleteServer calls = %d, want 1", repo.deleteCalls)
	}
	if w.Body.Len() != 0 {
		t.Errorf("a 204 must have no body, got %q", w.Body.String())
	}
}

func TestDeleteServer_NonOwner_Returns403AndKeepsTheServer(t *testing.T) {
	r, repo := newRouter(t, oneMB, nil)
	srv := serverOwnedBy("the-owner", "Mi Comunidad")
	repo.getResult = srv

	w := deleteAs(t, r, testUser, srv.ID.String(), `{"confirm_name":"Mi Comunidad"}`)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	if repo.deleteCalls != 0 {
		t.Error("a non-owner must not delete the server")
	}
}

func TestDeleteServer_WithoutBody_Returns400AndKeepsTheServer(t *testing.T) {
	r, repo := newRouter(t, oneMB, nil)
	srv := serverOwnedBy(testUser, "Mi Comunidad")
	repo.getResult = srv

	w := deleteAs(t, r, testUser, srv.ID.String(), "")

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	var got errJSON
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got.Error.Details["reason"] != "confirmation_mismatch" || got.Error.Details["field"] != "confirm_name" {
		t.Errorf("details = %v", got.Error.Details)
	}
	if repo.deleteCalls != 0 {
		t.Error("an unconfirmed deletion must not delete the server")
	}
}

func TestDeleteServer_WrongConfirmation_Returns400AndKeepsTheServer(t *testing.T) {
	r, repo := newRouter(t, oneMB, nil)
	srv := serverOwnedBy(testUser, "Mi Comunidad")
	repo.getResult = srv

	for _, body := range []string{`{"confirm_name":"otra"}`, `{"confirm_name":""}`, `{}`, `{"confirm_name":null}`} {
		w := deleteAs(t, r, testUser, srv.ID.String(), body)
		if w.Code != http.StatusBadRequest {
			t.Errorf("body %s: status = %d, want 400", body, w.Code)
		}
	}
	if repo.deleteCalls != 0 {
		t.Error("an unconfirmed deletion must not delete the server")
	}
}

func TestDeleteServer_MalformedJSON_Returns400(t *testing.T) {
	r, repo := newRouter(t, oneMB, nil)
	srv := serverOwnedBy(testUser, "Mi Comunidad")
	repo.getResult = srv

	w := deleteAs(t, r, testUser, srv.ID.String(), `{"confirm_name":`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	var got errJSON
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got.Error.Details["reason"] != "invalid_json" {
		t.Errorf("details = %v", got.Error.Details)
	}
	if repo.deleteCalls != 0 {
		t.Error("nothing may be deleted on a malformed request")
	}
}

func TestDeleteServer_Unauthenticated_Returns401(t *testing.T) {
	r, repo := newRouter(t, oneMB, nil)
	srv := serverOwnedBy(testUser, "Mi Comunidad")
	repo.getResult = srv

	w := deleteAs(t, r, "", srv.ID.String(), `{"confirm_name":"Mi Comunidad"}`)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	if repo.deleteCalls != 0 {
		t.Error("an anonymous request must not delete anything")
	}
}

func TestDeleteServer_UnknownServer_Returns404(t *testing.T) {
	r, repo := newRouter(t, oneMB, nil)
	repo.getErr = model.ErrServerNotFound

	w := deleteAs(t, r, testUser, uuid.NewString(), `{"confirm_name":"x"}`)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
}

func TestDeleteServer_InvalidServerID_Returns400(t *testing.T) {
	r, _ := newRouter(t, oneMB, nil)

	w := deleteAs(t, r, testUser, "not-a-uuid", `{"confirm_name":"x"}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
}

func TestDeleteServer_RepositoryFailure_Returns500WithoutLeakingDetails(t *testing.T) {
	r, repo := newRouter(t, oneMB, nil)
	srv := serverOwnedBy(testUser, "Mi Comunidad")
	repo.getResult = srv
	repo.deleteErr = http.ErrAbortHandler

	w := deleteAs(t, r, testUser, srv.ID.String(), `{"confirm_name":"Mi Comunidad"}`)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "abort") {
		t.Errorf("internal error leaked: %s", w.Body.String())
	}
}
