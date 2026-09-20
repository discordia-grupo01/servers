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

// POST /v1/servers/:serverId/roles
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

// GET /v1/servers/:serverId/roles
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

// GET /v1/roles/:roleId
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

// PATCH /v1/roles/:roleId
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

// PUT /v1/servers/:serverId/default-role
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

// DELETE /v1/roles/:roleId
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
