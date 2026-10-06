package channelperm_handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/handler"
	"github.com/discordia-grupo01/servers/internal/middleware"
	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/channelperm"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/service/channelperm_service"
)

const testUser = "manager-1"

type fakeUseCases struct {
	setResult channelperm.Override
	setErr    error
	setCalled bool
	gotSet    channelperm_service.SetOverrideParams

	deleteErr    error
	deleteCalled bool
	gotDelete    channelperm_service.DeleteOverrideParams

	listResult []channelperm.Override
	listErr    error
	gotList    channelperm_service.ListOverridesParams

	effResult []string
	effErr    error
	gotEff    channelperm_service.EffectivePermissionsParams
}

func (f *fakeUseCases) SetOverride(_ context.Context, p channelperm_service.SetOverrideParams) (channelperm.Override, error) {
	f.setCalled, f.gotSet = true, p
	return f.setResult, f.setErr
}

func (f *fakeUseCases) DeleteOverride(_ context.Context, p channelperm_service.DeleteOverrideParams) error {
	f.deleteCalled, f.gotDelete = true, p
	return f.deleteErr
}

func (f *fakeUseCases) ListOverrides(_ context.Context, p channelperm_service.ListOverridesParams) ([]channelperm.Override, error) {
	f.gotList = p
	return f.listResult, f.listErr
}

func (f *fakeUseCases) EffectivePermissions(_ context.Context, p channelperm_service.EffectivePermissionsParams) ([]string, error) {
	f.gotEff = p
	return f.effResult, f.effErr
}

type okPinger struct{}

func (okPinger) Ping(context.Context) error { return nil }

