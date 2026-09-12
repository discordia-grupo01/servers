package membership_handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
)

func decodeMemberList(t *testing.T, w *httptest.ResponseRecorder) model.MemberListResponse {
	t.Helper()
	var resp model.MemberListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("bad body %q: %v", w.Body.String(), err)
	}
	return resp
}

func leavePath(serverID, userID string) string {
	return "/v1/servers/" + serverID + "/members/" + userID
}

func membersPath(serverID, query string) string {
	path := "/v1/servers/" + serverID + "/members"
	if query != "" {
		path += "?" + query
	}
	return path
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

func TestListMembers_NoAuth_401(t *testing.T) {
	uc := &fakeMembershipUseCases{}
	r := newRouter(t, uc)

	w := doGet(t, r, membersPath(uuid.New().String(), ""), "")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
	if uc.listCalls != 0 {
		t.Error("use case must not be called without a caller id")
	}
}

func TestListMembers_InvalidServerID_400(t *testing.T) {
	uc := &fakeMembershipUseCases{}
	r := newRouter(t, uc)

	w := doGet(t, r, membersPath("not-a-uuid", ""), testUser)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
	if uc.listCalls != 0 {
		t.Error("use case must not be called with an invalid serverId")
	}
}

func TestListMembers_Success_200(t *testing.T) {
	serverID := uuid.New()
	uc := &fakeMembershipUseCases{
		listMembers: []model.Member{{UserID: testUser, IsOwner: true}},
		listTotal:   1,
	}
	r := newRouter(t, uc)

	w := doGet(t, r, membersPath(serverID.String(), "limit=5&offset=10"), testUser)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if uc.gotListParams.ServerID != serverID {
		t.Errorf("ServerID = %v, want %v", uc.gotListParams.ServerID, serverID)
	}
	if uc.gotListParams.ActorUserID != testUser {
		t.Errorf("ActorUserID = %q, want %q", uc.gotListParams.ActorUserID, testUser)
	}
	if uc.gotListParams.Limit != 5 || uc.gotListParams.Offset != 10 {
		t.Errorf("Limit/Offset = %d/%d, want 5/10", uc.gotListParams.Limit, uc.gotListParams.Offset)
	}

	resp := decodeMemberList(t, w)
	if resp.Total != 1 || len(resp.Members) != 1 {
		t.Fatalf("resp = %+v, want 1 member, total 1", resp)
	}
	if resp.Members[0].UserID != testUser || !resp.Members[0].IsOwner {
		t.Errorf("member = %+v, want owner %q", resp.Members[0], testUser)
	}
}

func TestListMembers_DefaultsWhenNoQuery(t *testing.T) {
	uc := &fakeMembershipUseCases{}
	r := newRouter(t, uc)

	w := doGet(t, r, membersPath(uuid.New().String(), ""), testUser)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if uc.gotListParams.Limit != model.MemberListDefaultLimit || uc.gotListParams.Offset != 0 {
		t.Errorf("Limit/Offset = %d/%d, want %d/0", uc.gotListParams.Limit, uc.gotListParams.Offset, model.MemberListDefaultLimit)
	}
}

func TestListMembers_InvalidLimit_400(t *testing.T) {
	uc := &fakeMembershipUseCases{}
	r := newRouter(t, uc)

	w := doGet(t, r, membersPath(uuid.New().String(), "limit=not-a-number"), testUser)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
	if uc.listCalls != 0 {
		t.Error("use case must not be called with an invalid limit")
	}
}

func TestListMembers_NegativeOffset_400(t *testing.T) {
	uc := &fakeMembershipUseCases{}
	r := newRouter(t, uc)

	w := doGet(t, r, membersPath(uuid.New().String(), "offset=-1"), testUser)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestListMembers_NotAMember_403(t *testing.T) {
	uc := &fakeMembershipUseCases{listErr: model.ErrForbiddenNotMember}
	r := newRouter(t, uc)

	w := doGet(t, r, membersPath(uuid.New().String(), ""), testUser)
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}
}

func TestListMembers_ServerNotFound_404(t *testing.T) {
	uc := &fakeMembershipUseCases{listErr: model.ErrServerNotFound}
	r := newRouter(t, uc)

	w := doGet(t, r, membersPath(uuid.New().String(), ""), testUser)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}
