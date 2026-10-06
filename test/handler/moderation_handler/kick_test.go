package moderation_handler_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
)

func kicksPath(serverID uuid.UUID) string { return "/v1/servers/" + serverID.String() + "/kicks" }

func TestKick_Returns204(t *testing.T) {
	serverID := uuid.New()
	f := &fakeModeration{}
	w := do(t, newRouter(f), http.MethodPost, kicksPath(serverID), testUser, `{"user_id":"member-1"}`)
	if w.Code != http.StatusNoContent || w.Body.Len() != 0 {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if f.gotKick.ServerID != serverID || f.gotKick.ActorUserID != testUser || f.gotKick.TargetUserID != "member-1" {
		t.Errorf("service got %+v", f.gotKick)
	}
}

func TestKick_InvalidJSON_BadRequest(t *testing.T) {
	f := &fakeModeration{}
	w := do(t, newRouter(f), http.MethodPost, kicksPath(uuid.New()), testUser, `{"user_id":`)
	if w.Code != http.StatusBadRequest || f.kickCalled {
		t.Fatalf("code=%d called=%v", w.Code, f.kickCalled)
	}
	if e := decodeError(t, w); e.Error.Details["reason"] != "invalid_json" {
		t.Errorf("details = %v", e.Error.Details)
	}
}

func TestKick_InvalidServerID_BadRequest(t *testing.T) {
	f := &fakeModeration{}
	w := do(t, newRouter(f), http.MethodPost, "/v1/servers/nope/kicks", testUser, `{"user_id":"x"}`)
	if w.Code != http.StatusBadRequest || f.kickCalled {
		t.Fatalf("code=%d called=%v", w.Code, f.kickCalled)
	}
}

func TestKick_MissingIdentity_Returns401(t *testing.T) {
	w := do(t, newRouter(&fakeModeration{}), http.MethodPost, kicksPath(uuid.New()), "", `{"user_id":"x"}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("code=%d", w.Code)
	}
}

func TestKick_DomainErrorsMapToHTTP(t *testing.T) {
	cases := map[string]struct {
		err    error
		status int
		field  string
		reason string
	}{
		"missing KICK_MEMBERS (CA3)": {model.ErrForbiddenMissingPermission, http.StatusForbidden, "", "missing_permission"},
		"equal or higher role (CA2)": {model.ErrTargetOutranksActor, http.StatusForbidden, "", "target_outranks_actor"},
		"target is the owner":        {model.ErrCannotKickOwner, http.StatusForbidden, "", "cannot_kick_owner"},
		"kicking yourself":           {model.ErrCannotKickSelf, http.StatusBadRequest, "user_id", "cannot_kick_self"},
		"missing user_id":            {model.ErrBanTargetRequired, http.StatusBadRequest, "user_id", "required"},
		"target not a member":        {model.ErrMemberNotFound, http.StatusNotFound, "", ""},
		"server not found":           {model.ErrServerNotFound, http.StatusNotFound, "", ""},
		"unexpected error is hidden": {errors.New("pq: boom"), http.StatusInternalServerError, "", ""},
	}
	for name, tc := range cases {
		w := do(t, newRouter(&fakeModeration{kickErr: tc.err}), http.MethodPost, kicksPath(uuid.New()), testUser, `{"user_id":"x"}`)
		if w.Code != tc.status {
			t.Errorf("%s: code=%d, want %d", name, w.Code, tc.status)
			continue
		}
		e := decodeError(t, w)
		if tc.reason != "" && e.Error.Details["reason"] != tc.reason {
			t.Errorf("%s: details = %v", name, e.Error.Details)
		}
		if tc.field != "" && e.Error.Details["field"] != tc.field {
			t.Errorf("%s: details = %v", name, e.Error.Details)
		}
	}
}
