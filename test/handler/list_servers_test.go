package handler_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/discordia-grupo01/servers/internal/model"
)

func TestListMyServers_NoAuth_Returns401(t *testing.T) {
	r, _ := newRouter(t, oneMB, nil)
	w := getAsUser(t, r, "", "/v1/servers")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestListMyServers_PassesTheCallerID(t *testing.T) {
	r, repo := newRouter(t, oneMB, nil)
	w := getAsUser(t, r, testUser, "/v1/servers")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	if repo.listCalls != 1 || repo.gotListUser != testUser {
		t.Errorf("repo called %d times with user %q, want 1 with %q", repo.listCalls, repo.gotListUser, testUser)
	}
}

func TestListMyServers_EmptyList_ReturnsEmptyArrayNotNull(t *testing.T) {
	r, _ := newRouter(t, oneMB, nil)
	w := getAsUser(t, r, testUser, "/v1/servers")
	if string(w.Body.Bytes()) == "null" {
		t.Error("body is the JSON literal null, want []")
	}
	var got []serverJSON
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("bad body %q: %v", w.Body.String(), err)
	}
	if len(got) != 0 {
		t.Errorf("got %d servers, want 0", len(got))
	}
}

func TestListMyServers_ReturnsWhatTheServiceGives(t *testing.T) {
	r, repo := newRouter(t, oneMB, nil)
	repo.listResult = []model.Server{
		{Name: "Server A", OwnerID: testUser, Channels: []model.Channel{}},
		{Name: "Server B", OwnerID: testUser, Channels: []model.Channel{}},
	}

	w := getAsUser(t, r, testUser, "/v1/servers")
	var got []serverJSON
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("bad body %q: %v", w.Body.String(), err)
	}
	if len(got) != 2 || got[0].Name != "Server A" || got[1].Name != "Server B" {
		t.Errorf("got = %+v", got)
	}
}
