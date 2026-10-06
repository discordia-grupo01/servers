// Package channelperm_handler exposes #57 (Sobrescribir permisos de un rol en
// un canal) over HTTP: a channel's per-role permission overrides and a
// member's effective permissions in a channel.
package channelperm_handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/discordia-grupo01/servers/internal/apierr"
	"github.com/discordia-grupo01/servers/internal/middleware"
	"github.com/discordia-grupo01/servers/internal/model/channelperm"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/service/channelperm_service"
)

type Handler struct {
	uc UseCases
}

func New(uc UseCases) *Handler {
	return &Handler{uc: uc}
}

type setOverrideRequest struct {
	Allow []string `json:"allow"`
	Deny  []string `json:"deny"`
}

type OverrideResponse struct {
	ChannelID string    `json:"channel_id"`
	RoleID    string    `json:"role_id"`
	Allow     []string  `json:"allow"`
	Deny      []string  `json:"deny"`
	UpdatedAt time.Time `json:"updated_at"`
}

func toResponse(o channelperm.Override) OverrideResponse {
	return OverrideResponse{
		ChannelID: o.ChannelID.String(),
		RoleID:    o.RoleID.String(),
		Allow:     role.PermissionNames(o.Allow),
		Deny:      role.PermissionNames(o.Deny),
		UpdatedAt: o.UpdatedAt,
	}
}

type effectivePermissionsResponse struct {
	ChannelID   string   `json:"channel_id"`
	UserID      string   `json:"user_id"`
	Permissions []string `json:"permissions"`
}

// SetOverride godoc
// @Summary      Set a role's permission override in a channel
// @Description  Creates or replaces what the role gains (allow) and loses (deny) in this channel only; the role's server-wide permissions don't change. Only VIEW_CHANNELS, SEND_MESSAGES and MANAGE_MESSAGES can be overridden. With both lists empty the override is removed (204). Requires MANAGE_ROLES (administrators and the owner hold it implicitly), and the role must be below the caller's highest role -- @everyone excepted. The caller can only allow permissions they hold themselves. Overrides never restrict the owner or administrators.
// @Tags         channel-permissions
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        channelId  path  string              true  "Channel ID"
// @Param        roleId     path  string              true  "Role ID (it may be @everyone)"
// @Param        body       body  setOverrideRequest  true  "Permissions to allow and to deny in this channel"
// @Success      200  {object}  OverrideResponse
// @Success      204  "both lists empty: the override was removed"
// @Failure      400  {object}  apierr.ErrorResponse  "invalid id or body, unknown permission, server-level permission (permission_not_overridable), the same permission allowed and denied (permission_conflict), or role from another server"
// @Failure      401  {object}  apierr.ErrorResponse
// @Failure      403  {object}  apierr.ErrorResponse  "missing MANAGE_ROLES, role not below the caller's (role_above_own), or allowing a permission the caller doesn't hold (permission_not_held)"
// @Failure      404  {object}  apierr.ErrorResponse  "channel or role not found"
// @Router       /v1/channels/{channelId}/permissions/roles/{roleId} [put]
func (h *Handler) SetOverride(c *gin.Context) {
	channelID, ok := apierr.ParseUUIDParam(c, "channelId")
	if !ok {
		return
	}
	roleID, ok := apierr.ParseUUIDParam(c, "roleId")
	if !ok {
		return
	}
	var req setOverrideRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.RespondError(c, http.StatusBadRequest, "invalid request body", map[string]any{"reason": "invalid_json"})
		return
	}

	o, err := h.uc.SetOverride(c.Request.Context(), channelperm_service.SetOverrideParams{
		ChannelID:   channelID,
		RoleID:      roleID,
		ActorUserID: middleware.UserID(c),
		Allow:       req.Allow,
		Deny:        req.Deny,
	})
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}
	if o.IsEmpty() {
		c.Status(http.StatusNoContent)
		return
	}
	c.JSON(http.StatusOK, toResponse(o))
}

