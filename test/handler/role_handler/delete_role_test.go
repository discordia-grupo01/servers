package role_handler_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
)

func TestDeleteRole_Returns204(t *testing.T) {
	fake := &fakeRoleUseCases{}
	r := newRouter(t, fake)
	roleID := uuid.New()

	w := doJSON(t, r, http.MethodDelete, "/v1/roles/"+roleID.String(), testUser, nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if fake.gotDelete.RoleID != roleID {
		t.Errorf("service got %+v", fake.gotDelete)
	}
	if fake.gotDelete.UserID != testUser {
		t.Errorf("UserID = %q, want %q", fake.gotDelete.UserID, testUser)
	}
}

func TestDeleteRole_InvalidRoleIDParam_BadRequest(t *testing.T) {
	r := newRouter(t, &fakeRoleUseCases{})
	w := doJSON(t, r, http.MethodDelete, "/v1/roles/not-a-uuid", testUser, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestDeleteRole_NotAdmin_Returns403(t *testing.T) {
	r := newRouter(t, &fakeRoleUseCases{deleteErr: model.ErrForbiddenNotAdmin})
	w := doJSON(t, r, http.MethodDelete, "/v1/roles/"+uuid.New().String(), testUser, nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestDeleteRole_RoleNotFound_Returns404(t *testing.T) {
	r := newRouter(t, &fakeRoleUseCases{deleteErr: model.ErrRoleNotFound})
	w := doJSON(t, r, http.MethodDelete, "/v1/roles/"+uuid.New().String(), testUser, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestDeleteRole_MissingIdentity_Returns401(t *testing.T) {
	r := newRouter(t, &fakeRoleUseCases{})
	w := doJSON(t, r, http.MethodDelete, "/v1/roles/"+uuid.New().String(), "", nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("code=%d", w.Code)
	}
}
