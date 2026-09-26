package role_handler_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
)

func TestSetDefaultRole_Returns204(t *testing.T) {
	fake := &fakeRoleUseCases{}
	r := newRouter(t, fake)
	serverID, roleID := uuid.New(), uuid.New()

	w := doJSON(t, r, http.MethodPut, "/v1/servers/"+serverID.String()+"/default-role", testUser,
		map[string]string{"role_id": roleID.String()})
	if w.Code != http.StatusNoContent {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if fake.gotSetDefault.ServerID != serverID || fake.gotSetDefault.RoleID != roleID {
		t.Errorf("service got %+v", fake.gotSetDefault)
	}
	if fake.gotSetDefault.UserID != testUser {
		t.Errorf("UserID = %q, want %q", fake.gotSetDefault.UserID, testUser)
	}
}

func TestSetDefaultRole_InvalidServerIDParam_BadRequest(t *testing.T) {
	r := newRouter(t, &fakeRoleUseCases{})
	w := doJSON(t, r, http.MethodPut, "/v1/servers/not-a-uuid/default-role", testUser,
		map[string]string{"role_id": uuid.New().String()})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestSetDefaultRole_InvalidRoleIDInBody_BadRequest(t *testing.T) {
	r := newRouter(t, &fakeRoleUseCases{})
	w := doJSON(t, r, http.MethodPut, "/v1/servers/"+uuid.New().String()+"/default-role", testUser,
		map[string]string{"role_id": "not-a-uuid"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	e := decodeError(t, w)
	if e.Error.Details["field"] != "role_id" {
		t.Errorf("details = %v", e.Error.Details)
	}
}

func TestSetDefaultRole_RoleServerMismatch_Returns400(t *testing.T) {
	r := newRouter(t, &fakeRoleUseCases{setDefaultErr: model.ErrRoleServerMismatch})
	w := doJSON(t, r, http.MethodPut, "/v1/servers/"+uuid.New().String()+"/default-role", testUser,
		map[string]string{"role_id": uuid.New().String()})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestSetDefaultRole_NotAdmin_Returns403(t *testing.T) {
	r := newRouter(t, &fakeRoleUseCases{setDefaultErr: model.ErrForbiddenNotAdmin})
	w := doJSON(t, r, http.MethodPut, "/v1/servers/"+uuid.New().String()+"/default-role", testUser,
		map[string]string{"role_id": uuid.New().String()})
	if w.Code != http.StatusForbidden {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestSetDefaultRole_RoleNotFound_Returns404(t *testing.T) {
	r := newRouter(t, &fakeRoleUseCases{setDefaultErr: model.ErrRoleNotFound})
	w := doJSON(t, r, http.MethodPut, "/v1/servers/"+uuid.New().String()+"/default-role", testUser,
		map[string]string{"role_id": uuid.New().String()})
	if w.Code != http.StatusNotFound {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestSetDefaultRole_MissingIdentity_Returns401(t *testing.T) {
	r := newRouter(t, &fakeRoleUseCases{})
	w := doJSON(t, r, http.MethodPut, "/v1/servers/"+uuid.New().String()+"/default-role", "",
		map[string]string{"role_id": uuid.New().String()})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("code=%d", w.Code)
	}
}
