package membership_handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/discordia-grupo01/servers/internal/apierr"
	"github.com/discordia-grupo01/servers/internal/middleware"
	"github.com/discordia-grupo01/servers/internal/model"
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

// GET /v1/servers/:serverId/members?limit=&offset=
func (h *Handler) ListMembers(c *gin.Context) {
	serverID, ok := apierr.ParseUUIDParam(c, "serverId")
	if !ok {
		return
	}

	limit, ok := parseNonNegativeIntQuery(c, "limit", model.MemberListDefaultLimit)
	if !ok {
		return
	}
	offset, ok := parseNonNegativeIntQuery(c, "offset", 0)
	if !ok {
		return
	}

	members, total, err := h.uc.ListMembers(c.Request.Context(), membership_service.ListMembersParams{
		ServerID:    serverID,
		ActorUserID: middleware.UserID(c),
		Limit:       limit,
		Offset:      offset,
	})
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}

	resp := model.MemberListResponse{
		Members: make([]model.MemberResponse, len(members)),
		Total:   total,
		Limit:   limit,
		Offset:  offset,
	}
	for i, m := range members {
		resp.Members[i] = model.MemberResponse{
			UserID: m.UserID, IsOwner: m.IsOwner, JoinedAt: m.JoinedAt,
		}
	}
	c.JSON(http.StatusOK, resp)
}

// parseNonNegativeIntQuery reads an optional non-negative integer query
// param, falling back to def when absent. It writes a 400 response and
// returns ok=false when the value is present but not a non-negative integer.
func parseNonNegativeIntQuery(c *gin.Context, name string, def int) (int, bool) {
	raw := c.Query(name)
	if raw == "" {
		return def, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		apierr.RespondError(c, http.StatusBadRequest, "invalid "+name, apierr.FieldErr(name, "must_be_a_non_negative_integer"))
		return 0, false
	}
	return n, true
}
