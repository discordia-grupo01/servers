package member_role_handler_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/role"
)

func TestAssignRole_Returns200(t *testing.T) {
	assigned := role.Role{ID: uuid.New(), Name: "Moderator"}
	fake := &fakeMemberRoleUseCases{assignResult: assigned}
	r := newRouter(t, fake)
	serverID, userID, roleID := uuid.New(), "member-1", uuid.New()

	w := doJSON(t, r, http.MethodPost, "/v1/servers/"+serverID.String()+"/members/"+userID+"/roles", testUser,
		map[string]string{"role_id": roleID.String()})
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if fake.gotAssign.ServerID != serverID || fake.gotAssign.TargetUserID != userID || fake.gotAssign.RoleID != roleID {
		t.Errorf("service got %+v", fake.gotAssign)
	}
	if fake.gotAssign.ActorUserID != testUser {
		t.Errorf("ActorUserID = %q, want %q", fake.gotAssign.ActorUserID, testUser)
	}
	if !strings.Contains(w.Body.String(), assigned.ID.String()) {
		t.Errorf("body = %s, want it to contain the assigned role %s", w.Body.String(), assigned.ID)
	}
}

func TestAssignRole_InvalidServerIDParam_BadRequest(t *testing.T) {
	r := newRouter(t, &fakeMemberRoleUseCases{})
	w := doJSON(t, r, http.MethodPost, "/v1/servers/not-a-uuid/members/member-1/roles", testUser,
		map[string]string{"role_id": uuid.New().String()})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestAssignRole_InvalidRoleIDInBody_BadRequest(t *testing.T) {
	r := newRouter(t, &fakeMemberRoleUseCases{})
	w := doJSON(t, r, http.MethodPost, "/v1/servers/"+uuid.New().String()+"/members/member-1/roles", testUser,
		map[string]string{"role_id": "not-a-uuid"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	e := decodeError(t, w)
	if e.Error.Details["field"] != "role_id" {
		t.Errorf("details = %v", e.Error.Details)
	}
}

func TestAssignRole_RoleServerMismatch_Returns400(t *testing.T) {
	r := newRouter(t, &fakeMemberRoleUseCases{assignErr: model.ErrRoleServerMismatch})
	w := doJSON(t, r, http.MethodPost, "/v1/servers/"+uuid.New().String()+"/members/member-1/roles", testUser,
		map[string]string{"role_id": uuid.New().String()})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestAssignRole_MemberNotFound_Returns404(t *testing.T) {
	r := newRouter(t, &fakeMemberRoleUseCases{assignErr: model.ErrMemberNotFound})
	w := doJSON(t, r, http.MethodPost, "/v1/servers/"+uuid.New().String()+"/members/member-1/roles", testUser,
		map[string]string{"role_id": uuid.New().String()})
	if w.Code != http.StatusNotFound {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestAssignRole_MissingIdentity_Returns401(t *testing.T) {
	r := newRouter(t, &fakeMemberRoleUseCases{})
	w := doJSON(t, r, http.MethodPost, "/v1/servers/"+uuid.New().String()+"/members/member-1/roles", "",
		map[string]string{"role_id": uuid.New().String()})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("code=%d", w.Code)
	}
}

func TestRemoveRole_Returns204(t *testing.T) {
	fake := &fakeMemberRoleUseCases{}
	r := newRouter(t, fake)
	serverID, userID, roleID := uuid.New(), "member-1", uuid.New()

	w := doJSON(t, r, http.MethodDelete, "/v1/servers/"+serverID.String()+"/members/"+userID+"/roles/"+roleID.String(), testUser, nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if fake.gotRemove.ServerID != serverID || fake.gotRemove.TargetUserID != userID || fake.gotRemove.RoleID != roleID {
		t.Errorf("service got %+v", fake.gotRemove)
	}
}

func TestRemoveRole_NotAssigned_Returns404(t *testing.T) {
	r := newRouter(t, &fakeMemberRoleUseCases{removeErr: model.ErrMemberRoleNotFound})
	w := doJSON(t, r, http.MethodDelete, "/v1/servers/"+uuid.New().String()+"/members/member-1/roles/"+uuid.New().String(), testUser, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestRemoveRole_NotOwner_Returns403(t *testing.T) {
	r := newRouter(t, &fakeMemberRoleUseCases{removeErr: model.ErrForbiddenNotOwner})
	w := doJSON(t, r, http.MethodDelete, "/v1/servers/"+uuid.New().String()+"/members/member-1/roles/"+uuid.New().String(), testUser, nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestRemoveRole_InvalidRoleIDParam_BadRequest(t *testing.T) {
	r := newRouter(t, &fakeMemberRoleUseCases{})
	w := doJSON(t, r, http.MethodDelete, "/v1/servers/"+uuid.New().String()+"/members/member-1/roles/not-a-uuid", testUser, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestListMemberRoles_Returns200WithArray(t *testing.T) {
	fake := &fakeMemberRoleUseCases{listResult: []role.Role{{ID: uuid.New()}}}
	r := newRouter(t, fake)
	serverID, userID := uuid.New(), "member-1"

	w := doJSON(t, r, http.MethodGet, "/v1/servers/"+serverID.String()+"/members/"+userID+"/roles", testUser, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if fake.gotListServerID != serverID || fake.gotListUserID != userID {
		t.Errorf("service got serverID=%s userID=%s", fake.gotListServerID, fake.gotListUserID)
	}
}

func TestListMemberRoles_ServerNotFound_Returns404(t *testing.T) {
	r := newRouter(t, &fakeMemberRoleUseCases{listErr: model.ErrServerNotFound})
	w := doJSON(t, r, http.MethodGet, "/v1/servers/"+uuid.New().String()+"/members/member-1/roles", testUser, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}
