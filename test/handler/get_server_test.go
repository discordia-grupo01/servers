package handler_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/role"
)

func TestGetServer_NoAuth_Returns401(t *testing.T) {
	r, _ := newRouter(t, oneMB, nil)
	w := getAsUser(t, r, "", "/v1/servers/"+uuid.New().String())
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestGetServer_InvalidServerID_Returns400(t *testing.T) {
	r, _ := newRouter(t, oneMB, nil)
	w := getAsUser(t, r, testUser, "/v1/servers/not-a-uuid")
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestGetServer_UnknownServer_Returns404(t *testing.T) {
	r, repo := newRouter(t, oneMB, nil)
	repo.getErr = model.ErrServerNotFound
	w := getAsUser(t, r, testUser, "/v1/servers/"+uuid.New().String())
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// The point of this endpoint: after joining via invite, a client only has
// server_id -- this is how it gets the name/channels/icon_url.
func TestGetServer_Found_ReturnsNameAndChannels(t *testing.T) {
	r, repo := newRouter(t, oneMB, nil)
	id := uuid.New()
	repo.getResult = model.Server{
		ID: id, Name: "Joined Server", OwnerID: "someone-else",
		Channels: []model.Channel{{ID: uuid.New(), Name: "general", Kind: model.ChannelText}},
	}
	repo.standing = role.Standing{IsMember: true, Permissions: role.PermViewChannels}

	w := getAsUser(t, r, testUser, "/v1/servers/"+id.String())
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	s := decodeServer(t, w)
	if s.Name != "Joined Server" || len(s.Channels) != 1 {
		t.Errorf("got = %+v", s)
	}
}

// #57: a caller who isn't a member sees the server but none of its channels.
func TestGetServer_NotAMember_ReturnsNoChannels(t *testing.T) {
	r, repo := newRouter(t, oneMB, nil)
	id := uuid.New()
	repo.getResult = model.Server{
		ID: id, Name: "Some Server", OwnerID: "someone-else",
		Channels: []model.Channel{{ID: uuid.New(), Name: "general", Kind: model.ChannelText}},
	}

	w := getAsUser(t, r, testUser, "/v1/servers/"+id.String())
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	if s := decodeServer(t, w); s.Name != "Some Server" || s.Channels == nil || len(s.Channels) != 0 {
		t.Errorf("got = %+v, want the server with channels: []", s)
	}
}

func TestGetMembership_NoAuth_Returns401(t *testing.T) {
	r, _ := newRouter(t, oneMB, nil)
	w := getAsUser(t, r, "", "/v1/servers/"+uuid.New().String()+"/membership")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestGetMembership_InvalidServerID_Returns400(t *testing.T) {
	r, _ := newRouter(t, oneMB, nil)
	w := getAsUser(t, r, testUser, "/v1/servers/not-a-uuid/membership")
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestGetMembership_Member_Returns200WithoutLoadingTheServer(t *testing.T) {
	r, repo := newRouter(t, oneMB, nil)
	repo.memberResult = true
	id := uuid.New()

	w := getAsUser(t, r, testUser, "/v1/servers/"+id.String()+"/membership")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	if body := w.Body.String(); !strings.Contains(body, `"member":true`) || !strings.Contains(body, id.String()) {
		t.Errorf("body = %s", body)
	}
	if repo.memberCalls != 1 || repo.getCalls != 0 {
		t.Errorf("IsMember calls = %d, GetByID calls = %d, want 1 and 0", repo.memberCalls, repo.getCalls)
	}
}

// Unlike GET /v1/servers/{id}, which shows a non-member the server with no
// channels, this is a yes/no check: voice must not read "200" as "member".
func TestGetMembership_NotAMember_Returns403(t *testing.T) {
	r, _ := newRouter(t, oneMB, nil)
	w := getAsUser(t, r, testUser, "/v1/servers/"+uuid.New().String()+"/membership")
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}
}

func TestGetMembership_UnknownServer_Returns404(t *testing.T) {
	r, repo := newRouter(t, oneMB, nil)
	repo.memberErr = model.ErrServerNotFound
	w := getAsUser(t, r, testUser, "/v1/servers/"+uuid.New().String()+"/membership")
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}
