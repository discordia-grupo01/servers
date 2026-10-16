package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/handler"
	"github.com/discordia-grupo01/servers/internal/middleware"
	"github.com/discordia-grupo01/servers/internal/service/channel_service"
)

type fakeSuspensions map[string]bool

func (f fakeSuspensions) IsSuspended(_ context.Context, userID string) (bool, error) {
	return f[userID], nil
}

// countingVoice records how many times the voice-access use case ran, to prove
// a suspended user never reaches it.
type countingVoice struct{ calls int }

func (v *countingVoice) AuthorizeVoice(_ context.Context, channelID uuid.UUID, _ string) (channel_service.VoiceAccess, error) {
	v.calls++
	return channel_service.VoiceAccess{ChannelID: channelID}, nil
}

func suspendedRouter(t *testing.T, suspended fakeSuspensions, voice *countingVoice) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	guard := middleware.NewSuspensionGuard(suspended, time.Minute)
	return handler.NewRouter(handler.Deps{
		VoiceAccess:     voice,
		Auth:            middleware.AuthConfig{UserIDHeader: "X-User-Id"},
		RejectSuspended: guard.RejectSuspended(),
		DB:              okPinger{},
	})
}

func doAs(r http.Handler, method, path, user string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if user != "" {
		req.Header.Set("X-User-Id", user)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// Every authenticated /v1 route answers 403 ACCOUNT_SUSPENDED to a suspended
// user before running anything: creating a server, inviting, joining, roles,
// channels, moderation and the voice token.
func TestRouter_SuspendedUserIsRejectedOnRepresentativeRoutes(t *testing.T) {
	voice := &countingVoice{}
	r := suspendedRouter(t, fakeSuspensions{"banned-user": true}, voice)
	serverID, channelID := uuid.NewString(), uuid.NewString()

	routes := []struct{ name, method, path string }{
		{"create server", http.MethodPost, "/v1/servers"},
		{"list my servers", http.MethodGet, "/v1/servers"},
		{"create invitation", http.MethodPost, "/v1/servers/" + serverID + "/invites"},
		{"join with invitation", http.MethodPost, "/v1/invites/some-code/join"},
		{"create role", http.MethodPost, "/v1/servers/" + serverID + "/roles"},
		{"list members", http.MethodGet, "/v1/servers/" + serverID + "/members"},
		{"create channel", http.MethodPost, "/v1/servers/" + serverID + "/channels"},
		{"ban member", http.MethodPost, "/v1/servers/" + serverID + "/bans"},
		{"voice access", http.MethodGet, "/v1/channels/" + channelID + "/voice-access"},
	}
	for _, rt := range routes {
		t.Run(rt.name, func(t *testing.T) {
			w := doAs(r, rt.method, rt.path, "banned-user")
			if w.Code != http.StatusForbidden {
				t.Fatalf("code = %d, want 403; body = %s", w.Code, w.Body.String())
			}
			var e struct {
				Error struct {
					Code    int            `json:"code"`
					Details map[string]any `json:"details"`
				} `json:"error"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
				t.Fatalf("bad error body %q: %v", w.Body.String(), err)
			}
			if e.Error.Code != http.StatusForbidden || e.Error.Details["error_code"] != "ACCOUNT_SUSPENDED" {
				t.Errorf("error = %+v, want code 403 / details.error_code ACCOUNT_SUSPENDED", e.Error)
			}
		})
	}
	if voice.calls != 0 {
		t.Errorf("voice use case ran %d times for a suspended user, want 0", voice.calls)
	}
}

// The voice service trusts voice-access to authorize the LiveKit token: a
// suspended user is denied there, an active one is not.
func TestRouter_VoiceAccessDeniedOnlyToTheSuspendedUser(t *testing.T) {
	voice := &countingVoice{}
	r := suspendedRouter(t, fakeSuspensions{"banned-user": true}, voice)
	path := "/v1/channels/" + uuid.NewString() + "/voice-access"

	if w := doAs(r, http.MethodGet, path, "banned-user"); w.Code != http.StatusForbidden || voice.calls != 0 {
		t.Fatalf("suspended: code = %d, voice calls = %d, want 403 and 0", w.Code, voice.calls)
	}
	if w := doAs(r, http.MethodGet, path, "active-user"); w.Code != http.StatusOK || voice.calls != 1 {
		t.Fatalf("active: code = %d, voice calls = %d, want 200 and 1; body = %s", w.Code, voice.calls, w.Body.String())
	}
}

// Public and operational routes carry no user, so they are never blocked.
func TestRouter_SuspensionDoesNotAffectPublicRoutes(t *testing.T) {
	r := suspendedRouter(t, fakeSuspensions{"banned-user": true}, &countingVoice{})
	for _, path := range []string{"/livez", "/readyz"} {
		if w := doAs(r, http.MethodGet, path, "banned-user"); w.Code != http.StatusOK {
			t.Errorf("%s code = %d, want 200", path, w.Code)
		}
	}
}

// Without the middleware wired (RejectSuspended nil) the routes behave as before.
func TestRouter_WithoutGuardNobodyIsRejected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	voice := &countingVoice{}
	r := handler.NewRouter(handler.Deps{VoiceAccess: voice, Auth: middleware.AuthConfig{UserIDHeader: "X-User-Id"}, DB: okPinger{}})
	if w := doAs(r, http.MethodGet, "/v1/channels/"+uuid.NewString()+"/voice-access", "anyone"); w.Code != http.StatusOK {
		t.Errorf("code = %d, want 200", w.Code)
	}
}
