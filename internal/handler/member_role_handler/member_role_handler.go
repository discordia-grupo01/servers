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
	GetMemberPermissions(ctx context.Context, serverID uuid.UUID, userID string) ([]string, error)
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

// AssignRole godoc
// @Summary      Assign a role to a member
// @Description  Owner-only. Idempotent: assigning a role the member already has succeeds without changing anything.
// @Tags         member-roles
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        serverId  path  string              true  "Server ID"
// @Param        userId    path  string              true  "Target member's user ID"
// @Param        body      body  assignRoleRequest   true  "ID of the role to assign"
// @Success      200  {object}  roleview.Response
// @Failure      400  {object}  apierr.ErrorResponse
// @Failure      401  {object}  apierr.ErrorResponse
// @Failure      403  {object}  apierr.ErrorResponse  "not the owner"
// @Failure      404  {object}  apierr.ErrorResponse
// @Router       /v1/servers/{serverId}/members/{userId}/roles [post]
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

// RemoveRole godoc
// @Summary      Remove a role from a member
// @Description  Owner-only. Idempotent.
// @Tags         member-roles
// @Security     BearerAuth
// @Param        serverId  path  string  true  "Server ID"
// @Param        userId    path  string  true  "Target member's user ID"
// @Param        roleId    path  string  true  "Role ID"
// @Success      204  "no content"
// @Failure      401  {object}  apierr.ErrorResponse
// @Failure      403  {object}  apierr.ErrorResponse  "not the owner"
// @Failure      404  {object}  apierr.ErrorResponse
// @Router       /v1/servers/{serverId}/members/{userId}/roles/{roleId} [delete]
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

// ListMemberRoles godoc
// @Summary      List a member's roles
// @Tags         member-roles
// @Produce      json
// @Security     BearerAuth
// @Param        serverId  path  string  true  "Server ID"
// @Param        userId    path  string  true  "Target member's user ID"
// @Success      200  {array}   roleview.Response
// @Failure      401  {object}  apierr.ErrorResponse
// @Failure      404  {object}  apierr.ErrorResponse
// @Router       /v1/servers/{serverId}/members/{userId}/roles [get]
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

type memberPermissionsResponse struct {
	ServerID    string   `json:"server_id"`
	UserID      string   `json:"user_id"`
	Permissions []string `json:"permissions"`
}

// GetMemberPermissions godoc
// @Summary      Get a member's effective permissions
// @Description  Union of the permissions of every role assigned to the member.
// @Tags         member-roles
// @Produce      json
// @Security     BearerAuth
// @Param        serverId  path  string  true  "Server ID"
// @Param        userId    path  string  true  "Target member's user ID"
// @Success      200  {object}  memberPermissionsResponse
// @Failure      401  {object}  apierr.ErrorResponse
// @Failure      404  {object}  apierr.ErrorResponse
// @Router       /v1/servers/{serverId}/members/{userId}/permissions [get]
func (h *Handler) GetMemberPermissions(c *gin.Context) {
	serverID, ok := apierr.ParseUUIDParam(c, "serverId")
	if !ok {
		return
	}
	targetUserID := c.Param("userId")

	permissions, err := h.uc.GetMemberPermissions(c.Request.Context(), serverID, targetUserID)
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}

	c.JSON(http.StatusOK, memberPermissionsResponse{
		ServerID:    serverID.String(),
		UserID:      targetUserID,
		Permissions: permissions,
	})
}
