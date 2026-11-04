package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/handler"
	"github.com/discordia-grupo01/servers/internal/middleware"
	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/service/membership_service"
)

// suspendedServers answers that every server, channel, category, role and
// invitation belongs to a suspended server -- or to an active one.
type suspendedServers struct{ suspended bool }

func (s suspendedServers) SuspendedByServer(context.Context, uuid.UUID) (bool, bool, error) {
	return s.suspended, true, nil
}
func (s suspendedServers) SuspendedByChannel(context.Context, uuid.UUID) (bool, bool, error) {
	return s.suspended, true, nil
}
func (s suspendedServers) SuspendedByCategory(context.Context, uuid.UUID) (bool, bool, error) {
	return s.suspended, true, nil
}
func (s suspendedServers) SuspendedByRole(context.Context, uuid.UUID) (bool, bool, error) {
	return s.suspended, true, nil
}
func (s suspendedServers) SuspendedByInvite(context.Context, string) (bool, bool, error) {
	return s.suspended, true, nil
}

// leaving records the leave requests that got through to the use case.
type leaving struct {
	calls []membership_service.LeaveParams
}

func (l *leaving) LeaveServer(_ context.Context, p membership_service.LeaveParams) error {
	l.calls = append(l.calls, p)
	return nil
}

func (l *leaving) ListMembers(context.Context, membership_service.ListMembersParams) ([]model.Member, int, error) {
	return nil, 0, nil
}

func suspendedServerRouter(t *testing.T, suspended bool, voice *countingVoice, membership *leaving) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	guard := middleware.NewServerSuspensionGuard(suspendedServers{suspended: suspended}, time.Minute)
	return handler.NewRouter(handler.Deps{
		VoiceAccess:           voice,
		Membership:            membership,
		Auth:                  middleware.AuthConfig{UserIDHeader: "X-User-Id"},
		RejectSuspendedServer: guard.RejectSuspendedServer(),
		DB:                    okPinger{},
	})
}

var routeParam = regexp.MustCompile(`:(\w+)`)

// concretePath fills each :param of a route with a value of the right shape.
func concretePath(route string) string {
	return routeParam.ReplaceAllStringFunc(route, func(p string) string {
		switch p[1:] {
		case "code":
			return "some-code"
		case "userId":
			return "someone-else"
		default:
			return uuid.NewString()
		}
	})
}

func assertServerSuspended(t *testing.T, w *httptest.ResponseRecorder, what string) {
	t.Helper()
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), `"error_code":"SERVER_SUSPENDED"`) {
		t.Errorf("%s: code = %d, body = %s, want 403 SERVER_SUSPENDED", what, w.Code, w.Body.String())
	}
}

// The test that keeps the guard honest as routes are added: every authenticated
// /v1 route of the real router must either be rejected for a suspended server
// or be one of the few that name no server. A new route that slips past the
// guard fails here.
func TestRouter_EveryRouteOnASuspendedServerIsRejectedExceptTheDocumentedOnes(t *testing.T) {
	voice, membership := &countingVoice{}, &leaving{}
	r := suspendedServerRouter(t, true, voice, membership)

	// Routes that, by design, don't answer SERVER_SUSPENDED.
	namesNoServer := map[string]bool{"GET /v1/servers": true, "POST /v1/servers": true}
	public := map[string]bool{"GET /v1/servers/:serverId/icon": true, "GET /v1/servers/:serverId/banner": true}
	leaveRoute := "DELETE /v1/servers/:serverId/members/:userId"

	var checked []string
	for _, route := range r.Routes() {
		key := route.Method + " " + route.Path
		if !strings.HasPrefix(route.Path, "/v1/") || namesNoServer[key] || public[key] {
			continue
		}
		t.Run(key, func(t *testing.T) {
			w := doAs(r, route.Method, concretePath(route.Path), "owner")
			assertServerSuspended(t, w, key)
		})
		checked = append(checked, key)
	}
	sort.Strings(checked)
	if len(checked) < 40 {
		t.Fatalf("only %d routes were checked; the router lost routes or the test stopped finding them: %v", len(checked), checked)
	}
	if !slices.Contains(checked, leaveRoute) {
		t.Errorf("%s was not checked", leaveRoute)
	}
	if voice.calls != 0 || len(membership.calls) != 0 {
		t.Errorf("a use case ran (voice=%d, leave=%d) for a suspended server", voice.calls, len(membership.calls))
	}
}

