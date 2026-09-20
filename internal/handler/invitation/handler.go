package invitation

import (
	"errors"
	"io"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/discordia-grupo01/servers/internal/apierr"
	"github.com/discordia-grupo01/servers/internal/middleware"
	"github.com/discordia-grupo01/servers/internal/model"
	invitationsvc "github.com/discordia-grupo01/servers/internal/service/invitation"
)

type Handler struct {
	uc UseCases
}

func NewHandler(uc UseCases) *Handler {
	return &Handler{uc: uc}
}

func (h *Handler) Register(r gin.IRoutes) {
	r.POST("/servers/:serverId/invites", h.generate)
	r.GET("/servers/:serverId/invites", h.list)
	r.DELETE("/invites/:code", h.revoke)
	r.POST("/invites/:code/join", h.join)
}

func toResponse(i model.Invitation) model.InvitationResponse {
	return model.InvitationResponse{
		Code:      i.Code,
		URL:       "/invite/" + i.Code,
		ServerID:  i.ServerID.String(),
		CreatedBy: i.CreatedBy,
		MaxUses:   i.MaxUses,
		Uses:      i.Uses,
		ExpiresAt: i.ExpiresAt,
		RevokedAt: i.RevokedAt,
		CreatedAt: i.CreatedAt,
	}
}

func (h *Handler) generate(c *gin.Context) {
	var req model.GenerateInvitationRequest
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		apierr.RespondError(c, http.StatusBadRequest, "invalid JSON body", map[string]any{"reason": "invalid_json"})
		return
	}

	params := invitationsvc.GenerateParams{
		ServerID:  c.Param("serverId"),
		CreatedBy: middleware.UserID(c),
		MaxUses:   req.MaxUses,
	}

	inv, err := h.uc.GenerateInvitation(c.Request.Context(), params)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, toResponse(inv))
}

func (h *Handler) list(c *gin.Context) {
	invitations, err := h.uc.ListInvitations(c.Request.Context(), invitationsvc.ListParams{
		ServerID: c.Param("serverId"),
		UserID:   middleware.UserID(c),
	})
	if err != nil {
		writeError(c, err)
		return
	}

	resp := make([]model.InvitationResponse, len(invitations))
	for i, inv := range invitations {
		resp[i] = toResponse(inv)
	}
	c.JSON(http.StatusOK, resp)
}

func (h *Handler) revoke(c *gin.Context) {
	err := h.uc.RevokeInvitation(c.Request.Context(), invitationsvc.RevokeParams{
		Code:   c.Param("code"),
		UserID: middleware.UserID(c),
	})
	if err != nil {
		writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) join(c *gin.Context) {
	res, err := h.uc.JoinServer(c.Request.Context(), invitationsvc.JoinParams{
		Code:   c.Param("code"),
		UserID: middleware.UserID(c),
	})
	if err != nil {
		writeError(c, err)
		return
	}

	status := http.StatusCreated
	if res.AlreadyMember {
		status = http.StatusOK
	}
	c.JSON(status, model.JoinResponse{ServerID: res.ServerID.String(), AlreadyMember: res.AlreadyMember})
}

func writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, model.ErrUnauthorized):
		apierr.RespondError(c, http.StatusUnauthorized, err.Error(), nil)
	case errors.Is(err, model.ErrInviteServerNotFound):
		apierr.RespondError(c, http.StatusNotFound, err.Error(), apierr.FieldErr("server_id", "server_not_found"))
	case errors.Is(err, model.ErrInvitePermissionDenied):
		apierr.RespondError(c, http.StatusForbidden, err.Error(), map[string]any{"reason": "invite_permission_denied"})
	case errors.Is(err, model.ErrInviteMaxUsesInvalid):
		apierr.RespondError(c, http.StatusBadRequest, err.Error(), apierr.FieldErr("max_uses", "max_uses_invalid"))
	case errors.Is(err, model.ErrInvitationNotFound),
		errors.Is(err, model.ErrInvitationRevoked),
		errors.Is(err, model.ErrInvitationExpired),
		errors.Is(err, model.ErrInvitationExhausted):
		apierr.RespondError(c, http.StatusNotFound, err.Error(), map[string]any{"reason": "invitation_invalid"})
	case errors.Is(err, model.ErrUserBanned):
		apierr.RespondError(c, http.StatusForbidden, err.Error(), map[string]any{"reason": "user_banned"})
	default:
		log.Printf("unmapped invitation error: %v", err)
		apierr.RespondError(c, http.StatusInternalServerError, "internal service error", nil)
	}
}
