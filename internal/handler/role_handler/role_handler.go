// Package role_handler exposes the HTTP API for managing roles.
package role_handler

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/apierr"
	"github.com/discordia-grupo01/servers/internal/handler/roleview"
	"github.com/discordia-grupo01/servers/internal/middleware"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/service/role_service"
)

// RoleUseCases is the slice of RoleService this handler needs.
type RoleUseCases interface {
	CreateRole(ctx context.Context, p role_service.CreateRoleParams) (role.Role, error)
	ListRoles(ctx context.Context, serverID uuid.UUID) ([]role.Role, error)
	GetRole(ctx context.Context, roleID uuid.UUID) (role.Role, error)
	UpdateRolePermissions(ctx context.Context, p role_service.UpdateRolePermissionsParams) (role.Role, error)
	SetDefaultRole(ctx context.Context, p role_service.SetDefaultRoleParams) error
	DeleteRole(ctx context.Context, p role_service.DeleteRoleParams) error
	ReorderRoles(ctx context.Context, p role_service.ReorderRolesParams) ([]role.Role, error)
}

type Handler struct {
	uc RoleUseCases
}

func New(uc RoleUseCases) *Handler {
	return &Handler{uc: uc}
}

type createRoleRequest struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

// CreateRole godoc
// @Summary      Create a role
// @Description  Administrators only (the owner, or a member with ADMINISTRATOR).
// @Tags         roles
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        serverId  path  string             true  "Server ID"
// @Param        body      body  createRoleRequest  true  "Role name and color"
// @Success      201  {object}  roleview.Response
// @Failure      400  {object}  apierr.ErrorResponse
// @Failure      401  {object}  apierr.ErrorResponse
// @Failure      403  {object}  apierr.ErrorResponse  "not a server administrator"
// @Failure      404  {object}  apierr.ErrorResponse
// @Router       /v1/servers/{serverId}/roles [post]
func (h *Handler) CreateRole(c *gin.Context) {
	serverID, ok := apierr.ParseUUIDParam(c, "serverId")
	if !ok {
		return
	}

	var req createRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.RespondError(c, http.StatusBadRequest, "invalid request body", map[string]any{"reason": "invalid_json"})
		return
	}

	rl, err := h.uc.CreateRole(c.Request.Context(), role_service.CreateRoleParams{
		ServerID: serverID,
		Name:     req.Name,
		Color:    req.Color,
		UserID:   middleware.UserID(c),
	})
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}
	c.JSON(http.StatusCreated, roleview.ToResponse(rl))
}

// ListRoles godoc
// @Summary      List a server's roles
// @Tags         roles
// @Produce      json
// @Security     BearerAuth
// @Param        serverId  path  string  true  "Server ID"
// @Success      200  {array}   roleview.Response
// @Failure      401  {object}  apierr.ErrorResponse
// @Failure      404  {object}  apierr.ErrorResponse
// @Router       /v1/servers/{serverId}/roles [get]
func (h *Handler) ListRoles(c *gin.Context) {
	serverID, ok := apierr.ParseUUIDParam(c, "serverId")
	if !ok {
		return
	}

	roles, err := h.uc.ListRoles(c.Request.Context(), serverID)
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

// GetRole godoc
// @Summary      Get a role
// @Tags         roles
// @Produce      json
// @Security     BearerAuth
// @Param        roleId  path  string  true  "Role ID"
// @Success      200  {object}  roleview.Response
// @Failure      401  {object}  apierr.ErrorResponse
// @Failure      404  {object}  apierr.ErrorResponse
// @Router       /v1/roles/{roleId} [get]
func (h *Handler) GetRole(c *gin.Context) {
	roleID, ok := apierr.ParseUUIDParam(c, "roleId")
	if !ok {
		return
	}

	rl, err := h.uc.GetRole(c.Request.Context(), roleID)
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}
	c.JSON(http.StatusOK, roleview.ToResponse(rl))
}

type updateRolePermissionsRequest struct {
	Permissions []string `json:"permissions"`
	Name        *string  `json:"name"`
	Color       *string  `json:"color"`
}

// UpdateRolePermissions godoc
// @Summary      Edit a role
// @Description  Administrators only (the owner, or a member with ADMINISTRATOR). Any of `permissions`/`name`/`color` may be omitted to leave it unchanged.
// @Tags         roles
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        roleId  path  string                          true  "Role ID"
// @Param        body    body  updateRolePermissionsRequest    true  "Fields to update"
// @Success      200  {object}  roleview.Response
// @Failure      400  {object}  apierr.ErrorResponse
// @Failure      401  {object}  apierr.ErrorResponse
// @Failure      403  {object}  apierr.ErrorResponse  "not a server administrator, or removing ADMINISTRATOR from a role you hold"
// @Failure      404  {object}  apierr.ErrorResponse
// @Router       /v1/roles/{roleId} [patch]
func (h *Handler) UpdateRolePermissions(c *gin.Context) {
	roleID, ok := apierr.ParseUUIDParam(c, "roleId")
	if !ok {
		return
	}

	var req updateRolePermissionsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.RespondError(c, http.StatusBadRequest, "invalid request body", map[string]any{"reason": "invalid_json"})
		return
	}

	rl, err := h.uc.UpdateRolePermissions(c.Request.Context(), role_service.UpdateRolePermissionsParams{
		RoleID:      roleID,
		UserID:      middleware.UserID(c),
		Permissions: req.Permissions,
		Name:        req.Name,
		Color:       req.Color,
	})
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}
	c.JSON(http.StatusOK, roleview.ToResponse(rl))
}

