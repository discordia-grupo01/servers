package handler_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

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

// The list tells the client which of my servers are suspended, and a suspended
// one carries no channels or categories.
func TestListMyServers_MarksSuspendedServers(t *testing.T) {
	r, repo := newRouter(t, oneMB, nil)
	when := time.Now().UTC()
	repo.listResult = []model.Server{
		{ID: uuid.New(), Name: "Activo", OwnerID: testUser},
		{ID: uuid.New(), Name: "Suspendido", OwnerID: testUser, SuspendedAt: &when, Channels: []model.Channel{{ID: uuid.New(), Name: "general"}}},
	}

	w := getAsUser(t, r, testUser, "/v1/servers")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	var got []struct {
		Name       string `json:"name"`
		Suspended  *bool  `json:"suspended"`
		Channels   []any  `json:"channels"`
		Categories []any  `json:"categories"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("bad body %q: %v", w.Body.String(), err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d servers, want 2", len(got))
	}
	for _, s := range got {
		if s.Suspended == nil {
			t.Fatalf("%s: the suspended field is missing, want it always present", s.Name)
		}
	}
	if *got[0].Suspended || !*got[1].Suspended {
		t.Errorf("suspended = %v / %v, want false / true", *got[0].Suspended, *got[1].Suspended)
	}
	if got[1].Channels == nil || len(got[1].Channels) != 0 || got[1].Categories == nil || len(got[1].Categories) != 0 {
		t.Errorf("suspended server: channels=%v categories=%v, want [] and []", got[1].Channels, got[1].Categories)
	}
}