// DeleteOverride godoc
// @Summary      Remove a role's permission override in a channel
// @Description  The role goes back to its server-wide permissions in this channel. Same authorization as setting it, without the grant check.
// @Tags         channel-permissions
// @Produce      json
// @Security     BearerAuth
// @Param        channelId  path  string  true  "Channel ID"
// @Param        roleId     path  string  true  "Role ID"
// @Success      204
// @Failure      400  {object}  apierr.ErrorResponse  "invalid id, or role from another server"
// @Failure      401  {object}  apierr.ErrorResponse
// @Failure      403  {object}  apierr.ErrorResponse  "missing MANAGE_ROLES, or role not below the caller's (role_above_own)"
// @Failure      404  {object}  apierr.ErrorResponse  "channel or role not found, or the role has no override in this channel (channel_override_not_found)"
// @Router       /v1/channels/{channelId}/permissions/roles/{roleId} [delete]
func (h *Handler) DeleteOverride(c *gin.Context) {
	channelID, ok := apierr.ParseUUIDParam(c, "channelId")
	if !ok {
		return
	}
	roleID, ok := apierr.ParseUUIDParam(c, "roleId")
	if !ok {
		return
	}
	err := h.uc.DeleteOverride(c.Request.Context(), channelperm_service.DeleteOverrideParams{
		ChannelID:   channelID,
		RoleID:      roleID,
		ActorUserID: middleware.UserID(c),
	})
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ListOverrides godoc
// @Summary      List a channel's permission overrides
// @Description  One entry per role with an override in this channel, @everyone first and then in the order of the role hierarchy. Roles without an override use their server-wide permissions and are not listed. Requires MANAGE_ROLES.
// @Tags         channel-permissions
// @Produce      json
// @Security     BearerAuth
// @Param        channelId  path  string  true  "Channel ID"
// @Success      200  {array}   OverrideResponse
// @Failure      400  {object}  apierr.ErrorResponse  "invalid channel id"
// @Failure      401  {object}  apierr.ErrorResponse
// @Failure      403  {object}  apierr.ErrorResponse  "missing MANAGE_ROLES"
// @Failure      404  {object}  apierr.ErrorResponse  "channel not found"
// @Router       /v1/channels/{channelId}/permissions [get]
func (h *Handler) ListOverrides(c *gin.Context) {
	channelID, ok := apierr.ParseUUIDParam(c, "channelId")
	if !ok {
		return
	}
	overrides, err := h.uc.ListOverrides(c.Request.Context(), channelperm_service.ListOverridesParams{
		ChannelID:   channelID,
		ActorUserID: middleware.UserID(c),
	})
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}
	resp := make([]OverrideResponse, 0, len(overrides))
	for _, o := range overrides {
		resp = append(resp, toResponse(o))
	}
	c.JSON(http.StatusOK, resp)
}

// EffectivePermissions godoc
// @Summary      Get a member's effective permissions in a channel
// @Description  What the member may actually do in this channel: their server-wide permissions with the channel's overrides for their roles applied (@everyone first, then their other roles, where allow wins over deny). Every permission for the owner and for members with ADMINISTRATOR, whatever the overrides. Same response shape and access rule as GET /v1/servers/{serverId}/members/{userId}/permissions, which ignores overrides: services that authorize per channel (messaging, voice) should use this one.
// @Tags         channel-permissions
// @Produce      json
// @Security     BearerAuth
// @Param        channelId  path  string  true  "Channel ID"
// @Param        userId     path  string  true  "Target member's user ID"
// @Success      200  {object}  effectivePermissionsResponse
// @Failure      400  {object}  apierr.ErrorResponse  "invalid channel id"
// @Failure      401  {object}  apierr.ErrorResponse
// @Failure      403  {object}  apierr.ErrorResponse  "the caller is not a member of the channel's server"
// @Failure      404  {object}  apierr.ErrorResponse  "channel not found, or the user is not a member"
// @Router       /v1/channels/{channelId}/members/{userId}/permissions [get]
func (h *Handler) EffectivePermissions(c *gin.Context) {
	channelID, ok := apierr.ParseUUIDParam(c, "channelId")
	if !ok {
		return
	}
	targetUserID := c.Param("userId")
	permissions, err := h.uc.EffectivePermissions(c.Request.Context(), channelperm_service.EffectivePermissionsParams{
		ChannelID:    channelID,
		TargetUserID: targetUserID,
		ActorUserID:  middleware.UserID(c),
	})
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}
	c.JSON(http.StatusOK, effectivePermissionsResponse{
		ChannelID:   channelID.String(),
		UserID:      targetUserID,
		Permissions: permissions,
	})
}
