package middleware_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/middleware"
)

// fakeServerChecker answers by resource id; an id it doesn't know is "not found".
type fakeServerChecker struct {
	suspended map[string]bool // keyed by the resource id or invitation code
	err       error
	calls     int
	last      string
}

func (f *fakeServerChecker) lookup(kind, id string) (bool, bool, error) {
	f.calls++
	f.last = kind
	if f.err != nil {
		return false, false, f.err
	}
	s, ok := f.suspended[id]
	return s, ok, nil
}

func (f *fakeServerChecker) SuspendedByServer(_ context.Context, id uuid.UUID) (bool, bool, error) {
	return f.lookup("server", id.String())
}
func (f *fakeServerChecker) SuspendedByChannel(_ context.Context, id uuid.UUID) (bool, bool, error) {
	return f.lookup("channel", id.String())
}
func (f *fakeServerChecker) SuspendedByCategory(_ context.Context, id uuid.UUID) (bool, bool, error) {
	return f.lookup("category", id.String())
}
func (f *fakeServerChecker) SuspendedByRole(_ context.Context, id uuid.UUID) (bool, bool, error) {
	return f.lookup("role", id.String())
}
func (f *fakeServerChecker) SuspendedByInvite(_ context.Context, code string) (bool, bool, error) {
	return f.lookup("invite", code)
}

func newServerGuard(checker *fakeServerChecker) (*middleware.ServerSuspensionGuard, *clock) {
	clk := &clock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	return middleware.NewServerSuspensionGuard(checker, ttl).WithClock(clk.Now), clk
}

// serverEngine registers, behind Auth and the server guard, one route per way a
// request can name a server, plus the ones that name none.
func serverEngine(guard *middleware.ServerSuspensionGuard, reached *int) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	v1 := r.Group("/v1")
	v1.Use(middleware.Auth(middleware.AuthConfig{UserIDHeader: "X-User-Id"}), guard.RejectSuspendedServer())
	ok := func(c *gin.Context) { *reached++; c.String(http.StatusOK, "ok") }
	v1.GET("/servers", ok)
	v1.POST("/servers", ok)
	v1.GET("/servers/:serverId", ok)
	v1.PATCH("/servers/:serverId", ok)
	v1.DELETE("/servers/:serverId", ok)
	v1.POST("/servers/:serverId/channels", ok)
	v1.DELETE("/servers/:serverId/members/:userId", ok)
	v1.DELETE("/servers/:serverId/members/:userId/roles/:roleId", ok)
	v1.PATCH("/channels/:channelId", ok)
	v1.PATCH("/categories/:categoryId", ok)
	v1.PATCH("/roles/:roleId", ok)
	v1.POST("/invites/:code/join", ok)
	return r
}

func doServer(r http.Handler, method, path, user string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("X-User-Id", user)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func mustBeServerSuspended(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if w.Code != http.StatusForbidden {
		t.Fatalf("code = %d, want 403; body = %s", w.Code, w.Body.String())
	}
	var e errorBody
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatalf("bad error body %q: %v", w.Body.String(), err)
	}
	if e.Error.Code != http.StatusForbidden || e.Error.Details["error_code"] != "SERVER_SUSPENDED" {
		t.Errorf("error = %+v, want code 403 / details.error_code SERVER_SUSPENDED", e.Error)
	}
}

