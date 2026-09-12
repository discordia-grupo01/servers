// Package category_handler exposes the HTTP API for managing categories.
package category_handler

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/discordia-grupo01/servers/internal/apierr"
	"github.com/discordia-grupo01/servers/internal/handler/categoryview"
	"github.com/discordia-grupo01/servers/internal/middleware"
	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/service/category_service"
)

// CategoryUseCases is the slice of CategoryService this handler needs.
type CategoryUseCases interface {
	CreateCategory(ctx context.Context, p category_service.CreateCategoryParams) (model.Category, error)
}

type Handler struct {
	uc CategoryUseCases
}

func New(uc CategoryUseCases) *Handler {
	return &Handler{uc: uc}
}

type createCategoryRequest struct {
	Name string `json:"name"`
}

// POST /v1/servers/:serverId/categories
func (h *Handler) CreateCategory(c *gin.Context) {
	serverID, ok := apierr.ParseUUIDParam(c, "serverId")
	if !ok {
		return
	}

	var req createCategoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.RespondError(c, http.StatusBadRequest, "invalid request body", map[string]any{"reason": "invalid_json"})
		return
	}

	cat, err := h.uc.CreateCategory(c.Request.Context(), category_service.CreateCategoryParams{
		ServerID: serverID,
		Name:     req.Name,
		UserID:   middleware.UserID(c),
	})
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}
	c.JSON(http.StatusCreated, categoryview.ToResponse(cat))
}