func newRouter(f *fakeUseCases) *gin.Engine {
	gin.SetMode(gin.TestMode)
	return handler.NewRouter(handler.Deps{
		ChannelPermissions: f,
		Auth:               middleware.AuthConfig{UserIDHeader: "X-User-Id"},
		DB:                 okPinger{},
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

func overridePath(channelID, roleID uuid.UUID) string {
	return "/v1/channels/" + channelID.String() + "/permissions/roles/" + roleID.String()
}

func TestSetOverride_Returns200WithTheOverride(t *testing.T) {
	channelID, roleID := uuid.New(), uuid.New()
	at := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	f := &fakeUseCases{setResult: channelperm.Override{ChannelID: channelID, RoleID: roleID, Allow: role.PermManageMessages, Deny: role.PermSendMessages, UpdatedAt: at}}

	w := do(t, newRouter(f), http.MethodPut, overridePath(channelID, roleID), testUser, `{"allow":["MANAGE_MESSAGES"],"deny":["SEND_MESSAGES"]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if f.gotSet.ChannelID != channelID || f.gotSet.RoleID != roleID || f.gotSet.ActorUserID != testUser ||
		!reflect.DeepEqual(f.gotSet.Allow, []string{"MANAGE_MESSAGES"}) || !reflect.DeepEqual(f.gotSet.Deny, []string{"SEND_MESSAGES"}) {
		t.Errorf("service got %+v", f.gotSet)
	}
	var got map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got["channel_id"] != channelID.String() || got["role_id"] != roleID.String() ||
		!reflect.DeepEqual(got["allow"], []any{"MANAGE_MESSAGES"}) || !reflect.DeepEqual(got["deny"], []any{"SEND_MESSAGES"}) ||
		got["updated_at"] != "2026-10-04T12:00:00Z" {
		t.Errorf("body = %v", got)
	}
}

// An empty result means the override was removed: 204 with no body.
func TestSetOverride_EmptyResult_Returns204(t *testing.T) {
	f := &fakeUseCases{}
	w := do(t, newRouter(f), http.MethodPut, overridePath(uuid.New(), uuid.New()), testUser, `{"allow":[],"deny":[]}`)
	if w.Code != http.StatusNoContent || w.Body.Len() != 0 {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestSetOverride_BadRequests(t *testing.T) {
	cases := map[string]struct{ path, body string }{
		"invalid json":       {overridePath(uuid.New(), uuid.New()), `{"allow":`},
		"invalid channel id": {"/v1/channels/nope/permissions/roles/" + uuid.NewString(), `{}`},
		"invalid role id":    {"/v1/channels/" + uuid.NewString() + "/permissions/roles/nope", `{}`},
	}
	for name, tc := range cases {
		f := &fakeUseCases{}
		w := do(t, newRouter(f), http.MethodPut, tc.path, testUser, tc.body)
		if w.Code != http.StatusBadRequest || f.setCalled {
			t.Errorf("%s: code=%d called=%v", name, w.Code, f.setCalled)
		}
	}
}

func TestSetOverride_DomainErrorsMapToHTTP(t *testing.T) {
	cases := map[string]struct {
		err         error
		status      int
		reason      string
		permissions []any
	}{
		"missing MANAGE_ROLES (CA2)": {model.ErrForbiddenNotAdmin, http.StatusForbidden, "", nil},
		"role above own":             {model.ErrRoleAboveOwn, http.StatusForbidden, "role_above_own", nil},
		"allow not held":             {&model.PermissionNotHeldError{Permissions: []string{"MANAGE_MESSAGES"}}, http.StatusForbidden, "permission_not_held", []any{"MANAGE_MESSAGES"}},
		"not overridable":            {&model.PermissionNotOverridableError{Permissions: []string{"KICK_MEMBERS"}}, http.StatusBadRequest, "permission_not_overridable", []any{"KICK_MEMBERS"}},
		"allowed and denied":         {&model.PermissionConflictError{Permissions: []string{"SEND_MESSAGES"}}, http.StatusBadRequest, "permission_conflict", []any{"SEND_MESSAGES"}},
		"unknown permission":         {&model.InvalidPermissionError{Value: "FLY"}, http.StatusBadRequest, "", nil},
		"role of another server":     {model.ErrRoleServerMismatch, http.StatusBadRequest, "", nil},
		"channel not found":          {model.ErrChannelNotFound, http.StatusNotFound, "", nil},
		"role not found":             {model.ErrRoleNotFound, http.StatusNotFound, "", nil},
		"unexpected error is hidden": {errors.New("pq: boom"), http.StatusInternalServerError, "", nil},
	}
	for name, tc := range cases {
		w := do(t, newRouter(&fakeUseCases{setErr: tc.err}), http.MethodPut, overridePath(uuid.New(), uuid.New()), testUser, `{"deny":["SEND_MESSAGES"]}`)
		if w.Code != tc.status {
			t.Errorf("%s: code=%d, want %d", name, w.Code, tc.status)
			continue
		}
		e := decodeError(t, w)
		if tc.reason != "" && e.Error.Details["reason"] != tc.reason {
			t.Errorf("%s: details = %v", name, e.Error.Details)
		}
		if tc.permissions != nil && !reflect.DeepEqual(e.Error.Details["permissions"], tc.permissions) {
			t.Errorf("%s: details = %v", name, e.Error.Details)
		}
	}
}

func TestDeleteOverride(t *testing.T) {
	channelID, roleID := uuid.New(), uuid.New()
	f := &fakeUseCases{}
	w := do(t, newRouter(f), http.MethodDelete, overridePath(channelID, roleID), testUser, "")
	if w.Code != http.StatusNoContent || f.gotDelete.ChannelID != channelID || f.gotDelete.RoleID != roleID || f.gotDelete.ActorUserID != testUser {
		t.Fatalf("code=%d got=%+v", w.Code, f.gotDelete)
	}

	w = do(t, newRouter(&fakeUseCases{deleteErr: model.ErrChannelOverrideNotFound}), http.MethodDelete, overridePath(channelID, roleID), testUser, "")
	if w.Code != http.StatusNotFound || decodeError(t, w).Error.Details["reason"] != "channel_override_not_found" {
		t.Errorf("no override: code=%d body=%s", w.Code, w.Body.String())
	}

	f = &fakeUseCases{}
	w = do(t, newRouter(f), http.MethodDelete, "/v1/channels/nope/permissions/roles/"+roleID.String(), testUser, "")
	if w.Code != http.StatusBadRequest || f.deleteCalled {
		t.Errorf("invalid channel id: code=%d called=%v", w.Code, f.deleteCalled)
	}
}

func TestListOverrides(t *testing.T) {
	channelID, everyoneID := uuid.New(), uuid.New()
	f := &fakeUseCases{listResult: []channelperm.Override{{ChannelID: channelID, RoleID: everyoneID, Deny: role.PermViewChannels}}}
	w := do(t, newRouter(f), http.MethodGet, "/v1/channels/"+channelID.String()+"/permissions", testUser, "")
	if w.Code != http.StatusOK || f.gotList.ChannelID != channelID || f.gotList.ActorUserID != testUser {
		t.Fatalf("code=%d got=%+v", w.Code, f.gotList)
	}
	var got []map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if len(got) != 1 || got[0]["role_id"] != everyoneID.String() || !reflect.DeepEqual(got[0]["allow"], []any{}) ||
		!reflect.DeepEqual(got[0]["deny"], []any{"VIEW_CHANNELS"}) {
		t.Errorf("body = %v (allow must be an empty array, not null)", got)
	}

	w = do(t, newRouter(&fakeUseCases{}), http.MethodGet, "/v1/channels/"+channelID.String()+"/permissions", testUser, "")
	if w.Body.String() != "[]" {
		t.Errorf("no overrides: body = %s, want []", w.Body.String())
	}

	w = do(t, newRouter(&fakeUseCases{listErr: model.ErrForbiddenNotAdmin}), http.MethodGet, "/v1/channels/"+channelID.String()+"/permissions", testUser, "")
	if w.Code != http.StatusForbidden {
		t.Errorf("CA2: code=%d", w.Code)
	}
}

// Same shape and codes as GET /servers/{id}/members/{userId}/permissions, so
// messaging only has to change the URL.
func TestEffectivePermissions(t *testing.T) {
	channelID := uuid.New()
	path := "/v1/channels/" + channelID.String() + "/members/member-1/permissions"
	f := &fakeUseCases{effResult: []string{"VIEW_CHANNELS"}}
	w := do(t, newRouter(f), http.MethodGet, path, testUser, "")
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if f.gotEff.ChannelID != channelID || f.gotEff.TargetUserID != "member-1" || f.gotEff.ActorUserID != testUser {
		t.Errorf("service got %+v", f.gotEff)
	}
	var got map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if !reflect.DeepEqual(got["permissions"], []any{"VIEW_CHANNELS"}) || got["user_id"] != "member-1" || got["channel_id"] != channelID.String() {
		t.Errorf("body = %v", got)
	}

	cases := map[string]struct {
		err    error
		status int
	}{
		"caller not a member": {model.ErrForbiddenNotMember, http.StatusForbidden},
		"target not a member": {model.ErrMemberNotFound, http.StatusNotFound},
		"channel not found":   {model.ErrChannelNotFound, http.StatusNotFound},
	}
	for name, tc := range cases {
		if w := do(t, newRouter(&fakeUseCases{effErr: tc.err}), http.MethodGet, path, testUser, ""); w.Code != tc.status {
			t.Errorf("%s: code=%d, want %d", name, w.Code, tc.status)
		}
	}
	if w := do(t, newRouter(&fakeUseCases{}), http.MethodGet, path, "", ""); w.Code != http.StatusUnauthorized {
		t.Errorf("no identity: code=%d", w.Code)
	}
}
