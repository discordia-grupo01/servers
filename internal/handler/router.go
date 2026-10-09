package handler

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/discordia-grupo01/servers/internal/apierr"
	"github.com/discordia-grupo01/servers/internal/handler/category_handler"
	"github.com/discordia-grupo01/servers/internal/handler/channel_handler"
	"github.com/discordia-grupo01/servers/internal/handler/channelperm_handler"
	"github.com/discordia-grupo01/servers/internal/handler/invitation"
	"github.com/discordia-grupo01/servers/internal/handler/member_role_handler"
	"github.com/discordia-grupo01/servers/internal/handler/membership_handler"
	"github.com/discordia-grupo01/servers/internal/handler/mention_handler"
	"github.com/discordia-grupo01/servers/internal/handler/moderation_handler"
	"github.com/discordia-grupo01/servers/internal/handler/ownership_transfer_handler"
	"github.com/discordia-grupo01/servers/internal/handler/role_handler"
	"github.com/discordia-grupo01/servers/internal/handler/voice_access_handler"
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
	Moderation         moderation_handler.UseCases
	ChannelPermissions channelperm_handler.UseCases
	VoiceAccess        voice_access_handler.UseCases
	Mentions           mention_handler.UseCases
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
	moderationH := moderation_handler.New(d.Moderation)
	channelPermH := channelperm_handler.New(d.ChannelPermissions)
	voiceAccessH := voice_access_handler.New(d.VoiceAccess)
	mentionH := mention_handler.New(d.Mentions)

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

	r.GET("/livez", livez)
	r.GET("/readyz", readyz(d.DB))

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
	v1.DELETE("/servers/:serverId", h.deleteServer)

	v1.POST("/servers/:serverId/roles", roleH.CreateRole)
	v1.GET("/servers/:serverId/roles", roleH.ListRoles)
	v1.GET("/roles/:roleId", roleH.GetRole)
	v1.PATCH("/roles/:roleId", roleH.UpdateRolePermissions)
	v1.DELETE("/roles/:roleId", roleH.DeleteRole)
	v1.PUT("/servers/:serverId/default-role", roleH.SetDefaultRole)
	v1.PATCH("/servers/:serverId/roles/reorder", roleH.ReorderRoles)

	v1.POST("/servers/:serverId/members/:userId/roles", memberRoleH.AssignRole)
	v1.DELETE("/servers/:serverId/members/:userId/roles/:roleId", memberRoleH.RemoveRole)
	v1.GET("/servers/:serverId/members/:userId/roles", memberRoleH.ListMemberRoles)
	v1.GET("/servers/:serverId/members/:userId/permissions", memberRoleH.GetMemberPermissions)

	v1.GET("/servers/:serverId/members", membershipH.ListMembers)
	v1.DELETE("/servers/:serverId/members/:userId", membershipH.LeaveServer)

	v1.POST("/servers/:serverId/channels", channelH.CreateChannel)
	v1.PATCH("/channels/:channelId", channelH.UpdateChannel)
	v1.DELETE("/channels/:channelId", channelH.DeleteChannel)
	if d.VoiceAccess != nil {
		v1.GET("/channels/:channelId/voice-access", voiceAccessH.AuthorizeVoice)
	}
	v1.PATCH("/channels/:channelId/category", channelH.MoveToCategory)
	v1.PATCH("/servers/:serverId/channels/reorder", channelH.Reorder)

	v1.GET("/channels/:channelId/permissions", channelPermH.ListOverrides)
	v1.PUT("/channels/:channelId/permissions/roles/:roleId", channelPermH.SetOverride)
	v1.DELETE("/channels/:channelId/permissions/roles/:roleId", channelPermH.DeleteOverride)
	v1.GET("/channels/:channelId/members/:userId/permissions", channelPermH.EffectivePermissions)

	v1.POST("/servers/:serverId/categories", categoryH.CreateCategory)
	v1.PATCH("/categories/:categoryId", categoryH.UpdateCategory)
	v1.PATCH("/servers/:serverId/categories/reorder", categoryH.Reorder)

	v1.POST("/servers/:serverId/ownership-transfers", transferH.Initiate)
	v1.GET("/servers/:serverId/ownership-transfers/pending", transferH.GetPending)
	v1.POST("/servers/:serverId/ownership-transfers/:transferId/accept", transferH.Accept)
	v1.POST("/servers/:serverId/ownership-transfers/:transferId/reject", transferH.Reject)
	v1.POST("/servers/:serverId/ownership-transfers/:transferId/cancel", transferH.Cancel)

	v1.POST("/servers/:serverId/mentions/resolve", mentionH.Resolve)

	v1.POST("/servers/:serverId/bans", moderationH.Ban)
	v1.GET("/servers/:serverId/bans", moderationH.ListBans)
	v1.DELETE("/servers/:serverId/bans/:userId", moderationH.Unban)
	v1.POST("/servers/:serverId/kicks", moderationH.Kick)
	v1.POST("/servers/:serverId/mutes", moderationH.Mute)
	v1.DELETE("/servers/:serverId/mutes/:userId", moderationH.Unmute)

	invitation.NewHandler(d.Invitations).Register(v1)

	return r
}

// livez godoc
// @Summary  Liveness check
// @Tags     ops
// @Success  200  "ok"
// @Router   /livez [get]
func livez(c *gin.Context) { c.String(http.StatusOK, "ok") }

// readyz godoc
// @Summary      Readiness check
// @Description  Also pings the database when one is configured.
// @Tags         ops
// @Produce      json
// @Success      200  "ready"
// @Failure      503  "database unavailable"
// @Router       /readyz [get]
func readyz(db Pinger) gin.HandlerFunc {
	return func(c *gin.Context) {
		if db != nil {
			ctx, cancel := context.WithTimeout(c.Request.Context(), dbPingTimeout)
			defer cancel()
			if err := db.Ping(ctx); err != nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unavailable", "detail": "database"})
				return
			}
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	}
}