// Whichever way the route names the server -- the server itself, one of its
// channels, categories or roles, or an invitation code -- a suspended server is
// rejected, for the owner as much as for anyone else.
func TestServerGuard_RejectsEveryWayOfNamingASuspendedServer(t *testing.T) {
	id := uuid.NewString()
	checker := &fakeServerChecker{suspended: map[string]bool{id: true, "code-1": true}}
	guard, _ := newServerGuard(checker)
	reached := 0
	r := serverEngine(guard, &reached)

	routes := []struct{ name, method, path, kind string }{
		{"read the server", http.MethodGet, "/v1/servers/" + id, "server"},
		{"edit the server", http.MethodPatch, "/v1/servers/" + id, "server"},
		{"delete the server", http.MethodDelete, "/v1/servers/" + id, "server"},
		{"create a channel", http.MethodPost, "/v1/servers/" + id + "/channels", "server"},
		{"remove someone else", http.MethodDelete, "/v1/servers/" + id + "/members/someone-else", "server"},
		{"remove a role from a member, even their own", http.MethodDelete, "/v1/servers/" + id + "/members/owner/roles/" + uuid.NewString(), "server"},
		{"edit a channel", http.MethodPatch, "/v1/channels/" + id, "channel"},
		{"edit a category", http.MethodPatch, "/v1/categories/" + id, "category"},
		{"edit a role", http.MethodPatch, "/v1/roles/" + id, "role"},
		{"join with an invitation", http.MethodPost, "/v1/invites/code-1/join", "invite"},
	}
	for _, rt := range routes {
		t.Run(rt.name, func(t *testing.T) {
			checker.last = ""
			guard.Reset() // every subtest must reach the checker, not the cache
			mustBeServerSuspended(t, doServer(r, rt.method, rt.path, "owner"))
			if checker.last != rt.kind {
				t.Errorf("looked the server up by %q, want %q", checker.last, rt.kind)
			}
		})
	}
	if reached != 0 {
		t.Errorf("a handler ran %d times for a suspended server, want 0", reached)
	}
}

func TestServerGuard_LetsThroughActiveServers(t *testing.T) {
	id := uuid.NewString()
	checker := &fakeServerChecker{suspended: map[string]bool{id: false, "code-1": false}}
	guard, _ := newServerGuard(checker)
	reached := 0
	r := serverEngine(guard, &reached)

	for _, rt := range []struct{ method, path string }{
		{http.MethodGet, "/v1/servers/" + id},
		{http.MethodPatch, "/v1/channels/" + id},
		{http.MethodPatch, "/v1/roles/" + id},
		{http.MethodPost, "/v1/invites/code-1/join"},
	} {
		if w := doServer(r, rt.method, rt.path, "owner"); w.Code != http.StatusOK {
			t.Errorf("%s %s = %d, want 200", rt.method, rt.path, w.Code)
		}
	}
}

// Creating a server and listing mine name no server, so there is nothing to check.
func TestServerGuard_RoutesThatNameNoServerAreNotChecked(t *testing.T) {
	checker := &fakeServerChecker{}
	guard, _ := newServerGuard(checker)
	reached := 0
	r := serverEngine(guard, &reached)

	for _, method := range []string{http.MethodGet, http.MethodPost} {
		if w := doServer(r, method, "/v1/servers", "owner"); w.Code != http.StatusOK {
			t.Errorf("%s /v1/servers = %d, want 200", method, w.Code)
		}
	}
	if checker.calls != 0 {
		t.Errorf("checker called %d times, want 0", checker.calls)
	}
}

