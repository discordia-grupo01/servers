package membership_handler

import (
	"net/http"

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

// LeaveServer godoc
// @Summary      Leave a server
// @Description  Self-leave only: userId must be the caller's own. The owner cannot leave (transfer ownership or delete the server first).
// @Tags         membership
// @Security     BearerAuth
// @Param        serverId  path  string  true  "Server ID"
// @Param        userId    path  string  true  "Must match the caller's own user ID"
// @Success      204  "no content"
// @Failure      401  {object}  apierr.ErrorResponse
// @Failure      403  {object}  apierr.ErrorResponse  "userId is not the caller"
// @Failure      404  {object}  apierr.ErrorResponse
// @Failure      409  {object}  apierr.ErrorResponse  "the owner cannot leave; transfer ownership or delete the server first"
// @Router       /v1/servers/{serverId}/members/{userId} [delete]
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

// ListMembers godoc
// @Summary      List a server's members
// @Description  Caller must already be a member. Paginated. Each member carries their public profile (name, avatar, description, status), replicated from identify; profile is null until it has been replicated, and may lag a few seconds behind a profile edit. A member suspended at platform level stays in the list but with a null profile (no name or avatar of their own) until reactivated. muted_until is when the member's active mute ends, or null if they aren't muted (an expired mute counts as not muted).
// @Tags         membership
// @Produce      json
// @Security     BearerAuth
// @Param        serverId  path   string  true   "Server ID"
// @Param        limit     query  int     false  "Page size" default(50)
// @Param        offset    query  int     false  "Items to skip" default(0)
// @Success      200  {object}  model.MemberListResponse
// @Failure      400  {object}  apierr.ErrorResponse  "invalid limit/offset"
// @Failure      401  {object}  apierr.ErrorResponse
// @Failure      403  {object}  apierr.ErrorResponse  "caller is not a member, or the caller's account is suspended (details.error_code ACCOUNT_SUSPENDED)"
// @Failure      404  {object}  apierr.ErrorResponse
// @Router       /v1/servers/{serverId}/members [get]
func (h *Handler) ListMembers(c *gin.Context) {
	serverID, ok := apierr.ParseUUIDParam(c, "serverId")
	if !ok {
		return
	}

	limit, ok := apierr.ParseNonNegativeIntQuery(c, "limit", model.MemberListDefaultLimit)
	if !ok {
		return
	}
	offset, ok := apierr.ParseNonNegativeIntQuery(c, "offset", 0)
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
		resp.Members[i] = model.NewMemberResponse(m)
	}
	c.JSON(http.StatusOK, resp)
}
