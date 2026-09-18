package role_handler_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/role"
)

func TestCreateRole_Returns201(t *testing.T) {
	r := newRouter(t, &fakeRoleUseCases{})
	serverID := uuid.New()
	w := doJSON(t, r, http.MethodPost, "/v1/servers/"+serverID.String()+"/roles", testUser,
		map[string]string{"name": "Moderator", "color": "#FF00AA"})
	if w.Code != http.StatusCreated {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	got := decodeRole(t, w)
	if got.Name != "Moderator" || got.Color != "#FF00AA" {
		t.Errorf("role = %+v", got)
	}
}

func TestCreateRole_InvalidServerIDParam_BadRequest(t *testing.T) {
	r := newRouter(t, &fakeRoleUseCases{})
	w := doJSON(t, r, http.MethodPost, "/v1/servers/not-a-uuid/roles", testUser,
		map[string]string{"name": "Moderator", "color": "#FF00AA"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	e := decodeError(t, w)
	if e.Error.Details["field"] != "serverId" || e.Error.Details["reason"] != "invalid_uuid" {
		t.Errorf("details = %v", e.Error.Details)
	}
}

func TestRoleHandler_InvalidUUIDParam_BadRequest(t *testing.T) {
	r := newRouter(t, &fakeRoleUseCases{})
	w := doJSON(t, r, http.MethodGet, "/v1/servers/no-es-un-uuid/roles", testUser, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestCreateRole_DomainError_MapsToHTTPStatus(t *testing.T) {
	r := newRouter(t, &fakeRoleUseCases{createErr: model.ErrForbiddenNotOwner})
	w := doJSON(t, r, http.MethodPost, "/v1/servers/"+uuid.New().String()+"/roles", testUser,
		map[string]string{"name": "Moderator", "color": "#FF00AA"})
	if w.Code != http.StatusForbidden {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestCreateRole_MissingIdentity_Returns401(t *testing.T) {
	r := newRouter(t, &fakeRoleUseCases{})
	w := doJSON(t, r, http.MethodPost, "/v1/servers/"+uuid.New().String()+"/roles", "",
		map[string]string{"name": "Moderator", "color": "#FF00AA"})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("code=%d", w.Code)
	}
}

func TestListRoles_Returns200WithArray(t *testing.T) {
	fake := &fakeRoleUseCases{listResult: []role.Role{
		{ID: uuid.New(), Name: "Admin", Color: "#000000"},
		{ID: uuid.New(), Name: "Member", Color: "#FFFFFF"},
	}}
	r := newRouter(t, fake)
	serverID := uuid.New()
	w := doJSON(t, r, http.MethodGet, "/v1/servers/"+serverID.String()+"/roles", testUser, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if fake.gotServerID != serverID {
		t.Errorf("service got serverID=%s, want %s", fake.gotServerID, serverID)
	}
}

func TestListRoles_ServerNotFound_Returns404(t *testing.T) {
	r := newRouter(t, &fakeRoleUseCases{listErr: model.ErrServerNotFound})
	w := doJSON(t, r, http.MethodGet, "/v1/servers/"+uuid.New().String()+"/roles", testUser, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestGetRole_Returns200(t *testing.T) {
	roleID := uuid.New()
	fake := &fakeRoleUseCases{getResult: role.Role{ID: roleID, Name: "Moderator", Color: "#FF00AA"}}
	r := newRouter(t, fake)
	w := doJSON(t, r, http.MethodGet, "/v1/roles/"+roleID.String(), testUser, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if fake.gotGetRole != roleID {
		t.Errorf("service got roleID=%s, want %s", fake.gotGetRole, roleID)
	}
	got := decodeRole(t, w)
	if got.Name != "Moderator" || got.Color != "#FF00AA" {
		t.Errorf("role = %+v", got)
	}
}

func TestGetRole_InvalidRoleIDParam_BadRequest(t *testing.T) {
	r := newRouter(t, &fakeRoleUseCases{})
	w := doJSON(t, r, http.MethodGet, "/v1/roles/not-a-uuid", testUser, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	e := decodeError(t, w)
	if e.Error.Details["field"] != "roleId" || e.Error.Details["reason"] != "invalid_uuid" {
		t.Errorf("details = %v", e.Error.Details)
	}
}

func TestGetRole_NotFound_Returns404(t *testing.T) {
	r := newRouter(t, &fakeRoleUseCases{getErr: model.ErrRoleNotFound})
	w := doJSON(t, r, http.MethodGet, "/v1/roles/"+uuid.New().String(), testUser, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}
