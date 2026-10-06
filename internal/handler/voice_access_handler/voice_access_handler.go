package voice_access_handler

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/apierr"
	"github.com/discordia-grupo01/servers/internal/middleware"
	"github.com/discordia-grupo01/servers/internal/service/channel_service"
)

type UseCases interface {
	AuthorizeVoice(ctx context.Context, channelID uuid.UUID, userID string) (channel_service.VoiceAccess, error)
}

type Handler struct {
	uc UseCases
}

func New(uc UseCases) *Handler {
	return &Handler{uc: uc}
}

type response struct {
	ChannelID string `json:"channel_id"`
	ServerID  string `json:"server_id"`
	Allowed   bool   `json:"allowed"`
	Name      string `json:"name"`
}

// AuthorizeVoice godoc
// @Summary      Check voice channel access
// @Description  Confirms that the authenticated user can connect to a voice channel.
// @Tags         voice
// @Produce      json
// @Security     BearerAuth
// @Param        channelId  path  string  true  "Voice channel ID"
// @Success      200  {object} response
// @Failure      401  {object} apierr.ErrorResponse
// @Failure      403  {object} apierr.ErrorResponse
// @Failure      404  {object} apierr.ErrorResponse
// @Router       /v1/channels/{channelId}/voice-access [get]
func (h *Handler) AuthorizeVoice(c *gin.Context) {
	channelID, err := uuid.Parse(c.Param("channelId"))
	if err != nil {
		apierr.RespondError(c, http.StatusBadRequest, "invalid channelId", apierr.FieldErr("channelId", "invalid_uuid"))
		return
	}

	access, err := h.uc.AuthorizeVoice(c.Request.Context(), channelID, middleware.UserID(c))
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}

	c.JSON(http.StatusOK, response{
		ChannelID: access.ChannelID.String(),
		ServerID:  access.ServerID.String(),
		Allowed:   true,
		Name:      access.Name,
	})
}
