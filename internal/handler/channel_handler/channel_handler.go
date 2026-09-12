// Package channel_handler exposes the HTTP API for managing channels.
package channel_handler

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/apierr"
	"github.com/discordia-grupo01/servers/internal/handler/channelview"
	"github.com/discordia-grupo01/servers/internal/middleware"
	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/service/channel_service"
)

// ChannelUseCases is the slice of ChannelService this handler needs.
type ChannelUseCases interface {
	CreateChannel(ctx context.Context, p channel_service.CreateChannelParams) (model.Channel, error)
	UpdateChannel(ctx context.Context, p channel_service.UpdateChannelParams) (model.Channel, error)
	DeleteChannel(ctx context.Context, p channel_service.DeleteChannelParams) error
	MoveChannelToCategory(ctx context.Context, p channel_service.MoveChannelToCategoryParams) (model.Channel, error)
	ReorderChannels(ctx context.Context, p channel_service.ReorderChannelsParams) error
}

type Handler struct {
	uc ChannelUseCases
}

func New(uc ChannelUseCases) *Handler {
	return &Handler{uc: uc}
}

func parseOptionalUUID(c *gin.Context, field string, raw *string) (*uuid.UUID, bool) {
	if raw == nil {
		return nil, true
	}
	id, err := uuid.Parse(*raw)
	if err != nil {
		apierr.RespondError(c, http.StatusBadRequest, "invalid "+field, apierr.FieldErr(field, "invalid_uuid"))
		return nil, false
	}
	return &id, true
}

type createChannelRequest struct {
	Name       string  `json:"name"`
	Kind       string  `json:"kind"`
	CategoryID *string `json:"category_id"`
}

// POST /v1/servers/:serverId/channels
func (h *Handler) CreateChannel(c *gin.Context) {
	serverID, ok := apierr.ParseUUIDParam(c, "serverId")
	if !ok {
		return
	}

	var req createChannelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.RespondError(c, http.StatusBadRequest, "invalid request body", map[string]any{"reason": "invalid_json"})
		return
	}

	categoryID, ok := parseOptionalUUID(c, "category_id", req.CategoryID)
	if !ok {
		return
	}

	ch, err := h.uc.CreateChannel(c.Request.Context(), channel_service.CreateChannelParams{
		ServerID:   serverID,
		Name:       req.Name,
		Kind:       req.Kind,
		CategoryID: categoryID,
		UserID:     middleware.UserID(c),
	})
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}
	c.JSON(http.StatusCreated, channelview.ToResponse(ch))
}

type updateChannelRequest struct {
	Name  *string `json:"name"`
	Topic *string `json:"topic"`
}

// PATCH /v1/channels/:channelId
func (h *Handler) UpdateChannel(c *gin.Context) {
	channelID, ok := apierr.ParseUUIDParam(c, "channelId")
	if !ok {
		return
	}

	var req updateChannelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.RespondError(c, http.StatusBadRequest, "invalid request body", map[string]any{"reason": "invalid_json"})
		return
	}

	ch, err := h.uc.UpdateChannel(c.Request.Context(), channel_service.UpdateChannelParams{
		ChannelID: channelID,
		UserID:    middleware.UserID(c),
		Name:      req.Name,
		Topic:     req.Topic,
	})
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}
	c.JSON(http.StatusOK, channelview.ToResponse(ch))
}

// DELETE /v1/channels/:channelId
func (h *Handler) DeleteChannel(c *gin.Context) {
	channelID, ok := apierr.ParseUUIDParam(c, "channelId")
	if !ok {
		return
	}

	err := h.uc.DeleteChannel(c.Request.Context(), channel_service.DeleteChannelParams{
		ChannelID: channelID,
		UserID:    middleware.UserID(c),
	})
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

type moveChannelRequest struct {
	CategoryID *string `json:"category_id"`
}

// PATCH /v1/channels/:channelId/category
func (h *Handler) MoveToCategory(c *gin.Context) {
	channelID, ok := apierr.ParseUUIDParam(c, "channelId")
	if !ok {
		return
	}

	var req moveChannelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.RespondError(c, http.StatusBadRequest, "invalid request body", map[string]any{"reason": "invalid_json"})
		return
	}

	categoryID, ok := parseOptionalUUID(c, "category_id", req.CategoryID)
	if !ok {
		return
	}

	ch, err := h.uc.MoveChannelToCategory(c.Request.Context(), channel_service.MoveChannelToCategoryParams{
		ChannelID:  channelID,
		UserID:     middleware.UserID(c),
		CategoryID: categoryID,
	})
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}
	c.JSON(http.StatusOK, channelview.ToResponse(ch))
}

type reorderChannelsRequest struct {
	CategoryID *string  `json:"category_id"`
	ChannelIDs []string `json:"channel_ids"`
}

// PATCH /v1/servers/:serverId/channels/reorder
func (h *Handler) Reorder(c *gin.Context) {
	serverID, ok := apierr.ParseUUIDParam(c, "serverId")
	if !ok {
		return
	}

	var req reorderChannelsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.RespondError(c, http.StatusBadRequest, "invalid request body", map[string]any{"reason": "invalid_json"})
		return
	}

	categoryID, ok := parseOptionalUUID(c, "category_id", req.CategoryID)
	if !ok {
		return
	}

	channelIDs := make([]uuid.UUID, 0, len(req.ChannelIDs))
	for _, raw := range req.ChannelIDs {
		id, err := uuid.Parse(raw)
		if err != nil {
			apierr.RespondError(c, http.StatusBadRequest, "invalid channel id in channel_ids", apierr.FieldErr("channel_ids", "invalid_uuid"))
			return
		}
		channelIDs = append(channelIDs, id)
	}

	err := h.uc.ReorderChannels(c.Request.Context(), channel_service.ReorderChannelsParams{
		ServerID:   serverID,
		CategoryID: categoryID,
		UserID:     middleware.UserID(c),
		ChannelIDs: channelIDs,
	})
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}
	c.Status(http.StatusOK)
}