// Leaving is the one thing a member can still do, but only for themselves.
func TestServerGuard_AMemberCanLeaveASuspendedServer(t *testing.T) {
	id := uuid.NewString()
	checker := &fakeServerChecker{suspended: map[string]bool{id: true}}
	guard, _ := newServerGuard(checker)
	reached := 0
	r := serverEngine(guard, &reached)

	if w := doServer(r, http.MethodDelete, "/v1/servers/"+id+"/members/alice", "alice"); w.Code != http.StatusOK {
		t.Fatalf("leaving: code = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	if checker.calls != 0 {
		t.Errorf("leaving looked the server up %d times, want 0", checker.calls)
	}
	mustBeServerSuspended(t, doServer(r, http.MethodDelete, "/v1/servers/"+id+"/members/bob", "alice"))
}

// A malformed id or a resource that doesn't exist is not this guard's business:
// the handler answers 400 or 404.
func TestServerGuard_MalformedOrUnknownResourcesPassThrough(t *testing.T) {
	checker := &fakeServerChecker{suspended: map[string]bool{}}
	guard, _ := newServerGuard(checker)
	reached := 0
	r := serverEngine(guard, &reached)

	if w := doServer(r, http.MethodGet, "/v1/servers/not-a-uuid", "owner"); w.Code != http.StatusOK {
		t.Errorf("malformed id: code = %d, want 200 (left to the handler)", w.Code)
	}
	if checker.calls != 0 {
		t.Errorf("a malformed id reached the checker %d times, want 0", checker.calls)
	}
	if w := doServer(r, http.MethodGet, "/v1/servers/"+uuid.NewString(), "owner"); w.Code != http.StatusOK {
		t.Errorf("unknown server: code = %d, want 200 (left to the handler)", w.Code)
	}
	if w := doServer(r, http.MethodPost, "/v1/invites/no-such-code/join", "owner"); w.Code != http.StatusOK {
		t.Errorf("unknown invitation: code = %d, want 200 (left to the handler)", w.Code)
	}
}

// If the state can't be read the request is refused, never let through, and the
// failure is not cached.
func TestServerGuard_FailsClosedWith503AndDoesNotCacheTheFailure(t *testing.T) {
	id := uuid.NewString()
	checker := &fakeServerChecker{err: errors.New("db down")}
	guard, _ := newServerGuard(checker)
	reached := 0
	r := serverEngine(guard, &reached)

	if w := doServer(r, http.MethodGet, "/v1/servers/"+id, "owner"); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("code = %d, want 503", w.Code)
	}
	checker.err = nil
	checker.suspended = map[string]bool{id: true}
	mustBeServerSuspended(t, doServer(r, http.MethodGet, "/v1/servers/"+id, "owner"))
	if reached != 0 {
		t.Errorf("a handler ran %d times, want 0", reached)
	}
}

func TestServerGuard_CachesWithinTheTTLAndRefreshesAfterIt(t *testing.T) {
	id := uuid.NewString()
	checker := &fakeServerChecker{suspended: map[string]bool{id: false}}
	guard, clk := newServerGuard(checker)
	reached := 0
	r := serverEngine(guard, &reached)

	doServer(r, http.MethodGet, "/v1/servers/"+id, "owner")
	doServer(r, http.MethodGet, "/v1/servers/"+id, "someone")
	if checker.calls != 1 {
		t.Fatalf("checker calls within the TTL = %d, want 1", checker.calls)
	}

	checker.suspended[id] = true
	if w := doServer(r, http.MethodGet, "/v1/servers/"+id, "owner"); w.Code != http.StatusOK {
		t.Fatalf("still cached: code = %d, want 200", w.Code)
	}
	clk.now = clk.now.Add(ttl + time.Second)
	mustBeServerSuspended(t, doServer(r, http.MethodGet, "/v1/servers/"+id, "owner"))
}

// Applying an event resets the cache, so the change is felt at once on that instance.
func TestServerGuard_ResetMakesAChangeEffectiveAtOnce(t *testing.T) {
	id := uuid.NewString()
	checker := &fakeServerChecker{suspended: map[string]bool{id: false}}
	guard, _ := newServerGuard(checker)
	reached := 0
	r := serverEngine(guard, &reached)

	doServer(r, http.MethodGet, "/v1/servers/"+id, "owner")
	checker.suspended[id] = true
	guard.Reset()
	mustBeServerSuspended(t, doServer(r, http.MethodGet, "/v1/servers/"+id, "owner"))

	checker.suspended[id] = false
	guard.Reset()
	if w := doServer(r, http.MethodGet, "/v1/servers/"+id, "owner"); w.Code != http.StatusOK {
		t.Errorf("after reactivating: code = %d, want 200", w.Code)
	}
}
