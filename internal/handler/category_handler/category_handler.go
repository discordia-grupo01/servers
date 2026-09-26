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

// CreateCategory godoc
// @Summary      Create a category
// @Description  Administrators only (the owner, or a member with ADMINISTRATOR).
// @Tags         categories
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        serverId  path  string                  true  "Server ID"
// @Param        body      body  createCategoryRequest   true  "Category name"
// @Success      201  {object}  categoryview.Response
// @Failure      400  {object}  apierr.ErrorResponse
// @Failure      401  {object}  apierr.ErrorResponse
// @Failure      403  {object}  apierr.ErrorResponse  "not a server administrator"
// @Failure      404  {object}  apierr.ErrorResponse
// @Router       /v1/servers/{serverId}/categories [post]
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

// UpdateCategory godoc
// @Summary      Rename a category
// @Description  Administrators only (the owner, or a member with ADMINISTRATOR).
// @Tags         categories
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        categoryId  path  string                  true  "Category ID"
// @Param        body        body  updateCategoryRequest   true  "New category name"
// @Success      200  {object}  categoryview.Response
// @Failure      400  {object}  apierr.ErrorResponse
// @Failure      401  {object}  apierr.ErrorResponse
// @Failure      403  {object}  apierr.ErrorResponse  "not a server administrator"
// @Failure      404  {object}  apierr.ErrorResponse
// @Router       /v1/categories/{categoryId} [patch]
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

type reorderCategoriesRequest struct {
	CategoryIDs []string `json:"category_ids"`
}

// Reorder godoc
// @Summary      Reorder a server's categories
// @Description  Administrators only (the owner, or a member with ADMINISTRATOR). `category_ids` is the full, final order.
// @Tags         categories
// @Accept       json
// @Security     BearerAuth
// @Param        serverId  path  string                      true  "Server ID"
// @Param        body      body  reorderCategoriesRequest    true  "Category IDs in their new order"
// @Success      200  "ok"
// @Failure      400  {object}  apierr.ErrorResponse
// @Failure      401  {object}  apierr.ErrorResponse
// @Failure      403  {object}  apierr.ErrorResponse  "not a server administrator"
// @Failure      404  {object}  apierr.ErrorResponse
// @Router       /v1/servers/{serverId}/categories/reorder [patch]
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
