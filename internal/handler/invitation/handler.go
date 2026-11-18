package invitation

import (
	"errors"
	"io"
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

// generate godoc
// @Summary      Generate an invitation
// @Description  Administrators, or members whose roles grant CREATE_INVITE. Body is optional JSON; omitting max_uses means unlimited uses. Expires in 7 days.
// @Tags         invitations
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        serverId  path  string                             true   "Server ID" Format(uuid)
// @Param        body      body  model.GenerateInvitationRequest    false  "Optional max_uses"
// @Success      201  {object}  model.InvitationResponse
// @Failure      400  {object}  apierr.ErrorResponse  "invalid max_uses"
// @Failure      401  {object}  apierr.ErrorResponse
// @Failure      403  {object}  apierr.ErrorResponse  "caller cannot invite (not an administrator nor holds CREATE_INVITE)"
// @Failure      404  {object}  apierr.ErrorResponse
// @Router       /v1/servers/{serverId}/invites [post]
func (h *Handler) generate(c *gin.Context) {
	var req model.GenerateInvitationRequest
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		apierr.RespondError(c, http.StatusBadRequest, apierr.CodeInvalidBody, "invalid JSON body", map[string]any{"reason": "invalid_json"})
		return
	}

	params := invitationsvc.GenerateParams{
		ServerID:  c.Param("serverId"),
		CreatedBy: middleware.UserID(c),
		MaxUses:   req.MaxUses,
	}

	inv, err := h.uc.GenerateInvitation(c.Request.Context(), params)
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}
	c.JSON(http.StatusCreated, toResponse(inv))
}

// list godoc
// @Summary      List a server's invitations
// @Description  Same permission as generating one: administrators, or members whose roles grant CREATE_INVITE. Returns all invitations (active, revoked or expired, no status filter on the backend), newest first.
// @Tags         invitations
// @Produce      json
// @Security     BearerAuth
// @Param        serverId  path  string  true  "Server ID" Format(uuid)
// @Success      200  {array}   model.InvitationResponse
// @Failure      401  {object}  apierr.ErrorResponse
// @Failure      403  {object}  apierr.ErrorResponse  "caller cannot invite (not an administrator nor holds CREATE_INVITE)"
// @Failure      404  {object}  apierr.ErrorResponse
// @Failure      400  {object}  apierr.ErrorResponse
// @Router       /v1/servers/{serverId}/invites [get]
func (h *Handler) list(c *gin.Context) {
	invitations, err := h.uc.ListInvitations(c.Request.Context(), invitationsvc.ListParams{
		ServerID: c.Param("serverId"),
		UserID:   middleware.UserID(c),
	})
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}

	resp := make([]model.InvitationResponse, len(invitations))
	for i, inv := range invitations {
		resp[i] = toResponse(inv)
	}
	c.JSON(http.StatusOK, resp)
}

// revoke godoc
// @Summary      Revoke an invitation
// @Description  Administrators revoke any invitation of the server; any member can revoke the ones they created. Idempotent: revoking an already-revoked code still returns 204.
// @Tags         invitations
// @Security     BearerAuth
// @Param        code  path  string  true  "Invitation code"
// @Success      204  "no content"
// @Failure      401  {object}  apierr.ErrorResponse
// @Failure      403  {object}  apierr.ErrorResponse  "not an administrator nor the invitation's creator"
// @Failure      404  {object}  apierr.ErrorResponse  "invitation not found"
// @Failure      400  {object}  apierr.ErrorResponse
// @Router       /v1/invites/{code} [delete]
func (h *Handler) revoke(c *gin.Context) {
	err := h.uc.RevokeInvitation(c.Request.Context(), invitationsvc.RevokeParams{
		Code:   c.Param("code"),
		UserID: middleware.UserID(c),
	})
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// join godoc
// @Summary      Join a server via an invitation code
// @Description  One-step join (no separate "preview" endpoint). Returns 200 instead of 201 if the caller was already a member.
// @Tags         invitations
// @Produce      json
// @Security     BearerAuth
// @Param        code  path  string  true  "Invitation code"
// @Success      201  {object}  model.JoinResponse
// @Success      200  {object}  model.JoinResponse  "already a member"
// @Failure      401  {object}  apierr.ErrorResponse
// @Failure      403  {object}  apierr.ErrorResponse  "user is banned from the server"
// @Failure      404  {object}  apierr.ErrorResponse  "invalid, revoked, expired or exhausted code"
// @Failure      400  {object}  apierr.ErrorResponse
// @Router       /v1/invites/{code}/join [post]
func (h *Handler) join(c *gin.Context) {
	res, err := h.uc.JoinServer(c.Request.Context(), invitationsvc.JoinParams{
		Code:   c.Param("code"),
		UserID: middleware.UserID(c),
	})
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}

	status := http.StatusCreated
	if res.AlreadyMember {
		status = http.StatusOK
	}
	c.JSON(status, model.JoinResponse{ServerID: res.ServerID.String(), AlreadyMember: res.AlreadyMember})
}
