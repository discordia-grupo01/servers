package handler

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/discordia-grupo01/servers/internal/apierr"
	"github.com/discordia-grupo01/servers/internal/handler/category_handler"
	"github.com/discordia-grupo01/servers/internal/handler/channel_handler"
	"github.com/discordia-grupo01/servers/internal/handler/invitation"
	"github.com/discordia-grupo01/servers/internal/handler/member_role_handler"
	"github.com/discordia-grupo01/servers/internal/handler/membership_handler"
	"github.com/discordia-grupo01/servers/internal/handler/ownership_transfer_handler"
	"github.com/discordia-grupo01/servers/internal/handler/role_handler"
	"github.com/discordia-grupo01/servers/internal/middleware"
)

type Pinger interface {
	Ping(ctx context.Context) error
}

type Deps struct {
	Servers            ServerUseCases
	Icons              IconStore
	Roles              role_handler.RoleUseCases
	MemberRoles        member_role_handler.MemberRoleUseCases
	Membership         membership_handler.UseCases
	OwnershipTransfers ownership_transfer_handler.UseCases
	Invitations        invitation.UseCases
	Channels           channel_handler.ChannelUseCases
	Categories         category_handler.CategoryUseCases
	Auth               middleware.AuthConfig
	DB                 Pinger
}

func NewRouter(d Deps) *gin.Engine {
	h := &serverHandler{uc: d.Servers, icons: d.Icons}
	roleH := role_handler.New(d.Roles)
	memberRoleH := member_role_handler.New(d.MemberRoles)
	membershipH := membership_handler.New(d.Membership)
	transferH := ownership_transfer_handler.New(d.OwnershipTransfers)
	channelH := channel_handler.New(d.Channels)
	categoryH := category_handler.New(d.Categories)

	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())
	r.MaxMultipartMemory = maxIconUploadBytes
	r.HandleMethodNotAllowed = true
	r.NoRoute(func(c *gin.Context) {
		apierr.RespondError(c, http.StatusNotFound, "route not found", nil)
	})
	r.NoMethod(func(c *gin.Context) {
		apierr.RespondError(c, http.StatusMethodNotAllowed, "method not allowed", nil)
	})

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

	v1Public := r.Group("/v1")
	v1Public.GET("/servers/:serverId/icon", h.getIcon)
	v1Public.GET("/servers/:serverId/banner", h.getBanner)

	v1 := r.Group("/v1")
	v1.Use(middleware.Auth(d.Auth))
	v1.POST("/servers", h.createServer)
	v1.GET("/servers", h.listMyServers)
	v1.GET("/servers/:serverId", h.getServer)
	v1.PATCH("/servers/:serverId", h.updateServer)

	v1.POST("/servers/:serverId/roles", roleH.CreateRole)
	v1.GET("/servers/:serverId/roles", roleH.ListRoles)
	v1.GET("/roles/:roleId", roleH.GetRole)
	v1.PATCH("/roles/:roleId", roleH.UpdateRolePermissions)
	v1.DELETE("/roles/:roleId", roleH.DeleteRole)
	v1.PUT("/servers/:serverId/default-role", roleH.SetDefaultRole)

	v1.POST("/servers/:serverId/members/:userId/roles", memberRoleH.AssignRole)
	v1.DELETE("/servers/:serverId/members/:userId/roles/:roleId", memberRoleH.RemoveRole)
	v1.GET("/servers/:serverId/members/:userId/roles", memberRoleH.ListMemberRoles)
	v1.GET("/servers/:serverId/members/:userId/permissions", memberRoleH.GetMemberPermissions)

	v1.GET("/servers/:serverId/members", membershipH.ListMembers)
	v1.DELETE("/servers/:serverId/members/:userId", membershipH.LeaveServer)

	v1.POST("/servers/:serverId/channels", channelH.CreateChannel)
	v1.PATCH("/channels/:channelId", channelH.UpdateChannel)
	v1.DELETE("/channels/:channelId", channelH.DeleteChannel)
	v1.PATCH("/channels/:channelId/category", channelH.MoveToCategory)
	v1.PATCH("/servers/:serverId/channels/reorder", channelH.Reorder)

	v1.POST("/servers/:serverId/categories", categoryH.CreateCategory)
	v1.PATCH("/servers/:serverId/categories/reorder", categoryH.Reorder)

	v1.POST("/servers/:serverId/ownership-transfers", transferH.Initiate)
	v1.GET("/servers/:serverId/ownership-transfers/pending", transferH.GetPending)
	v1.POST("/servers/:serverId/ownership-transfers/:transferId/accept", transferH.Accept)
	v1.POST("/servers/:serverId/ownership-transfers/:transferId/reject", transferH.Reject)
	v1.POST("/servers/:serverId/ownership-transfers/:transferId/cancel", transferH.Cancel)

	invitation.NewHandler(d.Invitations).Register(v1)

	return r
}
