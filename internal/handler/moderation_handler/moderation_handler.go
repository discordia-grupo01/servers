// Package moderation_handler exposes the Moderación epic over HTTP: banning
// members (#25), revoking bans (#26), listing bans and kicking members (#59).
package moderation_handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/discordia-grupo01/servers/internal/apierr"
	"github.com/discordia-grupo01/servers/internal/middleware"
	"github.com/discordia-grupo01/servers/internal/model/moderation"
	"github.com/discordia-grupo01/servers/internal/service/moderation_service"
)

type Handler struct {
	uc UseCases
}

func New(uc UseCases) *Handler {
	return &Handler{uc: uc}
}

type banRequest struct {
	UserID string  `json:"user_id"`
	Reason *string `json:"reason"`
}

// Ban godoc
// @Summary      Ban a member from the server
// @Description  Requires BAN_MEMBERS (the owner and administrators hold it implicitly). The target must be a current member ranked strictly below the caller in the role hierarchy; the owner and the caller themselves can't be banned. The member is removed (with their roles), a pending ownership transfer to them is cancelled, and they can't rejoin through any invitation until the ban is revoked. `reason` is optional (max 512 characters).
// @Tags         moderation
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        serverId  path  string      true  "Server ID"
// @Param        body      body  banRequest  true  "Who to ban and, optionally, why"
// @Success      201  {object}  moderation.BanResponse
// @Failure      400  {object}  apierr.ErrorResponse  "missing user_id, banning yourself, or invalid reason"
// @Failure      401  {object}  apierr.ErrorResponse
// @Failure      403  {object}  apierr.ErrorResponse  "missing BAN_MEMBERS, target is the owner, or target has an equal or higher role"
// @Failure      404  {object}  apierr.ErrorResponse  "server not found, or target is not a member"
// @Failure      409  {object}  apierr.ErrorResponse  "target is already banned"
// @Router       /v1/servers/{serverId}/bans [post]
func (h *Handler) Ban(c *gin.Context) {
	serverID, ok := apierr.ParseUUIDParam(c, "serverId")
	if !ok {
		return
	}

	var req banRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.RespondError(c, http.StatusBadRequest, "invalid request body", map[string]any{"reason": "invalid_json"})
		return
	}

	ban, err := h.uc.BanMember(c.Request.Context(), moderation_service.BanParams{
		ServerID:     serverID,
		ActorUserID:  middleware.UserID(c),
		TargetUserID: req.UserID,
		Reason:       req.Reason,
	})
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}
	c.JSON(http.StatusCreated, moderation.ToResponse(ban))
}

type kickRequest struct {
	UserID string `json:"user_id"`
}

type muteRequest struct {
	UserID  string    `json:"user_id"`
	UntilAt time.Time `json:"until_at"`
	Reason  *string   `json:"reason"`
}

