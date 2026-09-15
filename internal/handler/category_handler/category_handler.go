// Package category_handler exposes the HTTP API for managing categories.
package category_handler

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/apierr"
	"github.com/discordia-grupo01/servers/internal/handler/categoryview"
	"github.com/discordia-grupo01/servers/internal/middleware"
	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/service/category_service"
)

// CategoryUseCases is the slice of CategoryService this handler needs.
type CategoryUseCases interface {
	CreateCategory(ctx context.Context, p category_service.CreateCategoryParams) (model.Category, error)
	UpdateCategory(ctx context.Context, p category_service.UpdateCategoryParams) (model.Category, error)
	DeleteCategory(ctx context.Context, p category_service.DeleteCategoryParams) error
	ReorderCategories(ctx context.Context, p category_service.ReorderCategoriesParams) error
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

type updateCategoryRequest struct {
	Name string `json:"name"`
}

// PATCH /v1/categories/:categoryId
func (h *Handler) UpdateCategory(c *gin.Context) {
	categoryID, ok := apierr.ParseUUIDParam(c, "categoryId")
	if !ok {
		return
	}

	var req updateCategoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.RespondError(c, http.StatusBadRequest, "invalid request body", map[string]any{"reason": "invalid_json"})
		return
	}

	cat, err := h.uc.UpdateCategory(c.Request.Context(), category_service.UpdateCategoryParams{
		CategoryID: categoryID,
		Name:       req.Name,
		UserID:     middleware.UserID(c),
	})
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}
	c.JSON(http.StatusOK, categoryview.ToResponse(cat))
}

// DELETE /v1/categories/:categoryId
func (h *Handler) DeleteCategory(c *gin.Context) {
	categoryID, ok := apierr.ParseUUIDParam(c, "categoryId")
	if !ok {
		return
	}

	err := h.uc.DeleteCategory(c.Request.Context(), category_service.DeleteCategoryParams{
		CategoryID: categoryID,
		UserID:     middleware.UserID(c),
	})
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

type reorderCategoriesRequest struct {
	CategoryIDs []string `json:"category_ids"`
}

// PATCH /v1/servers/:serverId/categories/reorder
func (h *Handler) Reorder(c *gin.Context) {
	serverID, ok := apierr.ParseUUIDParam(c, "serverId")
	if !ok {
		return
	}

	var req reorderCategoriesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.RespondError(c, http.StatusBadRequest, "invalid request body", map[string]any{"reason": "invalid_json"})
		return
	}

	categoryIDs := make([]uuid.UUID, 0, len(req.CategoryIDs))
	for _, raw := range req.CategoryIDs {
		id, err := uuid.Parse(raw)
		if err != nil {
			apierr.RespondError(c, http.StatusBadRequest, "invalid category id in category_ids", apierr.FieldErr("category_ids", "invalid_uuid"))
			return
		}
		categoryIDs = append(categoryIDs, id)
	}

	err := h.uc.ReorderCategories(c.Request.Context(), category_service.ReorderCategoriesParams{
		ServerID:    serverID,
		UserID:      middleware.UserID(c),
		CategoryIDs: categoryIDs,
	})
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}
	c.Status(http.StatusOK)
}
