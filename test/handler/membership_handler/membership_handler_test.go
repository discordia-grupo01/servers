package membership_handler_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
)

func leavePath(serverID, userID string) string {
	return "/v1/servers/" + serverID + "/members/" + userID
}

func TestLeaveServer_NoAuth_401(t *testing.T) {
	uc := &fakeMembershipUseCases{}
	r := newRouter(t, uc)

	w := doDelete(t, r, leavePath(uuid.New().String(), testUser), "")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
	if uc.calls != 0 {
		t.Error("use case must not be called without a caller id")
	}
}

func TestLeaveServer_InvalidServerID_400(t *testing.T) {
	uc := &fakeMembershipUseCases{}
	r := newRouter(t, uc)

	w := doDelete(t, r, leavePath("not-a-uuid", testUser), testUser)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
	if uc.calls != 0 {
		t.Error("use case must not be called with an invalid serverId")
	}
}

func TestLeaveServer_Success_204(t *testing.T) {
	uc := &fakeMembershipUseCases{}
	r := newRouter(t, uc)
	serverID := uuid.New()

	w := doDelete(t, r, leavePath(serverID.String(), testUser), testUser)
	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", w.Code)
	}
	if uc.calls != 1 {
		t.Fatalf("use case calls = %d, want 1", uc.calls)
	}
	if uc.gotParams.ServerID != serverID {
		t.Errorf("ServerID = %v, want %v", uc.gotParams.ServerID, serverID)
	}
	if uc.gotParams.ActorUserID != testUser {
		t.Errorf("ActorUserID = %q, want %q (from X-User-Id)", uc.gotParams.ActorUserID, testUser)
	}
	if uc.gotParams.TargetUserID != testUser {
		t.Errorf("TargetUserID = %q, want %q (from the path)", uc.gotParams.TargetUserID, testUser)
	}
}

func TestLeaveServer_OwnerCannotLeave_409(t *testing.T) {
	uc := &fakeMembershipUseCases{err: model.ErrOwnerCannotLeave}
	r := newRouter(t, uc)

	w := doDelete(t, r, leavePath(uuid.New().String(), testUser), testUser)
	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", w.Code)
	}
	e := decodeError(t, w)
	if e.Error.Details["reason"] != "owner_must_transfer_or_delete" {
		t.Errorf("details.reason = %v, want owner_must_transfer_or_delete", e.Error.Details["reason"])
	}
}

func TestLeaveServer_NotSelf_403(t *testing.T) {
	uc := &fakeMembershipUseCases{err: model.ErrLeaveForbidden}
	r := newRouter(t, uc)

	w := doDelete(t, r, leavePath(uuid.New().String(), "someone-else"), testUser)
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}
}

func TestLeaveServer_NotAMember_404(t *testing.T) {
	uc := &fakeMembershipUseCases{err: model.ErrMemberNotFound}
	r := newRouter(t, uc)

	w := doDelete(t, r, leavePath(uuid.New().String(), testUser), testUser)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestLeaveServer_ServerNotFound_404(t *testing.T) {
	uc := &fakeMembershipUseCases{err: model.ErrServerNotFound}
	r := newRouter(t, uc)

	w := doDelete(t, r, leavePath(uuid.New().String(), testUser), testUser)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}
