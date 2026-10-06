package mention_handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/handler"
	"github.com/discordia-grupo01/servers/internal/middleware"
	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/mention"
	"github.com/discordia-grupo01/servers/internal/service/mention_service"
)

const testUser = "author-1"

type fakeMentions struct {
	called bool
	got    mention_service.ResolveParams
	result mention.Resolution
	err    error
}

func (f *fakeMentions) Resolve(_ context.Context, p mention_service.ResolveParams) (mention.Resolution, error) {
	f.called, f.got = true, p
	return f.result, f.err
}

type okPinger struct{}

func (okPinger) Ping(context.Context) error { return nil }

func newRouter(f *fakeMentions) *gin.Engine {
	gin.SetMode(gin.TestMode)
	return handler.NewRouter(handler.Deps{
		Mentions: f,
		Auth:     middleware.AuthConfig{UserIDHeader: "X-User-Id"},
		DB:       okPinger{},
	})
}

func post(t *testing.T, r http.Handler, path, user, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if user != "" {
		req.Header.Set("X-User-Id", user)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func resolvePath(serverID uuid.UUID) string {
	return "/v1/servers/" + serverID.String() + "/mentions/resolve"
}

type errorBody struct {
	Error struct {
		Code    int            `json:"code"`
		Details map[string]any `json:"details"`
	} `json:"error"`
}

func decodeError(t *testing.T, w *httptest.ResponseRecorder) errorBody {
	t.Helper()
	var body errorBody
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body: %v (%s)", err, w.Body.String())
	}
	return body
}

func TestResolve_OK_PassesTheMentionsAndReturnsTheResolution(t *testing.T) {
	serverID, channelID, roleID := uuid.New(), uuid.New(), uuid.New()
	f := &fakeMentions{result: mention.Resolution{
		UserIDs:      []string{"alice"},
		RoleIDs:      []uuid.UUID{roleID},
		Everyone:     true,
		RecipientIDs: []string{"alice", "bob"},
	}}
	body := `{"channel_id":"` + channelID.String() + `","user_ids":["alice"],"role_ids":["` + roleID.String() + `"],"everyone":true}`

	w := post(t, newRouter(f), resolvePath(serverID), testUser, body)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	want := mention_service.ResolveParams{
		ServerID: serverID, ChannelID: channelID, ActorUserID: testUser,
		UserIDs: []string{"alice"}, RoleIDs: []uuid.UUID{roleID}, Everyone: true,
	}
	got := f.got
	if got.ServerID != want.ServerID || got.ChannelID != want.ChannelID || got.ActorUserID != want.ActorUserID ||
		len(got.UserIDs) != 1 || got.UserIDs[0] != "alice" || len(got.RoleIDs) != 1 || got.RoleIDs[0] != roleID || !got.Everyone {
		t.Errorf("params = %+v, want %+v", got, want)
	}

	var resp mention.ResolutionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Everyone || len(resp.RecipientIDs) != 2 || len(resp.RoleIDs) != 1 || resp.RoleIDs[0] != roleID.String() {
		t.Errorf("response = %+v", resp)
	}
}

func TestResolve_EmptyResolution_ListsAreEmptyArrays(t *testing.T) {
	f := &fakeMentions{}
	w := post(t, newRouter(f), resolvePath(uuid.New()), testUser, `{"channel_id":"`+uuid.NewString()+`"}`)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	want := `{"user_ids":[],"role_ids":[],"everyone":false,"recipient_ids":[]}`
	if got := w.Body.String(); got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
}

func TestResolve_BadRequests(t *testing.T) {
	cases := map[string]struct {
		path      string
		body      string
		wantField string
	}{
		"invalid server id":  {"/v1/servers/not-a-uuid/mentions/resolve", `{"channel_id":"` + uuid.NewString() + `"}`, "serverId"},
		"missing channel_id": {resolvePath(uuid.New()), `{"user_ids":["alice"]}`, "channel_id"},
		"invalid channel_id": {resolvePath(uuid.New()), `{"channel_id":"nope"}`, "channel_id"},
		"invalid role id":    {resolvePath(uuid.New()), `{"channel_id":"` + uuid.NewString() + `","role_ids":["nope"]}`, "role_ids"},
		"invalid json":       {resolvePath(uuid.New()), `{"channel_id":`, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := &fakeMentions{}
			w := post(t, newRouter(f), tc.path, testUser, tc.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %s)", w.Code, w.Body.String())
			}
			if f.called {
				t.Error("use case should not run on a bad request")
			}
			if tc.wantField != "" {
				if got := decodeError(t, w).Error.Details["field"]; got != tc.wantField {
					t.Errorf("details.field = %v, want %s", got, tc.wantField)
				}
			}
		})
	}
}

func TestResolve_DomainErrors_AreMapped(t *testing.T) {
	cases := map[string]struct {
		err        error
		wantStatus int
		wantField  string
	}{
		"not a member":     {model.ErrForbiddenNotMember, http.StatusForbidden, ""},
		"server missing":   {model.ErrServerNotFound, http.StatusNotFound, ""},
		"channel missing":  {model.ErrChannelNotFound, http.StatusNotFound, ""},
		"foreign channel":  {model.ErrChannelServerMismatch, http.StatusBadRequest, ""},
		"too many users":   {mention.ErrTooManyUsers, http.StatusBadRequest, "user_ids"},
		"too many roles":   {mention.ErrTooManyRoles, http.StatusBadRequest, "role_ids"},
		"no authenticated": {model.ErrUnauthorized, http.StatusUnauthorized, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := &fakeMentions{err: tc.err}
			w := post(t, newRouter(f), resolvePath(uuid.New()), testUser, `{"channel_id":"`+uuid.NewString()+`"}`)
			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", w.Code, tc.wantStatus, w.Body.String())
			}
			if tc.wantField != "" {
				body := decodeError(t, w)
				if body.Error.Details["field"] != tc.wantField || body.Error.Details["reason"] != "too_many" {
					t.Errorf("details = %v, want field=%s reason=too_many", body.Error.Details, tc.wantField)
				}
			}
		})
	}
}

func TestResolve_WithoutIdentity_IsUnauthorized(t *testing.T) {
	f := &fakeMentions{}
	w := post(t, newRouter(f), resolvePath(uuid.New()), "", `{"channel_id":"`+uuid.NewString()+`"}`)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if f.called {
		t.Error("use case should not run without identity")
	}
}
