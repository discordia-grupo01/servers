package handler_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	invitation "github.com/discordia-grupo01/servers/internal/service/invitation"
)

func invPath(serverID string) string { return "/v1/servers/" + serverID + "/invites" }

func joinPath(code string) string { return "/v1/invites/" + code + "/join" }

func TestGenerateHTTP_Returns201WithCodeAndURL(t *testing.T) {
	inv := &fakeInvites{}
	r := newInviteRouter(t, inv)
	sid := uuid.NewString()

	w := doReq(t, r, http.MethodPost, invPath(sid), testUser, map[string]any{})
	if w.Code != http.StatusCreated {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	body := decodeInvitation(t, w)
	if body.Code == "" {
		t.Error("empty code in response")
	}
	if body.URL != "/invite/"+body.Code {
		t.Errorf("url = %q, want %q", body.URL, "/invite/"+body.Code)
	}
	if body.ServerID != sid {
		t.Errorf("server_id = %q, want %q", body.ServerID, sid)
	}
}

func TestGenerateHTTP_PassesServerIDAndCaller(t *testing.T) {
	inv := &fakeInvites{}
	r := newInviteRouter(t, inv)
	sid := uuid.NewString()

	doReq(t, r, http.MethodPost, invPath(sid), testUser, map[string]any{})
	if inv.genParams.ServerID != sid {
		t.Errorf("ServerID = %q, want %q", inv.genParams.ServerID, sid)
	}
	if inv.genParams.CreatedBy != testUser {
		t.Errorf("CreatedBy = %q, want %q", inv.genParams.CreatedBy, testUser)
	}
}

func TestGenerateHTTP_PassesMaxUses(t *testing.T) {
	inv := &fakeInvites{}
	r := newInviteRouter(t, inv)

	doReq(t, r, http.MethodPost, invPath(uuid.NewString()), testUser, map[string]any{"max_uses": 25})
	if inv.genParams.MaxUses == nil || *inv.genParams.MaxUses != 25 {
		t.Fatalf("MaxUses = %v, want 25", inv.genParams.MaxUses)
	}
}

func TestGenerateHTTP_ExpiresInSecondsBecomesFutureInstant(t *testing.T) {
	inv := &fakeInvites{}
	r := newInviteRouter(t, inv)

	before := time.Now().UTC()
	doReq(t, r, http.MethodPost, invPath(uuid.NewString()), testUser, map[string]any{"expires_in_seconds": 3600})
	got := inv.genParams.ExpiresAt
	if got == nil {
		t.Fatal("ExpiresAt is nil")
	}
	if got.Before(before.Add(30*time.Minute)) || got.After(before.Add(90*time.Minute)) {
		t.Errorf("ExpiresAt = %v, want ~1h from now", got)
	}
}

func TestGenerateHTTP_RejectsNonPositiveExpiresInSeconds(t *testing.T) {
	inv := &fakeInvites{}
	r := newInviteRouter(t, inv)

	w := doReq(t, r, http.MethodPost, invPath(uuid.NewString()), testUser, map[string]any{"expires_in_seconds": 0})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code=%d", w.Code)
	}
	if e := decodeError(t, w); e.Error.Details["reason"] != "expiration_invalid" {
		t.Errorf("reason = %v", e.Error.Details["reason"])
	}
	if inv.genCalled {
		t.Error("service was called despite invalid expiration")
	}
}

func TestGenerateHTTP_EmptyBodyIsAllowed(t *testing.T) {
	inv := &fakeInvites{}
	r := newInviteRouter(t, inv)

	w := doReq(t, r, http.MethodPost, invPath(uuid.NewString()), testUser, nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if inv.genParams.MaxUses != nil || inv.genParams.ExpiresAt != nil {
		t.Error("no options should have been set")
	}
}

func TestGenerateHTTP_MalformedJSON_Returns400(t *testing.T) {
	r := newInviteRouter(t, &fakeInvites{})
	w := doReq(t, r, http.MethodPost, invPath(uuid.NewString()), testUser, "{not json")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code=%d", w.Code)
	}
	if e := decodeError(t, w); e.Error.Details["reason"] != "invalid_json" {
		t.Errorf("reason = %v", e.Error.Details["reason"])
	}
}

func TestGenerateHTTP_MissingIdentity_Returns401(t *testing.T) {
	r := newInviteRouter(t, &fakeInvites{})
	w := doReq(t, r, http.MethodPost, invPath(uuid.NewString()), "", map[string]any{})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("code=%d", w.Code)
	}
}

