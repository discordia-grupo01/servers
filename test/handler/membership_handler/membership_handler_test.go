package membership_handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/userprofile"
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

	w := doGet(t, r, membersPath(serverID.String(), "limit=5&offset=10&q=ana"), testUser)
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
	if uc.gotListParams.Search != "ana" {
		t.Errorf("Search = %q, want ana", uc.gotListParams.Search)
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

// A replicated profile is returned with its five public fields; a member
// without one gets "profile": null (not an omitted key).
func TestListMembers_IncludesProfileOrNull(t *testing.T) {
	uc := &fakeMembershipUseCases{
		listMembers: []model.Member{
			{UserID: testUser, IsOwner: true, Profile: &userprofile.Profile{
				UserID: testUser, Name: "Manu", AvatarURL: "https://cdn/a.png", Description: "hola",
				StatusText: "jugando", StatusEmoji: "🎮", Version: 3,
			}},
			{UserID: "user-2"},
		},
		listTotal: 2,
	}
	r := newRouter(t, uc)

	w := doGet(t, r, membersPath(uuid.New().String(), ""), testUser)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var raw struct {
		Members []map[string]json.RawMessage `json:"members"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("bad body %q: %v", w.Body.String(), err)
	}
	if len(raw.Members) != 2 {
		t.Fatalf("members = %d, want 2", len(raw.Members))
	}
	var profile map[string]any
	if err := json.Unmarshal(raw.Members[0]["profile"], &profile); err != nil {
		t.Fatalf("bad profile %s: %v", raw.Members[0]["profile"], err)
	}
	want := map[string]any{"name": "Manu", "avatar_url": "https://cdn/a.png", "description": "hola", "status_text": "jugando", "status_emoji": "🎮"}
	if len(profile) != len(want) {
		t.Errorf("profile = %v, want exactly %v", profile, want)
	}
	for k, v := range want {
		if profile[k] != v {
			t.Errorf("profile[%q] = %v, want %v", k, profile[k], v)
		}
	}
	if p, ok := raw.Members[1]["profile"]; !ok || string(p) != "null" {
		t.Errorf("second member profile = %s (present=%v), want null", p, ok)
	}
}

func TestListMembers_ReportsMutedUntil(t *testing.T) {
	until := time.Date(2026, 10, 12, 18, 0, 0, 0, time.UTC)
	uc := &fakeMembershipUseCases{
		listMembers: []model.Member{{UserID: "muted", MutedUntil: &until}, {UserID: "free"}},
		listTotal:   2,
	}

	w := doGet(t, newRouter(t, uc), membersPath(uuid.New().String(), ""), testUser)

	var body struct {
		Members []map[string]json.RawMessage `json:"members"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || len(body.Members) != 2 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if got := string(body.Members[0]["muted_until"]); got != `"2026-10-12T18:00:00Z"` {
		t.Errorf("muted member muted_until = %s", got)
	}
	if got := string(body.Members[1]["muted_until"]); got != "null" {
		t.Errorf("free member muted_until = %s, want null", got)
	}
}
