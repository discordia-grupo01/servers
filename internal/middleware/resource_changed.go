package middleware

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/logging"
)

const (
	changedFromServerParam = "server"
	changedFromChannel     = "channel"
	changedFromCategory    = "category"
)

type changedRoute struct {
	resource string
	from     string
}

var changedRoutes = map[string]changedRoute{
	"PATCH /v1/channels/:channelId":                  {"channels", changedFromChannel},
	"PATCH /v1/channels/:channelId/category":         {"channels", changedFromChannel},
	"PATCH /v1/servers/:serverId/channels/reorder":   {"channels", changedFromServerParam},
	"POST /v1/servers/:serverId/categories":          {"channels", changedFromServerParam},
	"PATCH /v1/categories/:categoryId":               {"channels", changedFromCategory},
	"PATCH /v1/servers/:serverId/categories/reorder": {"channels", changedFromServerParam},
	"PATCH /v1/servers/:serverId/roles/reorder":      {"roles", changedFromServerParam},
	"PUT /v1/servers/:serverId/default-role":         {"roles", changedFromServerParam},
	"PATCH /v1/servers/:serverId/vanity":             {"server", changedFromServerParam},
}

type ChangeRecorder interface {
	RecordResourceChanged(ctx context.Context, serverID uuid.UUID, resource string) error
}

type ServerOfResolver interface {
	ServerOfChannel(ctx context.Context, channelID uuid.UUID) (uuid.UUID, error)
	ServerOfCategory(ctx context.Context, categoryID uuid.UUID) (uuid.UUID, error)
}

func ResourceChanged(recorder ChangeRecorder, resolver ServerOfResolver) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		status := c.Writer.Status()
		if status < 200 || status >= 300 {
			return
		}
		route, ok := changedRoutes[c.Request.Method+" "+c.FullPath()]
		if !ok {
			return
		}

		serverID, err := serverOf(c, route, resolver)
		if err != nil {
			logging.Warnf("resource_changed: could not tell the server of %s %s: %v", c.Request.Method, c.FullPath(), err)
			return
		}
		if err := recorder.RecordResourceChanged(c.Request.Context(), serverID, route.resource); err != nil {
			logging.Warnf("resource_changed: could not record %s of %s: %v", route.resource, serverID, err)
		}
	}
}

func serverOf(c *gin.Context, route changedRoute, resolver ServerOfResolver) (uuid.UUID, error) {
	switch route.from {
	case changedFromChannel:
		id, err := uuid.Parse(c.Param("channelId"))
		if err != nil {
			return uuid.Nil, err
		}
		return resolver.ServerOfChannel(c.Request.Context(), id)
	case changedFromCategory:
		id, err := uuid.Parse(c.Param("categoryId"))
		if err != nil {
			return uuid.Nil, err
		}
		return resolver.ServerOfCategory(c.Request.Context(), id)
	default:
		return uuid.Parse(c.Param("serverId"))
	}
}