func TestGenerateHTTP_MapsDomainErrors(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		reason string
	}{
		{"permission", model.ErrInvitePermissionDenied, http.StatusForbidden, "invite_permission_denied"},
		{"max_uses", model.ErrInviteMaxUsesInvalid, http.StatusBadRequest, "max_uses_invalid"},
		{"expiration", model.ErrInviteExpirationInvalid, http.StatusBadRequest, "expiration_invalid"},
		{"server", model.ErrInviteServerNotFound, http.StatusNotFound, "server_not_found"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newInviteRouter(t, &fakeInvites{genErr: tc.err})
			w := doReq(t, r, http.MethodPost, invPath(uuid.NewString()), testUser, map[string]any{})
			if w.Code != tc.status {
				t.Fatalf("code=%d, want %d", w.Code, tc.status)
			}
			e := decodeError(t, w)
			if e.Error.Details["reason"] != tc.reason {
				t.Errorf("reason = %v, want %q", e.Error.Details["reason"], tc.reason)
			}
		})
	}
}

func TestRevokeHTTP_Returns204(t *testing.T) {
	inv := &fakeInvites{}
	r := newInviteRouter(t, inv)

	w := doReq(t, r, http.MethodDelete, "/v1/invites/abc123", testUser, nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if inv.revParams.Code != "abc123" || inv.revParams.UserID != testUser {
		t.Errorf("params = %+v", inv.revParams)
	}
}

func TestRevokeHTTP_UnknownInvitation_Returns404(t *testing.T) {
	r := newInviteRouter(t, &fakeInvites{revErr: model.ErrInvitationNotFound})
	w := doReq(t, r, http.MethodDelete, "/v1/invites/ghost", testUser, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("code=%d", w.Code)
	}
	if e := decodeError(t, w); e.Error.Details["reason"] != "invitation_invalid" {
		t.Errorf("reason = %v", e.Error.Details["reason"])
	}
}

func TestRevokeHTTP_NonMember_Returns403(t *testing.T) {
	r := newInviteRouter(t, &fakeInvites{revErr: model.ErrInvitePermissionDenied})
	w := doReq(t, r, http.MethodDelete, "/v1/invites/x", testUser, nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("code=%d", w.Code)
	}
}

func TestRevokeHTTP_MissingIdentity_Returns401(t *testing.T) {
	r := newInviteRouter(t, &fakeInvites{})
	w := doReq(t, r, http.MethodDelete, "/v1/invites/x", "", nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("code=%d", w.Code)
	}
}

func TestJoinHTTP_NewMember_Returns201(t *testing.T) {
	sid := uuid.New()
	inv := &fakeInvites{joinOut: invitation.JoinResult{ServerID: sid, AlreadyMember: false}}
	r := newInviteRouter(t, inv)

	w := doReq(t, r, http.MethodPost, joinPath("code-1"), testUser, nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	body := decodeJoin(t, w)
	if body.ServerID != sid.String() || body.AlreadyMember {
		t.Errorf("body = %+v", body)
	}
	if inv.joinParams.Code != "code-1" || inv.joinParams.UserID != testUser {
		t.Errorf("params = %+v", inv.joinParams)
	}
}

func TestJoinHTTP_AlreadyMember_Returns200(t *testing.T) {
	sid := uuid.New()
	inv := &fakeInvites{joinOut: invitation.JoinResult{ServerID: sid, AlreadyMember: true}}
	r := newInviteRouter(t, inv)

	w := doReq(t, r, http.MethodPost, joinPath("code-1"), testUser, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if body := decodeJoin(t, w); !body.AlreadyMember || body.ServerID != sid.String() {
		t.Errorf("body = %+v", body)
	}
}

func TestJoinHTTP_InvalidInvitation(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
	}{
		{"not_found", model.ErrInvitationNotFound, http.StatusNotFound},
		{"revoked", model.ErrInvitationRevoked, http.StatusNotFound},
		{"expired", model.ErrInvitationExpired, http.StatusNotFound},
		{"exhausted", model.ErrInvitationExhausted, http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newInviteRouter(t, &fakeInvites{joinErr: tc.err})
			w := doReq(t, r, http.MethodPost, joinPath("x"), testUser, nil)
			if w.Code != tc.status {
				t.Fatalf("code=%d, want %d", w.Code, tc.status)
			}
			if e := decodeError(t, w); e.Error.Details["reason"] != "invitation_invalid" {
				t.Errorf("reason = %v, want invitation_invalid", e.Error.Details["reason"])
			}
		})
	}
}

func TestJoinHTTP_BannedUser_Returns403(t *testing.T) {
	r := newInviteRouter(t, &fakeInvites{joinErr: model.ErrUserBanned})
	w := doReq(t, r, http.MethodPost, joinPath("x"), testUser, nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("code=%d", w.Code)
	}
	if e := decodeError(t, w); e.Error.Details["reason"] != "user_banned" {
		t.Errorf("reason = %v, want user_banned", e.Error.Details["reason"])
	}
}

func TestJoinHTTP_MissingIdentity_Returns401(t *testing.T) {
	r := newInviteRouter(t, &fakeInvites{})
	w := doReq(t, r, http.MethodPost, joinPath("x"), "", nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("code=%d", w.Code)
	}
}
