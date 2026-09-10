// Package handler expone la API HTTP del servicio.
package handler

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/discordia-grupo01/servers/internal/handler/member_role_handler"
	"github.com/discordia-grupo01/servers/internal/handler/role_handler"
	"github.com/discordia-grupo01/servers/internal/middleware"
)

type Pinger interface {
	Ping(ctx context.Context) error
}

type Deps struct {
	Servers     ServerUseCases
	Roles       role_handler.RoleUseCases
	MemberRoles member_role_handler.MemberRoleUseCases
	Auth        middleware.AuthConfig
	DB          Pinger
}

func NewRouter(d Deps) *gin.Engine {
	h := &serverHandler{uc: d.Servers}
	roleH := role_handler.New(d.Roles)
	memberRoleH := member_role_handler.New(d.MemberRoles)

	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())
	r.MaxMultipartMemory = maxIconUploadBytes

	r.GET("/livez", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	r.GET("/readyz", func(c *gin.Context) {
		if d.DB != nil {
			ctx, cancel := context.WithTimeout(c.Request.Context(), dbPingTimeout)
			defer cancel()
			if err := d.DB.Ping(ctx); err != nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unavailable", "detail": "database"})
				return
			}
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	})

	r.GET("/", func(c *gin.Context) { c.String(http.StatusOK, "discordia-servers up") })

	v1 := r.Group("/v1")
	v1.Use(middleware.Auth(d.Auth))
	v1.POST("/servers", h.createServer)

	v1.POST("/servers/:serverId/roles", roleH.CreateRole)
	v1.GET("/servers/:serverId/roles", roleH.ListRoles)
	v1.PATCH("/roles/:roleId", roleH.UpdateRolePermissions)

	v1.POST("/servers/:serverId/members/:userId/roles", memberRoleH.AssignRole)
	v1.DELETE("/servers/:serverId/members/:userId/roles/:roleId", memberRoleH.RemoveRole)
	v1.GET("/servers/:serverId/members/:userId/roles", memberRoleH.ListMemberRoles)

	return r
}
