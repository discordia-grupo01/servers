package middleware

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/discordia-grupo01/servers/internal/apierr"
)

// ErrorCodeAccountSuspended is the details.error_code of the 403 a suspended
// user gets. servers' error body has a numeric code equal to the HTTP status,
// so the reason travels in details to keep that format for existing clients.
const ErrorCodeAccountSuspended = "ACCOUNT_SUSPENDED"

const (
	// DefaultSuspensionCacheTTL is how long a user's suspended / not suspended
	// answer is reused before asking the database again. It bounds how long a
	// suspension applied by another instance takes to start being enforced.
	DefaultSuspensionCacheTTL = 5 * time.Second

	// maxCachedUsers bounds the cache: past it, the whole map is dropped
	// (entries are only a few seconds old anyway).
	maxCachedUsers = 10_000
)

// SuspensionChecker tells whether a user is suspended at platform level.
type SuspensionChecker interface {
	IsSuspended(ctx context.Context, userID string) (bool, error)
}

type cachedSuspension struct {
	suspended bool
	expires   time.Time
}

// SuspensionGuard rejects every request from a suspended user, with a short
// in-memory cache so the check does not hit the database on every request.
type SuspensionGuard struct {
	checker SuspensionChecker
	ttl     time.Duration
	now     func() time.Time

	mu    sync.Mutex
	cache map[string]cachedSuspension
}

func NewSuspensionGuard(checker SuspensionChecker, ttl time.Duration) *SuspensionGuard {
	return &SuspensionGuard{checker: checker, ttl: ttl, now: time.Now, cache: make(map[string]cachedSuspension)}
}

// WithClock replaces the clock used to expire cache entries. For tests.
func (g *SuspensionGuard) WithClock(now func() time.Time) *SuspensionGuard {
	g.now = now
	return g
}

// Invalidate forgets what is cached for userID, so the next request asks the
// database. The consumer calls it right after applying an admin event, which
// makes the change effective at once on the instance that applied it.
func (g *SuspensionGuard) Invalidate(userID string) {
	g.mu.Lock()
	delete(g.cache, userID)
	g.mu.Unlock()
}

// RejectSuspended must run after Auth: it needs the authenticated user id. A
// suspended user gets 403 with details.error_code = ACCOUNT_SUSPENDED on any
// route behind it. If the state can't be read the request is refused with 503
// instead of letting a possibly suspended user through; that answer is not
// cached.
func (g *SuspensionGuard) RejectSuspended() gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := UserID(c)
		if userID == "" {
			c.Next()
			return
		}
		suspended, err := g.isSuspended(c.Request.Context(), userID)
		if err != nil {
			log.Printf("suspension check failed, user_id=%s: %v", userID, err)
			c.Abort()
			apierr.RespondError(c, http.StatusServiceUnavailable, "could not verify account status", nil)
			return
		}
		if suspended {
			c.Abort()
			apierr.RespondError(c, http.StatusForbidden, "account suspended", map[string]any{"error_code": ErrorCodeAccountSuspended})
			return
		}
		c.Next()
	}
}

func (g *SuspensionGuard) isSuspended(ctx context.Context, userID string) (bool, error) {
	now := g.now()
	g.mu.Lock()
	entry, ok := g.cache[userID]
	g.mu.Unlock()
	if ok && now.Before(entry.expires) {
		return entry.suspended, nil
	}

	suspended, err := g.checker.IsSuspended(ctx, userID)
	if err != nil {
		return false, err
	}

	g.mu.Lock()
	if len(g.cache) >= maxCachedUsers {
		g.cache = make(map[string]cachedSuspension)
	}
	g.cache[userID] = cachedSuspension{suspended: suspended, expires: now.Add(g.ttl)}
	g.mu.Unlock()
	return suspended, nil
}
