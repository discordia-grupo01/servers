// Package ownership_transfer_handler exposes the HTTP API for transferring a
// server's ownership to another member.
package ownership_transfer_handler

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/discordia-grupo01/servers/internal/apierr"
	"github.com/discordia-grupo01/servers/internal/middleware"
	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/service/ownership_transfer_service"
)

type Handler struct {
	uc UseCases
}

func New(uc UseCases) *Handler {
	return &Handler{uc: uc}
}

func toResponse(t model.OwnershipTransfer) model.OwnershipTransferResponse {
	return model.OwnershipTransferResponse{
		ID:         t.ID.String(),
		ServerID:   t.ServerID.String(),
		FromUserID: t.FromUserID,
		ToUserID:   t.ToUserID,
		Status:     string(t.Status),
		CreatedAt:  t.CreatedAt,
		ResolvedAt: t.ResolvedAt,
	}
}

// POST /v1/servers/:serverId/ownership-transfers
func (h *Handler) Initiate(c *gin.Context) {
	serverID, ok := apierr.ParseUUIDParam(c, "serverId")
	if !ok {
		return
	}

	var req model.InitiateOwnershipTransferRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.RespondError(c, http.StatusBadRequest, "invalid request body", map[string]any{"reason": "invalid_json"})
		return
	}

	t, err := h.uc.InitiateTransfer(c.Request.Context(), ownership_transfer_service.InitiateParams{
		ServerID:    serverID,
		ActorUserID: middleware.UserID(c),
		ToUserID:    strings.TrimSpace(req.ToUserID),
	})
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}
	c.JSON(http.StatusCreated, toResponse(t))
}

// GET /v1/servers/:serverId/ownership-transfers/pending
func (h *Handler) GetPending(c *gin.Context) {
	serverID, ok := apierr.ParseUUIDParam(c, "serverId")
	if !ok {
		return
	}

	t, err := h.uc.GetPendingTransfer(c.Request.Context(), ownership_transfer_service.GetPendingParams{
		ServerID:    serverID,
		ActorUserID: middleware.UserID(c),
	})
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}
	c.JSON(http.StatusOK, toResponse(t))
}

// POST /v1/servers/:serverId/ownership-transfers/:transferId/accept
func (h *Handler) Accept(c *gin.Context) {
	t, ok := h.respond(c, func(uc UseCases, ctx context.Context, p ownership_transfer_service.RespondParams) (model.OwnershipTransfer, error) {
		return uc.AcceptTransfer(ctx, p)
	})
	if !ok {
		return
	}
	c.JSON(http.StatusOK, toResponse(t))
}

// POST /v1/servers/:serverId/ownership-transfers/:transferId/reject
func (h *Handler) Reject(c *gin.Context) {
	t, ok := h.respond(c, func(uc UseCases, ctx context.Context, p ownership_transfer_service.RespondParams) (model.OwnershipTransfer, error) {
		return uc.RejectTransfer(ctx, p)
	})
	if !ok {
		return
	}
	c.JSON(http.StatusOK, toResponse(t))
}

func (h *Handler) respond(
	c *gin.Context,
	call func(UseCases, context.Context, ownership_transfer_service.RespondParams) (model.OwnershipTransfer, error),
) (model.OwnershipTransfer, bool) {
	serverID, ok := apierr.ParseUUIDParam(c, "serverId")
	if !ok {
		return model.OwnershipTransfer{}, false
	}
	transferID, ok := apierr.ParseUUIDParam(c, "transferId")
	if !ok {
		return model.OwnershipTransfer{}, false
	}

	t, err := call(h.uc, c.Request.Context(), ownership_transfer_service.RespondParams{
		ServerID:    serverID,
		TransferID:  transferID,
		ActorUserID: middleware.UserID(c),
	})
	if err != nil {
		apierr.WriteDomainError(c, err)
		return model.OwnershipTransfer{}, false
	}
	return t, true
}
