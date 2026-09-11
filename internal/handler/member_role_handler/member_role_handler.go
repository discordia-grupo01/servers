// Package member_role_handler exposes the HTTP API for assigning and
// removing roles from a server's members.
package member_role_handler

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/apierr"
	"github.com/discordia-grupo01/servers/internal/handler/roleview"
	"github.com/discordia-grupo01/servers/internal/middleware"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/service/member_role_service"
)

// MemberRoleUseCases is the slice of MemberRoleService this handler needs.
type MemberRoleUseCases interface {
	AssignRole(ctx context.Context, p member_role_service.AssignRoleParams) (role.Role, error)
	RemoveRole(ctx context.Context, p member_role_service.RemoveRoleParams) error
	ListMemberRoles(ctx context.Context, serverID uuid.UUID, userID string) ([]role.Role, error)
}

type Handler struct {
	uc MemberRoleUseCases
}

func New(uc MemberRoleUseCases) *Handler {
	return &Handler{uc: uc}
}

type assignRoleRequest struct {
	RoleID string `json:"role_id"`
}

// POST /v1/servers/:serverId/members/:userId/roles
func (h *Handler) AssignRole(c *gin.Context) {
	serverID, ok := apierr.ParseUUIDParam(c, "serverId")
	if !ok {
		return
	}
	targetUserID := c.Param("userId")

	var req assignRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.RespondError(c, http.StatusBadRequest, "invalid request body", map[string]any{"reason": "invalid_json"})
		return
	}
	roleID, err := uuid.Parse(req.RoleID)
	if err != nil {
		apierr.RespondError(c, http.StatusBadRequest, "invalid role_id", apierr.FieldErr("role_id", "invalid_uuid"))
		return
	}

	rl, err := h.uc.AssignRole(c.Request.Context(), member_role_service.AssignRoleParams{
		ServerID:     serverID,
		TargetUserID: targetUserID,
		RoleID:       roleID,
		ActorUserID:  middleware.UserID(c),
	})
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}
	c.JSON(http.StatusOK, roleview.ToResponse(rl))
}

// DELETE /v1/servers/:serverId/members/:userId/roles/:roleId
func (h *Handler) RemoveRole(c *gin.Context) {
	serverID, ok := apierr.ParseUUIDParam(c, "serverId")
	if !ok {
		return
	}
	roleID, ok := apierr.ParseUUIDParam(c, "roleId")
	if !ok {
		return
	}
	targetUserID := c.Param("userId")

	err := h.uc.RemoveRole(c.Request.Context(), member_role_service.RemoveRoleParams{
		ServerID:     serverID,
		TargetUserID: targetUserID,
		RoleID:       roleID,
		ActorUserID:  middleware.UserID(c),
	})
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// GET /v1/servers/:serverId/members/:userId/roles
func (h *Handler) ListMemberRoles(c *gin.Context) {
	serverID, ok := apierr.ParseUUIDParam(c, "serverId")
	if !ok {
		return
	}
	targetUserID := c.Param("userId")

	roles, err := h.uc.ListMemberRoles(c.Request.Context(), serverID, targetUserID)
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}

	resp := make([]roleview.Response, 0, len(roles))
	for _, rl := range roles {
		resp = append(resp, roleview.ToResponse(rl))
	}
	c.JSON(http.StatusOK, resp)
}
