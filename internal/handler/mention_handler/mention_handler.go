// Package mention_handler exposes the server side of Menciones (#46) over
// HTTP, for messaging to call before saving a message with mentions.
package mention_handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/apierr"
	"github.com/discordia-grupo01/servers/internal/middleware"
	"github.com/discordia-grupo01/servers/internal/model/mention"
	"github.com/discordia-grupo01/servers/internal/service/mention_service"
)

type Handler struct {
	uc UseCases
}

func New(uc UseCases) *Handler {
	return &Handler{uc: uc}
}

type resolveRequest struct {
	ChannelID string   `json:"channel_id"`
	UserIDs   []string `json:"user_ids"`
	RoleIDs   []string `json:"role_ids"`
	Everyone  bool     `json:"everyone"`
}

// Resolve godoc
// @Summary      Resolve the mentions of a message
// @Description  Called by messaging with the author's token before saving a message. Takes the users, roles and @everyone mentioned in it and returns which mentions hold and who gets notified. `user_ids` keeps only members of the server. `role_ids` (never @everyone's own id) and `everyone` count only if the caller holds MENTION_EVERYONE (the owner and administrators hold it implicitly); otherwise they come back empty/false, so the message can still go out with them as plain text. `recipient_ids` are the members reached who can see the channel (VIEW_CHANNELS), without the caller. Duplicates count once; at most 50 users and 20 roles.
// @Tags         mentions
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        serverId  path  string          true  "Server ID"
// @Param        body      body  resolveRequest  true  "Channel of the message and the mentions found in it"
// @Success      200  {object}  mention.ResolutionResponse
// @Failure      400  {object}  apierr.ErrorResponse  "invalid body, channel_id or role id, channel of another server, or too many mentions"
// @Failure      401  {object}  apierr.ErrorResponse
// @Failure      403  {object}  apierr.ErrorResponse  "caller is not a member"
// @Failure      404  {object}  apierr.ErrorResponse  "server or channel not found"
// @Router       /v1/servers/{serverId}/mentions/resolve [post]
func (h *Handler) Resolve(c *gin.Context) {
	serverID, ok := apierr.ParseUUIDParam(c, "serverId")
	if !ok {
		return
	}

	var req resolveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.RespondError(c, http.StatusBadRequest, "invalid request body", map[string]any{"reason": "invalid_json"})
		return
	}
	channelID, err := uuid.Parse(req.ChannelID)
	if err != nil {
		apierr.RespondError(c, http.StatusBadRequest, "invalid channel_id", apierr.FieldErr("channel_id", "invalid_uuid"))
		return
	}
	roleIDs, ok := parseRoleIDs(c, req.RoleIDs)
	if !ok {
		return
	}

	res, err := h.uc.Resolve(c.Request.Context(), mention_service.ResolveParams{
		ServerID:    serverID,
		ChannelID:   channelID,
		ActorUserID: middleware.UserID(c),
		UserIDs:     req.UserIDs,
		RoleIDs:     roleIDs,
		Everyone:    req.Everyone,
	})
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}
	c.JSON(http.StatusOK, mention.ToResponse(res))
}

func parseRoleIDs(c *gin.Context, raw []string) ([]uuid.UUID, bool) {
	ids := make([]uuid.UUID, 0, len(raw))
	for _, s := range raw {
		id, err := uuid.Parse(s)
		if err != nil {
			apierr.RespondError(c, http.StatusBadRequest, "invalid role id", apierr.FieldErr("role_ids", "invalid_uuid"))
			return nil, false
		}
		ids = append(ids, id)
	}
	return ids, true
}