// Mute godoc
// @Summary      Temporarily mute a member
// @Description  Requires MANAGE_MESSAGES. The member remains in the server and connected sessions stay open, but message sends and voice publishing are denied until until_at.
// @Tags         moderation
// @Accept       json
// @Security     BearerAuth
// @Param        serverId path string true "Server ID"
// @Param        body body muteRequest true "Mute details"
// @Success      204
// @Router       /v1/servers/{serverId}/mutes [post]
func (h *Handler) Mute(c *gin.Context) {
	serverID, ok := apierr.ParseUUIDParam(c, "serverId")
	if !ok {
		return
	}
	var req muteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.RespondError(c, http.StatusBadRequest, "invalid request body", map[string]any{"reason": "invalid_json"})
		return
	}
	if err := h.uc.MuteMember(c.Request.Context(), moderation_service.MuteParams{
		ServerID: serverID, ActorUserID: middleware.UserID(c), TargetUserID: req.UserID,
		UntilAt: req.UntilAt, Reason: req.Reason,
	}); err != nil {
		apierr.WriteDomainError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// Unmute godoc
// @Summary      Remove a member mute
// @Tags         moderation
// @Security     BearerAuth
// @Param        serverId path string true "Server ID"
// @Param        userId path string true "Muted user ID"
// @Success      204
// @Failure      404  {object} apierr.ErrorResponse  "user is not muted in this server"
// @Router       /v1/servers/{serverId}/mutes/{userId} [delete]
func (h *Handler) Unmute(c *gin.Context) {
	serverID, ok := apierr.ParseUUIDParam(c, "serverId")
	if !ok {
		return
	}
	if err := h.uc.UnmuteMember(c.Request.Context(), moderation_service.UnmuteParams{
		ServerID: serverID, ActorUserID: middleware.UserID(c), TargetUserID: c.Param("userId"),
	}); err != nil {
		apierr.WriteDomainError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// Kick godoc
// @Summary      Kick a member from the server
// @Description  Requires KICK_MEMBERS (the owner and administrators hold it implicitly). The target must be a current member ranked strictly below the caller in the role hierarchy; the owner and the caller themselves can't be kicked (to leave, use DELETE /v1/servers/{serverId}/members/{userId}). The member is removed (with their roles) and a pending ownership transfer to them is cancelled. Unlike a ban, they can rejoin with any valid invitation.
// @Tags         moderation
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        serverId  path  string       true  "Server ID"
// @Param        body      body  kickRequest  true  "Who to kick"
// @Success      204
// @Failure      400  {object}  apierr.ErrorResponse  "missing user_id, or kicking yourself"
// @Failure      401  {object}  apierr.ErrorResponse
// @Failure      403  {object}  apierr.ErrorResponse  "missing KICK_MEMBERS, target is the owner, or target has an equal or higher role"
// @Failure      404  {object}  apierr.ErrorResponse  "server not found, or target is not a member"
// @Router       /v1/servers/{serverId}/kicks [post]
func (h *Handler) Kick(c *gin.Context) {
	serverID, ok := apierr.ParseUUIDParam(c, "serverId")
	if !ok {
		return
	}

	var req kickRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.RespondError(c, http.StatusBadRequest, "invalid request body", map[string]any{"reason": "invalid_json"})
		return
	}

	err := h.uc.KickMember(c.Request.Context(), moderation_service.KickParams{
		ServerID:     serverID,
		ActorUserID:  middleware.UserID(c),
		TargetUserID: req.UserID,
	})
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// Unban godoc
// @Summary      Revoke a ban
// @Description  Requires BAN_MEMBERS (the owner and administrators hold it implicitly). There is no hierarchy check. The user does not become a member again: they can rejoin with any valid invitation, and get the default role like any new member.
// @Tags         moderation
// @Produce      json
// @Security     BearerAuth
// @Param        serverId  path  string  true  "Server ID"
// @Param        userId    path  string  true  "ID of the banned user"
// @Success      204
// @Failure      400  {object}  apierr.ErrorResponse  "invalid server id"
// @Failure      401  {object}  apierr.ErrorResponse
// @Failure      403  {object}  apierr.ErrorResponse  "missing BAN_MEMBERS"
// @Failure      404  {object}  apierr.ErrorResponse  "server not found, or the user is not banned"
// @Router       /v1/servers/{serverId}/bans/{userId} [delete]
func (h *Handler) Unban(c *gin.Context) {
	serverID, ok := apierr.ParseUUIDParam(c, "serverId")
	if !ok {
		return
	}

	err := h.uc.UnbanMember(c.Request.Context(), moderation_service.UnbanParams{
		ServerID:     serverID,
		ActorUserID:  middleware.UserID(c),
		TargetUserID: c.Param("userId"),
	})
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ListBans godoc
// @Summary      List the server's bans
// @Description  Requires BAN_MEMBERS (the owner and administrators hold it implicitly). Newest first, paginated. Each ban carries the banned user's public profile (name, avatar, description, status), replicated from identify; profile is null until it has been replicated, and may lag a few seconds behind a profile edit. A banned user is no longer a member, so this is the only place to get it.
// @Tags         moderation
// @Produce      json
// @Security     BearerAuth
// @Param        serverId  path   string  true   "Server ID"
// @Param        limit     query  int     false  "Page size (max 100)" default(20)
// @Param        offset    query  int     false  "Items to skip" default(0)
// @Success      200  {object}  moderation.BanListResponse
// @Failure      400  {object}  apierr.ErrorResponse  "invalid limit/offset"
// @Failure      401  {object}  apierr.ErrorResponse
// @Failure      403  {object}  apierr.ErrorResponse  "missing BAN_MEMBERS"
// @Failure      404  {object}  apierr.ErrorResponse
// @Router       /v1/servers/{serverId}/bans [get]
func (h *Handler) ListBans(c *gin.Context) {
	serverID, ok := apierr.ParseUUIDParam(c, "serverId")
	if !ok {
		return
	}
	limit, ok := apierr.ParseNonNegativeIntQuery(c, "limit", moderation.ListDefaultLimit)
	if !ok {
		return
	}
	offset, ok := apierr.ParseNonNegativeIntQuery(c, "offset", 0)
	if !ok {
		return
	}

	page, err := h.uc.ListBans(c.Request.Context(), moderation_service.ListBansParams{
		ServerID:    serverID,
		ActorUserID: middleware.UserID(c),
		Limit:       limit,
		Offset:      offset,
	})
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}

	resp := moderation.BanListResponse{
		Bans:   make([]moderation.BanResponse, 0, len(page.Bans)),
		Total:  page.Total,
		Limit:  page.Limit,
		Offset: page.Offset,
	}
	for _, b := range page.Bans {
		resp.Bans = append(resp.Bans, moderation.ToResponse(b))
	}
	c.JSON(http.StatusOK, resp)
}