type setDefaultRoleRequest struct {
	RoleID string `json:"role_id"`
}

type reorderRolesRequest struct {
	RoleIDs []string `json:"role_ids"`
}

// ReorderRoles godoc
// @Summary      Reorder the server's role hierarchy
// @Description  Administrators only (the owner, or a member with ADMINISTRATOR). `role_ids` is the full, final hierarchy, highest first: it must contain every role of the server exactly once. Positions are rewritten to 1..N. A non-owner administrator can only rearrange the roles below their own highest role.
// @Tags         roles
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        serverId  path  string               true  "Server ID"
// @Param        body      body  reorderRolesRequest  true  "Role IDs in their new order, highest first"
// @Success      200  {array}   roleview.Response
// @Failure      400  {object}  apierr.ErrorResponse  "invalid body, or the list doesn't match the server's roles"
// @Failure      401  {object}  apierr.ErrorResponse
// @Failure      403  {object}  apierr.ErrorResponse  "not a server administrator, or moving a role at or above your highest role"
// @Failure      404  {object}  apierr.ErrorResponse
// @Router       /v1/servers/{serverId}/roles/reorder [patch]
func (h *Handler) ReorderRoles(c *gin.Context) {
	serverID, ok := apierr.ParseUUIDParam(c, "serverId")
	if !ok {
		return
	}

	var req reorderRolesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.RespondError(c, http.StatusBadRequest, "invalid request body", map[string]any{"reason": "invalid_json"})
		return
	}
	if req.RoleIDs == nil {
		apierr.RespondError(c, http.StatusBadRequest, "role_ids must be provided", apierr.FieldErr("role_ids", "required"))
		return
	}
	roleIDs := make([]uuid.UUID, 0, len(req.RoleIDs))
	for _, raw := range req.RoleIDs {
		id, err := uuid.Parse(raw)
		if err != nil {
			apierr.RespondError(c, http.StatusBadRequest, "invalid role id in role_ids", apierr.FieldErr("role_ids", "invalid_uuid"))
			return
		}
		roleIDs = append(roleIDs, id)
	}

	roles, err := h.uc.ReorderRoles(c.Request.Context(), role_service.ReorderRolesParams{
		ServerID: serverID,
		UserID:   middleware.UserID(c),
		RoleIDs:  roleIDs,
	})
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

// SetDefaultRole godoc
// @Summary      Set the server's default role
// @Description  Administrators only (the owner, or a member with ADMINISTRATOR). The default role is assigned automatically to new members.
// @Tags         roles
// @Accept       json
// @Security     BearerAuth
// @Param        serverId  path  string                  true  "Server ID"
// @Param        body      body  setDefaultRoleRequest    true  "ID of the role to use as default"
// @Success      204  "no content"
// @Failure      400  {object}  apierr.ErrorResponse
// @Failure      401  {object}  apierr.ErrorResponse
// @Failure      403  {object}  apierr.ErrorResponse  "not a server administrator"
// @Failure      404  {object}  apierr.ErrorResponse
// @Router       /v1/servers/{serverId}/default-role [put]
func (h *Handler) SetDefaultRole(c *gin.Context) {
	serverID, ok := apierr.ParseUUIDParam(c, "serverId")
	if !ok {
		return
	}

	var req setDefaultRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.RespondError(c, http.StatusBadRequest, "invalid request body", map[string]any{"reason": "invalid_json"})
		return
	}
	roleID, err := uuid.Parse(req.RoleID)
	if err != nil {
		apierr.RespondError(c, http.StatusBadRequest, "invalid role_id", apierr.FieldErr("role_id", "invalid_uuid"))
		return
	}

	err = h.uc.SetDefaultRole(c.Request.Context(), role_service.SetDefaultRoleParams{
		ServerID: serverID,
		RoleID:   roleID,
		UserID:   middleware.UserID(c),
	})
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// DeleteRole godoc
// @Summary      Delete a role
// @Description  Administrators only (the owner, or a member with ADMINISTRATOR).
// @Tags         roles
// @Security     BearerAuth
// @Param        roleId  path  string  true  "Role ID"
// @Success      204  "no content"
// @Failure      401  {object}  apierr.ErrorResponse
// @Failure      403  {object}  apierr.ErrorResponse  "not a server administrator, or deleting a role with ADMINISTRATOR that you hold"
// @Failure      404  {object}  apierr.ErrorResponse
// @Router       /v1/roles/{roleId} [delete]
func (h *Handler) DeleteRole(c *gin.Context) {
	roleID, ok := apierr.ParseUUIDParam(c, "roleId")
	if !ok {
		return
	}

	err := h.uc.DeleteRole(c.Request.Context(), role_service.DeleteRoleParams{
		RoleID: roleID,
		UserID: middleware.UserID(c),
	})
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
