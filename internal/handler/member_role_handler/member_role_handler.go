// Package member_role_handler exposes the HTTP API for assigning and
// removing roles from a server's members.
package member_role_handler

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/apierr"
	"github.com/discordia-grupo01/servers/internal/middleware"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/service/member_role_service"
)

// MemberRoleUseCases is the slice of MemberRoleService this handler needs.
type MemberRoleUseCases interface {
	AssignRole(ctx context.Context, p member_role_service.AssignRoleParams) error
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

type roleResponse struct {
	ID          string    `json:"id"`
	ServerID    string    `json:"server_id"`
	Name        string    `json:"name"`
	Color       string    `json:"color"`
	Permissions []string  `json:"permissions"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func toRoleResponse(r role.Role) roleResponse {
	return roleResponse{
		ID:          r.ID.String(),
		ServerID:    r.ServerID.String(),
		Name:        r.Name,
		Color:       r.Color,
		Permissions: role.PermissionNames(r.Permissions),
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
}

func parseUUIDParam(c *gin.Context, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil {
		apierr.RespondError(c, http.StatusBadRequest, "invalid "+name, apierr.FieldErr(name, "invalid_uuid"))
		return uuid.UUID{}, false
	}
	return id, true
}

// POST /v1/servers/:serverId/members/:userId/roles
func (h *Handler) AssignRole(c *gin.Context) {
	serverID, ok := parseUUIDParam(c, "serverId")
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

	err = h.uc.AssignRole(c.Request.Context(), member_role_service.AssignRoleParams{
		ServerID:     serverID,
		TargetUserID: targetUserID,
		RoleID:       roleID,
		ActorUserID:  middleware.UserID(c),
	})
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}
	c.Status(http.StatusOK)
}

// DELETE /v1/servers/:serverId/members/:userId/roles/:roleId
func (h *Handler) RemoveRole(c *gin.Context) {
	serverID, ok := parseUUIDParam(c, "serverId")
	if !ok {
		return
	}
	roleID, ok := parseUUIDParam(c, "roleId")
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
	serverID, ok := parseUUIDParam(c, "serverId")
	if !ok {
		return
	}
	targetUserID := c.Param("userId")

	roles, err := h.uc.ListMemberRoles(c.Request.Context(), serverID, targetUserID)
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}

	resp := make([]roleResponse, 0, len(roles))
	for _, rl := range roles {
		resp = append(resp, toRoleResponse(rl))
	}
	c.JSON(http.StatusOK, resp)
}