func TestRouter_VoiceAccessIsDeniedOnASuspendedServer(t *testing.T) {
	voice := &countingVoice{}
	r := suspendedServerRouter(t, true, voice, &leaving{})

	w := doAs(r, http.MethodGet, "/v1/channels/"+uuid.NewString()+"/voice-access", "member")

	assertServerSuspended(t, w, "voice-access")
	if voice.calls != 0 {
		t.Errorf("voice use case ran %d times, want 0: nobody gets a LiveKit token for a suspended server", voice.calls)
	}
}

// Leaving is allowed for oneself, and only for oneself.
func TestRouter_AMemberCanLeaveASuspendedServerButNotRemoveOthers(t *testing.T) {
	membership := &leaving{}
	r := suspendedServerRouter(t, true, &countingVoice{}, membership)
	serverID := uuid.NewString()

	w := doAs(r, http.MethodDelete, "/v1/servers/"+serverID+"/members/alice", "alice")
	if w.Code == http.StatusForbidden {
		t.Fatalf("leaving: code = %d, body = %s, want it to reach the use case", w.Code, w.Body.String())
	}
	if len(membership.calls) != 1 || membership.calls[0].ActorUserID != "alice" || membership.calls[0].TargetUserID != "alice" {
		t.Fatalf("leave calls = %+v, want one for alice", membership.calls)
	}

	assertServerSuspended(t, doAs(r, http.MethodDelete, "/v1/servers/"+serverID+"/members/bob", "alice"), "removing someone else")
	if len(membership.calls) != 1 {
		t.Errorf("leave calls = %d, want still 1", len(membership.calls))
	}
}

// With every server active the guard changes nothing.
func TestRouter_ActiveServersAreNotAffected(t *testing.T) {
	voice := &countingVoice{}
	r := suspendedServerRouter(t, false, voice, &leaving{})

	if w := doAs(r, http.MethodGet, "/v1/channels/"+uuid.NewString()+"/voice-access", "member"); w.Code != http.StatusOK || voice.calls != 1 {
		t.Fatalf("code = %d, voice calls = %d, want 200 and 1; body = %s", w.Code, voice.calls, w.Body.String())
	}
}

// The account guard runs first: someone who is suspended themselves is told so.
func TestRouter_AccountSuspensionWinsOverServerSuspension(t *testing.T) {
	gin.SetMode(gin.TestMode)
	accounts := middleware.NewSuspensionGuard(fakeSuspensions{"banned-user": true}, time.Minute)
	servers := middleware.NewServerSuspensionGuard(suspendedServers{suspended: true}, time.Minute)
	r := handler.NewRouter(handler.Deps{
		VoiceAccess:           &countingVoice{},
		Auth:                  middleware.AuthConfig{UserIDHeader: "X-User-Id"},
		RejectSuspended:       accounts.RejectSuspended(),
		RejectSuspendedServer: servers.RejectSuspendedServer(),
		DB:                    okPinger{},
	})

	w := doAs(r, http.MethodGet, "/v1/channels/"+uuid.NewString()+"/voice-access", "banned-user")

	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), `"error_code":"ACCOUNT_SUSPENDED"`) {
		t.Errorf("code = %d, body = %s, want 403 ACCOUNT_SUSPENDED", w.Code, w.Body.String())
	}
}

// Creating a server, listing mine and the public images keep working.
func TestRouter_SuspendedServersDoNotBlockRoutesThatNameNoServer(t *testing.T) {
	r := suspendedServerRouter(t, true, &countingVoice{}, &leaving{})
	for _, path := range []string{"/livez", "/readyz"} {
		if w := doAs(r, http.MethodGet, path, ""); w.Code != http.StatusOK {
			t.Errorf("%s code = %d, want 200", path, w.Code)
		}
	}
}
