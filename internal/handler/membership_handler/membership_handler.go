package membership_handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/discordia-grupo01/servers/internal/apierr"
	"github.com/discordia-grupo01/servers/internal/middleware"
	"github.com/discordia-grupo01/servers/internal/service/membership_service"
)

type Handler struct {
	uc UseCases
}

func New(uc UseCases) *Handler {
	return &Handler{uc: uc}
}

// DELETE /v1/servers/:serverId/members/:userId
func (h *Handler) LeaveServer(c *gin.Context) {
	serverID, ok := apierr.ParseUUIDParam(c, "serverId")
	if !ok {
		return
	}

	err := h.uc.LeaveServer(c.Request.Context(), membership_service.LeaveParams{
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
