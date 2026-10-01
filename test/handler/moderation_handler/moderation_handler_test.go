package moderation_handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/handler"
	"github.com/discordia-grupo01/servers/internal/middleware"
	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/moderation"
	"github.com/discordia-grupo01/servers/internal/model/userprofile"
	"github.com/discordia-grupo01/servers/internal/service/moderation_service"
)

const testUser = "mod-1"

type fakeModeration struct {
	banErr    error
	banCalled bool
	gotBan    moderation_service.BanParams
	banResult moderation.Ban

	unbanErr    error
	unbanCalled bool
	gotUnban    moderation_service.UnbanParams

	listErr    error
	listCalled bool
	gotList    moderation_service.ListBansParams
	listResult moderation.BanPage
}

func (f *fakeModeration) BanMember(_ context.Context, p moderation_service.BanParams) (moderation.Ban, error) {
	f.banCalled, f.gotBan = true, p
	return f.banResult, f.banErr
}

func (f *fakeModeration) UnbanMember(_ context.Context, p moderation_service.UnbanParams) error {
	f.unbanCalled, f.gotUnban = true, p
	return f.unbanErr
}

func (f *fakeModeration) ListBans(_ context.Context, p moderation_service.ListBansParams) (moderation.BanPage, error) {
	f.listCalled, f.gotList = true, p
	return f.listResult, f.listErr
}

type okPinger struct{}

func (okPinger) Ping(context.Context) error { return nil }

func newRouter(f *fakeModeration) *gin.Engine {
	gin.SetMode(gin.TestMode)
	return handler.NewRouter(handler.Deps{
		Moderation: f,
		Auth:       middleware.AuthConfig{UserIDHeader: "X-User-Id"},
		DB:         okPinger{},
	})
}

