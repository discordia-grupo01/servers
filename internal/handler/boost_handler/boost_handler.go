package boost_handler

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/apierr"
	"github.com/discordia-grupo01/servers/internal/middleware"
	"github.com/discordia-grupo01/servers/internal/model"
)

type UseCases interface {
	Summary(ctx context.Context, serverID uuid.UUID, actorID string) (model.BoostSummary, error)
	SetVanity(ctx context.Context, serverID uuid.UUID, actorID string, code *string) (*string, error)
}

type Handler struct {
	uc UseCases
}

func New(uc UseCases) *Handler {
	return &Handler{uc: uc}
}

type NextLevelResponse struct {
	Level          int `json:"level" example:"2"`
	BoostsRequired int `json:"boosts_required" example:"15"`
	BoostsMissing  int `json:"boosts_missing" example:"8"`
}

type SummaryResponse struct {
	BoostCount    int                `json:"boost_count" example:"7"`
	Level         int                `json:"level" example:"1"`
	PerksUnlocked []string           `json:"perks_unlocked" example:"extra_emoji_slots,animated_icon"`
	NextLevel     *NextLevelResponse `json:"next_level"`
}

type VanityRequest struct {
	Code *string `json:"code" example:"mi-servidor"`
}

type VanityResponse struct {
	VanityCode *string `json:"vanity_code" example:"mi-servidor"`
}

// Summary godoc
// @Summary      See a server's boosts and perks
// @Description  Any member of the server. next_level is null on the last level. Perks: extra_emoji_slots and animated_icon from level 1 (5 boosts), custom_banner and larger_attachments from level 2 (15), vanity_url and boosted_badge at level 3 (30).
// @Tags         boosts
// @Produce      json
// @Security     BearerAuth
// @Param        serverId  path  string  true  "Server ID"
// @Success      200  {object}  SummaryResponse
// @Failure      401  {object}  apierr.ErrorResponse
// @Failure      403  {object}  apierr.ErrorResponse  "not a member"
// @Failure      404  {object}  apierr.ErrorResponse  "server not found"
// @Router       /v1/servers/{serverId}/boosts [get]
func (h *Handler) Summary(c *gin.Context) {
	serverID, ok := apierr.ParseUUIDParam(c, "serverId")
	if !ok {
		return
	}
	summary, err := h.uc.Summary(c.Request.Context(), serverID, middleware.UserID(c))
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}
	resp := SummaryResponse{BoostCount: summary.BoostCount, Level: summary.Level, PerksUnlocked: summary.Perks}
	if next := summary.Next; next != nil {
		resp.NextLevel = &NextLevelResponse{Level: next.Level, BoostsRequired: next.BoostsRequired, BoostsMissing: next.BoostsMissing}
	}
	c.JSON(http.StatusOK, resp)
}

// SetVanity godoc
// @Summary      Set or clear a server's vanity code
// @Description  Requires MANAGE_SERVER. Setting a code needs boost level 3; a null code clears it at any level. The code goes in POST /v1/invites/{code}/join: 3 to 32 letters, numbers or hyphens, not starting or ending with a hyphen, and stored in lowercase. If the server drops below level 3 the code is kept but stops working, and nobody else can take it.
// @Tags         boosts
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        serverId  path  string         true  "Server ID"
// @Param        body      body  VanityRequest  true  "The code, or null to clear it"
// @Success      200  {object}  VanityResponse
// @Failure      400  {object}  apierr.ErrorResponse  "invalid code or body"
// @Failure      401  {object}  apierr.ErrorResponse
// @Failure      403  {object}  apierr.ErrorResponse  "missing MANAGE_SERVER, or boost_level_required"
// @Failure      404  {object}  apierr.ErrorResponse  "server not found"
// @Failure      409  {object}  apierr.ErrorResponse  "vanity_code_taken"
// @Router       /v1/servers/{serverId}/vanity [patch]
func (h *Handler) SetVanity(c *gin.Context) {
	serverID, ok := apierr.ParseUUIDParam(c, "serverId")
	if !ok {
		return
	}
	var req VanityRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.RespondError(c, http.StatusBadRequest, "invalid request body", nil)
		return
	}
	code, err := h.uc.SetVanity(c.Request.Context(), serverID, middleware.UserID(c), req.Code)
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}
	c.JSON(http.StatusOK, VanityResponse{VanityCode: code})
}
