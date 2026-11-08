package middleware

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/apierr"
	"github.com/discordia-grupo01/servers/internal/logging"
)

// ErrorCodeServerSuspended is the details.error_code of the 403 anyone gets for
// acting on a server the platform staff suspended. Same shape as
// ErrorCodeAccountSuspended: the reason travels in details so the numeric code
// stays equal to the HTTP status.
const ErrorCodeServerSuspended = "SERVER_SUSPENDED"

// leaveServerRoute is the only route a suspended server still answers: a member
// removing their own membership.
const leaveServerRoute = "/v1/servers/:serverId/members/:userId"

// ServerSuspensionChecker tells whether a server is suspended, given the server
// itself or the channel, category, role or invitation code that belongs to it.
// found is false when the resource doesn't exist.
type ServerSuspensionChecker interface {
	SuspendedByServer(ctx context.Context, serverID uuid.UUID) (suspended, found bool, err error)
	SuspendedByChannel(ctx context.Context, channelID uuid.UUID) (suspended, found bool, err error)
	SuspendedByCategory(ctx context.Context, categoryID uuid.UUID) (suspended, found bool, err error)
	SuspendedByRole(ctx context.Context, roleID uuid.UUID) (suspended, found bool, err error)
	SuspendedByInvite(ctx context.Context, code string) (suspended, found bool, err error)
}

// ServerSuspensionGuard rejects every request that acts on a suspended server,
// with a short in-memory cache so the check does not hit the database on every
// request.
type ServerSuspensionGuard struct {
	checker ServerSuspensionChecker
	ttl     time.Duration
	now     func() time.Time

	mu    sync.Mutex
	cache map[string]cachedSuspension
}

func NewServerSuspensionGuard(checker ServerSuspensionChecker, ttl time.Duration) *ServerSuspensionGuard {
	return &ServerSuspensionGuard{checker: checker, ttl: ttl, now: time.Now, cache: make(map[string]cachedSuspension)}
}

// WithClock replaces the clock used to expire cache entries. For tests.
func (g *ServerSuspensionGuard) WithClock(now func() time.Time) *ServerSuspensionGuard {
	g.now = now
	return g
}

// Reset forgets everything that is cached. The consumer calls it right after
// applying an admin event, which makes the change effective at once on the
// instance that applied it. It drops the whole cache rather than one server
// because the keys are channels, roles, categories and invitation codes, and
// which of them belong to a server isn't known here; admin events are rare and
// the entries only a few seconds old, so nothing is lost.
func (g *ServerSuspensionGuard) Reset() {
	g.mu.Lock()
	g.cache = make(map[string]cachedSuspension)
	g.mu.Unlock()
}

// RejectSuspendedServer must run after Auth and after the account guard. It
// looks at the route to find which server the request acts on, from the first
// of serverId, channelId, categoryId, roleId or code that the route has, and
// answers 403 with details.error_code = SERVER_SUSPENDED if that server is
// suspended -- for the owner too.
//
// It lets through the routes that name no server (creating a server, listing
// mine), a malformed id or a resource that doesn't exist (the handler answers
// 400 or 404 itself), and a member leaving the server. If the state can't be
// read the request is refused with 503 instead of letting it through; that
// answer is not cached.
func (g *ServerSuspensionGuard) RejectSuspendedServer() gin.HandlerFunc {
	return func(c *gin.Context) {
		scope, ok := serverScope(c)
		if !ok || isLeavingTheServer(c) {
			c.Next()
			return
		}

		suspended, found, err := g.isSuspended(c.Request.Context(), scope)
		if err != nil {
			logging.Errorf("server suspension check failed, %s=%s: %v", scope.kind, scope.value, err)
			c.Abort()
			apierr.RespondError(c, http.StatusServiceUnavailable, "could not verify server status", nil)
			return
		}
		if found && suspended {
			c.Abort()
			apierr.RespondError(c, http.StatusForbidden, "server suspended", map[string]any{"error_code": ErrorCodeServerSuspended})
			return
		}
		c.Next()
	}
}

// scope is the resource of the route that identifies a server.
type scope struct {
	kind  string // the route parameter: serverId, channelId, categoryId, roleId or code
	value string
	id    uuid.UUID // parsed value, except for code
}

var uuidScopes = []string{"serverId", "channelId", "categoryId", "roleId"}

func serverScope(c *gin.Context) (scope, bool) {
	for _, kind := range uuidScopes {
		raw := c.Param(kind)
		if raw == "" {
			continue
		}
		id, err := uuid.Parse(raw)
		if err != nil {
			return scope{}, false
		}
		return scope{kind: kind, value: raw, id: id}, true
	}
	if code := c.Param("code"); code != "" {
		return scope{kind: "code", value: code}, true
	}
	return scope{}, false
}

func isLeavingTheServer(c *gin.Context) bool {
	if c.Request.Method != http.MethodDelete || c.FullPath() != leaveServerRoute {
		return false
	}
	userID := UserID(c)
	return userID != "" && c.Param("userId") == userID
}

func (g *ServerSuspensionGuard) isSuspended(ctx context.Context, s scope) (suspended, found bool, err error) {
	key := s.kind + ":" + s.value
	now := g.now()
	g.mu.Lock()
	entry, ok := g.cache[key]
	g.mu.Unlock()
	if ok && now.Before(entry.expires) {
		return entry.suspended, true, nil
	}

	suspended, found, err = g.lookup(ctx, s)
	if err != nil || !found {
		return false, found, err
	}

	g.mu.Lock()
	if len(g.cache) >= maxCachedUsers {
		g.cache = make(map[string]cachedSuspension)
	}
	g.cache[key] = cachedSuspension{suspended: suspended, expires: now.Add(g.ttl)}
	g.mu.Unlock()
	return suspended, true, nil
}

func (g *ServerSuspensionGuard) lookup(ctx context.Context, s scope) (suspended, found bool, err error) {
	switch s.kind {
	case "serverId":
		return g.checker.SuspendedByServer(ctx, s.id)
	case "channelId":
		return g.checker.SuspendedByChannel(ctx, s.id)
	case "categoryId":
		return g.checker.SuspendedByCategory(ctx, s.id)
	case "roleId":
		return g.checker.SuspendedByRole(ctx, s.id)
	default:
		return g.checker.SuspendedByInvite(ctx, s.value)
	}
}