func do(t *testing.T, r http.Handler, method, path, user, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if user != "" {
		req.Header.Set("X-User-Id", user)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

type errJSON struct {
	Error struct {
		Code    int            `json:"code"`
		Message string         `json:"message"`
		Details map[string]any `json:"details"`
	} `json:"error"`
}

func decodeError(t *testing.T, w *httptest.ResponseRecorder) errJSON {
	t.Helper()
	var e errJSON
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatalf("bad error body %q: %v", w.Body.String(), err)
	}
	return e
}

func bansPath(serverID uuid.UUID) string { return "/v1/servers/" + serverID.String() + "/bans" }

func TestBan_Returns201WithTheBan(t *testing.T) {
	serverID := uuid.New()
	reason, by := "spam", testUser
	at := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	f := &fakeModeration{banResult: moderation.Ban{ServerID: serverID, UserID: "member-1", Reason: &reason, BannedBy: &by, BannedAt: at}}

	w := do(t, newRouter(f), http.MethodPost, bansPath(serverID), testUser, `{"user_id":"member-1","reason":"spam"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if f.gotBan.ServerID != serverID || f.gotBan.ActorUserID != testUser || f.gotBan.TargetUserID != "member-1" ||
		f.gotBan.Reason == nil || *f.gotBan.Reason != "spam" {
		t.Errorf("service got %+v", f.gotBan)
	}
	var got moderation.BanResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ServerID != serverID.String() || got.UserID != "member-1" || got.Reason == nil || *got.Reason != "spam" ||
		got.BannedBy == nil || *got.BannedBy != testUser || !got.BannedAt.Equal(at) {
		t.Errorf("body = %+v", got)
	}
}

func TestBan_ReasonIsOptional(t *testing.T) {
	f := &fakeModeration{}
	w := do(t, newRouter(f), http.MethodPost, bansPath(uuid.New()), testUser, `{"user_id":"member-1"}`)
	if w.Code != http.StatusCreated || f.gotBan.Reason != nil {
		t.Fatalf("code=%d reason=%v", w.Code, f.gotBan.Reason)
	}
}

func TestBan_InvalidJSON_BadRequest(t *testing.T) {
	f := &fakeModeration{}
	w := do(t, newRouter(f), http.MethodPost, bansPath(uuid.New()), testUser, `{"user_id":`)
	if w.Code != http.StatusBadRequest || f.banCalled {
		t.Fatalf("code=%d called=%v", w.Code, f.banCalled)
	}
}

func TestBan_InvalidServerID_BadRequest(t *testing.T) {
	w := do(t, newRouter(&fakeModeration{}), http.MethodPost, "/v1/servers/nope/bans", testUser, `{"user_id":"x"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code=%d", w.Code)
	}
}

func TestBan_MissingIdentity_Returns401(t *testing.T) {
	w := do(t, newRouter(&fakeModeration{}), http.MethodPost, bansPath(uuid.New()), "", `{"user_id":"x"}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("code=%d", w.Code)
	}
}

func TestBan_DomainErrorsMapToHTTP(t *testing.T) {
	cases := map[string]struct {
		err    error
		status int
		field  string
		reason string
	}{
		"missing BAN_MEMBERS (CA3)":  {model.ErrForbiddenMissingPermission, http.StatusForbidden, "", "missing_permission"},
		"equal or higher role (CA2)": {model.ErrTargetOutranksActor, http.StatusForbidden, "", "target_outranks_actor"},
		"target is the owner":        {model.ErrCannotBanOwner, http.StatusForbidden, "", "cannot_ban_owner"},
		"banning yourself":           {model.ErrCannotBanSelf, http.StatusBadRequest, "user_id", "cannot_ban_self"},
		"missing user_id":            {model.ErrBanTargetRequired, http.StatusBadRequest, "user_id", "required"},
		"reason too long":            {moderation.ErrReasonTooLong, http.StatusBadRequest, "reason", "reason_too_long"},
		"reason with control chars":  {moderation.ErrReasonInvalidChars, http.StatusBadRequest, "reason", "reason_invalid_chars"},
		"already banned":             {model.ErrAlreadyBanned, http.StatusConflict, "", "already_banned"},
		"target not a member":        {model.ErrMemberNotFound, http.StatusNotFound, "", ""},
		"server not found":           {model.ErrServerNotFound, http.StatusNotFound, "", ""},
		"unexpected error is hidden": {errors.New("pq: boom"), http.StatusInternalServerError, "", ""},
	}
	for name, tc := range cases {
		w := do(t, newRouter(&fakeModeration{banErr: tc.err}), http.MethodPost, bansPath(uuid.New()), testUser, `{"user_id":"x"}`)
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

func TestListBans_Returns200WithThePage(t *testing.T) {
	serverID := uuid.New()
	f := &fakeModeration{listResult: moderation.BanPage{
		Bans:  []moderation.Ban{{ServerID: serverID, UserID: "a"}, {ServerID: serverID, UserID: "b"}},
		Total: 5, Limit: 2, Offset: 2,
	}}
	w := do(t, newRouter(f), http.MethodGet, bansPath(serverID)+"?limit=2&offset=2", testUser, "")
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if f.gotList.ServerID != serverID || f.gotList.ActorUserID != testUser || f.gotList.Limit != 2 || f.gotList.Offset != 2 {
		t.Errorf("service got %+v", f.gotList)
	}
	var got moderation.BanListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Bans) != 2 || got.Bans[0].UserID != "a" || got.Total != 5 || got.Limit != 2 || got.Offset != 2 {
		t.Errorf("body = %+v", got)
	}
}

func TestListBans_IncludesTheBannedUsersProfile(t *testing.T) {
	serverID := uuid.New()
	f := &fakeModeration{listResult: moderation.BanPage{
		Bans: []moderation.Ban{
			{ServerID: serverID, UserID: "a", Profile: &userprofile.Profile{UserID: "a", Name: "Manu", AvatarURL: "https://cdn/a.png", Description: "hola", StatusText: "jugando", StatusEmoji: "🎮"}},
			{ServerID: serverID, UserID: "b"},
		},
		Total: 2, Limit: 20,
	}}
	w := do(t, newRouter(f), http.MethodGet, bansPath(serverID), testUser, "")
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	var got moderation.BanListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	want := model.MemberProfileResponse{Name: "Manu", AvatarURL: "https://cdn/a.png", Description: "hola", StatusText: "jugando", StatusEmoji: "🎮"}
	if got.Bans[0].Profile == nil || *got.Bans[0].Profile != want {
		t.Errorf("first profile = %+v, want %+v", got.Bans[0].Profile, want)
	}
	if got.Bans[1].Profile != nil {
		t.Errorf("second profile = %+v, want null while not replicated", got.Bans[1].Profile)
	}
}

func TestListBans_DefaultsAndEmptyList(t *testing.T) {
	f := &fakeModeration{listResult: moderation.BanPage{Limit: moderation.ListDefaultLimit}}
	w := do(t, newRouter(f), http.MethodGet, bansPath(uuid.New()), testUser, "")
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d", w.Code)
	}
	if f.gotList.Limit != moderation.ListDefaultLimit || f.gotList.Offset != 0 {
		t.Errorf("service got %+v", f.gotList)
	}
	var got map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if bans, ok := got["bans"].([]any); !ok || len(bans) != 0 {
		t.Errorf("bans = %v, want an empty array (not null)", got["bans"])
	}
}

func TestListBans_InvalidPagination_BadRequest(t *testing.T) {
	for _, q := range []string{"?limit=-1", "?limit=abc", "?offset=-2"} {
		f := &fakeModeration{}
		w := do(t, newRouter(f), http.MethodGet, bansPath(uuid.New())+q, testUser, "")
		if w.Code != http.StatusBadRequest || f.listCalled {
			t.Errorf("%s: code=%d called=%v", q, w.Code, f.listCalled)
		}
	}
}

func TestUnban_Returns204(t *testing.T) {
	serverID := uuid.New()
	f := &fakeModeration{}
	w := do(t, newRouter(f), http.MethodDelete, bansPath(serverID)+"/banned-1", testUser, "")
	if w.Code != http.StatusNoContent || w.Body.Len() != 0 {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if f.gotUnban.ServerID != serverID || f.gotUnban.ActorUserID != testUser || f.gotUnban.TargetUserID != "banned-1" {
		t.Errorf("service got %+v", f.gotUnban)
	}
}

func TestUnban_InvalidServerID_BadRequest(t *testing.T) {
	f := &fakeModeration{}
	w := do(t, newRouter(f), http.MethodDelete, "/v1/servers/nope/bans/banned-1", testUser, "")
	if w.Code != http.StatusBadRequest || f.unbanCalled {
		t.Fatalf("code=%d called=%v", w.Code, f.unbanCalled)
	}
}

func TestUnban_MissingIdentity_Returns401(t *testing.T) {
	w := do(t, newRouter(&fakeModeration{}), http.MethodDelete, bansPath(uuid.New())+"/banned-1", "", "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("code=%d", w.Code)
	}
}

func TestUnban_DomainErrorsMapToHTTP(t *testing.T) {
	cases := map[string]struct {
		err    error
		status int
		reason string
	}{
		"missing BAN_MEMBERS (CA2)":  {model.ErrForbiddenMissingPermission, http.StatusForbidden, "missing_permission"},
		"not banned":                 {model.ErrBanNotFound, http.StatusNotFound, "ban_not_found"},
		"server not found":           {model.ErrServerNotFound, http.StatusNotFound, ""},
		"unexpected error is hidden": {errors.New("pq: boom"), http.StatusInternalServerError, ""},
	}
	for name, tc := range cases {
		w := do(t, newRouter(&fakeModeration{unbanErr: tc.err}), http.MethodDelete, bansPath(uuid.New())+"/banned-1", testUser, "")
		if w.Code != tc.status {
			t.Errorf("%s: code=%d, want %d", name, w.Code, tc.status)
			continue
		}
		if tc.reason != "" {
			if e := decodeError(t, w); e.Error.Details["reason"] != tc.reason {
				t.Errorf("%s: details = %v", name, e.Error.Details)
			}
		}
	}
}

func TestListBans_WithoutPermission_Returns403(t *testing.T) {
	w := do(t, newRouter(&fakeModeration{listErr: model.ErrForbiddenMissingPermission}), http.MethodGet, bansPath(uuid.New()), testUser, "")
	if w.Code != http.StatusForbidden {
		t.Fatalf("code=%d", w.Code)
	}
}
