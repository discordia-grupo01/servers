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

	"github.com/discordia-grupo01/servers/internal/middleware"
)

type fakeChecker struct {
	suspended map[string]bool
	err       error
	calls     int
}

func (f *fakeChecker) IsSuspended(_ context.Context, userID string) (bool, error) {
	f.calls++
	return f.suspended[userID], f.err
}

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

const ttl = 5 * time.Second

func newGuard(checker *fakeChecker) (*middleware.SuspensionGuard, *clock) {
	clk := &clock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	return middleware.NewSuspensionGuard(checker, ttl).WithClock(clk.Now), clk
}

// serve runs one request as user through Auth + RejectSuspended and returns the
// response; reached tells whether the handler behind the guard ran.
func serve(guard *middleware.SuspensionGuard, user string) (w *httptest.ResponseRecorder, reached bool) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.Auth(middleware.AuthConfig{UserIDHeader: "X-User-Id"}), guard.RejectSuspended())
	r.GET("/x", func(c *gin.Context) {
		reached = true
		c.String(http.StatusOK, "ok")
	})
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("X-User-Id", user)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w, reached
}

type errorBody struct {
	Error struct {
		Code    int            `json:"code"`
		Message string         `json:"message"`
		Details map[string]any `json:"details"`
	} `json:"error"`
}

func TestRejectSuspended_LetsAnActiveUserThrough(t *testing.T) {
	guard, _ := newGuard(&fakeChecker{})
	w, reached := serve(guard, "user-1")
	if w.Code != http.StatusOK || !reached {
		t.Fatalf("code=%d reached=%v body=%s", w.Code, reached, w.Body.String())
	}
}

func TestRejectSuspended_SuspendedUserGets403WithAccountSuspended(t *testing.T) {
	guard, _ := newGuard(&fakeChecker{suspended: map[string]bool{"user-1": true}})
	w, reached := serve(guard, "user-1")
	if w.Code != http.StatusForbidden || reached {
		t.Fatalf("code=%d reached=%v body=%s", w.Code, reached, w.Body.String())
	}
	var e errorBody
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatalf("bad error body %q: %v", w.Body.String(), err)
	}
	if e.Error.Code != http.StatusForbidden || e.Error.Details["error_code"] != "ACCOUNT_SUSPENDED" {
		t.Errorf("error = %+v, want code 403 and details.error_code ACCOUNT_SUSPENDED", e.Error)
	}
}

func TestRejectSuspended_OnlyTheSuspendedUserIsBlocked(t *testing.T) {
	guard, _ := newGuard(&fakeChecker{suspended: map[string]bool{"bad": true}})
	if w, _ := serve(guard, "bad"); w.Code != http.StatusForbidden {
		t.Errorf("suspended user code = %d, want 403", w.Code)
	}
	if w, reached := serve(guard, "good"); w.Code != http.StatusOK || !reached {
		t.Errorf("other user code = %d reached = %v, want 200", w.Code, reached)
	}
}

func TestRejectSuspended_CachesWithinTheTTLAndRefreshesAfterIt(t *testing.T) {
	checker := &fakeChecker{}
	guard, clk := newGuard(checker)

	serve(guard, "user-1")
	serve(guard, "user-1")
	if checker.calls != 1 {
		t.Fatalf("checker calls within the TTL = %d, want 1", checker.calls)
	}

	// Suspended behind the cache's back: still allowed until the entry expires.
	checker.suspended = map[string]bool{"user-1": true}
	clk.now = clk.now.Add(ttl - time.Second)
	if w, _ := serve(guard, "user-1"); w.Code != http.StatusOK {
		t.Errorf("before expiry code = %d, want 200 (cached)", w.Code)
	}
	clk.now = clk.now.Add(2 * time.Second)
	if w, _ := serve(guard, "user-1"); w.Code != http.StatusForbidden {
		t.Errorf("after expiry code = %d, want 403", w.Code)
	}
	if checker.calls != 2 {
		t.Errorf("checker calls = %d, want 2", checker.calls)
	}
}

func TestRejectSuspended_InvalidateAppliesTheChangeAtOnce(t *testing.T) {
	checker := &fakeChecker{}
	guard, _ := newGuard(checker)
	serve(guard, "user-1") // caches "not suspended"

	checker.suspended = map[string]bool{"user-1": true}
	guard.Invalidate("user-1")
	if w, _ := serve(guard, "user-1"); w.Code != http.StatusForbidden {
		t.Fatalf("after invalidate code = %d, want 403", w.Code)
	}

	checker.suspended = map[string]bool{}
	guard.Invalidate("user-1")
	if w, reached := serve(guard, "user-1"); w.Code != http.StatusOK || !reached {
		t.Fatalf("after reactivation code = %d reached = %v, want 200", w.Code, reached)
	}
}

// If the state can't be read, the request is refused instead of letting a
// possibly suspended user through, and the failure is not cached.
func TestRejectSuspended_CheckerFailureReturns503AndIsNotCached(t *testing.T) {
	checker := &fakeChecker{err: errors.New("db down")}
	guard, _ := newGuard(checker)

	w, reached := serve(guard, "user-1")
	if w.Code != http.StatusServiceUnavailable || reached {
		t.Fatalf("code=%d reached=%v, want 503 and not reached", w.Code, reached)
	}

	checker.err = nil
	if w, reached := serve(guard, "user-1"); w.Code != http.StatusOK || !reached {
		t.Errorf("after recovery code=%d reached=%v, want 200", w.Code, reached)
	}
}

// Without an authenticated user Auth already answered 401; the guard is never
// reached, and if it were it must not ask about an empty user.
func TestRejectSuspended_UnauthenticatedRequestStaysA401(t *testing.T) {
	checker := &fakeChecker{}
	guard, _ := newGuard(checker)
	w, reached := serve(guard, "")
	if w.Code != http.StatusUnauthorized || reached || checker.calls != 0 {
		t.Errorf("code=%d reached=%v checker calls=%d, want 401, not reached, 0 calls", w.Code, reached, checker.calls)
	}
}
