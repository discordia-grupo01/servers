// Package role_handler exposes the HTTP API for managing roles.
package role_handler

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/apierr"
	"github.com/discordia-grupo01/servers/internal/middleware"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/service/role_service"
)

// RoleUseCases is the slice of RoleService this handler needs.
type RoleUseCases interface {
	CreateRole(ctx context.Context, p role_service.CreateRoleParams) (role.Role, error)
	ListRoles(ctx context.Context, serverID uuid.UUID) ([]role.Role, error)
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

// POST /v1/servers/:serverId/roles
func (h *Handler) CreateRole(c *gin.Context) {
	serverID, ok := parseUUIDParam(c, "serverId")
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
	c.JSON(http.StatusCreated, toRoleResponse(rl))
}

// GET /v1/servers/:serverId/roles
func (h *Handler) ListRoles(c *gin.Context) {
	serverID, ok := parseUUIDParam(c, "serverId")
	if !ok {
		return
	}

	roles, err := h.uc.ListRoles(c.Request.Context(), serverID)
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
