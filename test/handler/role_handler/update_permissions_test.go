package role_handler_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
)

func TestUpdateRolePermissions_Returns200(t *testing.T) {
	fake := &fakeRoleUseCases{}
	r := newRouter(t, fake)
	roleID := uuid.New()

	w := doJSON(t, r, http.MethodPatch, "/v1/roles/"+roleID.String(), testUser,
		map[string]any{"permissions": []string{"SEND_MESSAGES"}})
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if fake.gotUpdate.RoleID != roleID {
		t.Errorf("handler passed RoleID=%s, want %s", fake.gotUpdate.RoleID, roleID)
	}
}

func TestUpdateRolePermissions_InvalidRoleIDParam_BadRequest(t *testing.T) {
	r := newRouter(t, &fakeRoleUseCases{})
	w := doJSON(t, r, http.MethodPatch, "/v1/roles/not-a-uuid", testUser,
		map[string]any{"permissions": []string{"SEND_MESSAGES"}})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestUpdateRolePermissions_UnknownPermission_Returns400WithInvalidValue(t *testing.T) {
	fake := &fakeRoleUseCases{updateErr: &model.InvalidPermissionError{Value: "FLY_TO_MOON"}}
	r := newRouter(t, fake)
	w := doJSON(t, r, http.MethodPatch, "/v1/roles/"+uuid.New().String(), testUser,
		map[string]any{"permissions": []string{"FLY_TO_MOON"}})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	e := decodeError(t, w)
	if e.Error.Details["invalid_value"] != "FLY_TO_MOON" {
		t.Errorf("details = %v", e.Error.Details)
	}
}

func TestUpdateRolePermissions_RoleNotFound_Returns404(t *testing.T) {
	r := newRouter(t, &fakeRoleUseCases{updateErr: model.ErrRoleNotFound})
	w := doJSON(t, r, http.MethodPatch, "/v1/roles/"+uuid.New().String(), testUser,
		map[string]any{"permissions": []string{"SEND_MESSAGES"}})
	if w.Code != http.StatusNotFound {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestUpdateRolePermissions_NotAdmin_Returns403(t *testing.T) {
	r := newRouter(t, &fakeRoleUseCases{updateErr: model.ErrForbiddenNotAdmin})
	w := doJSON(t, r, http.MethodPatch, "/v1/roles/"+uuid.New().String(), testUser,
		map[string]any{"permissions": []string{"SEND_MESSAGES"}})
	if w.Code != http.StatusForbidden {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}

// CA2 of #22: removing ADMINISTRATOR from your own role is a 403 with a
// machine-readable reason, so clients can explain it.
func TestUpdateRolePermissions_RemovingOwnAdmin_Returns403WithReason(t *testing.T) {
	r := newRouter(t, &fakeRoleUseCases{updateErr: model.ErrCannotRemoveOwnAdmin})
	w := doJSON(t, r, http.MethodPatch, "/v1/roles/"+uuid.New().String(), testUser,
		map[string]any{"permissions": []string{"SEND_MESSAGES"}})
	if w.Code != http.StatusForbidden {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	body := decodeError(t, w)
	if body.Error.Details["reason"] != "cannot_remove_own_admin" {
		t.Errorf("details = %v", body.Error.Details)
	}
}
