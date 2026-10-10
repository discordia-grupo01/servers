// Package word_filter_handler exposes a server's banned words list over HTTP.
package word_filter_handler

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/discordia-grupo01/servers/internal/apierr"
	"github.com/discordia-grupo01/servers/internal/middleware"
	"github.com/discordia-grupo01/servers/internal/service/word_filter_service"
)

type UseCases interface {
	List(ctx context.Context, p word_filter_service.ListParams) ([]string, error)
	Replace(ctx context.Context, p word_filter_service.ReplaceParams) ([]string, error)
}

type Handler struct {
	uc UseCases
}

func New(uc UseCases) *Handler {
	return &Handler{uc: uc}
}

type BannedWordsRequest struct {
	Words []string `json:"words"`
}

type BannedWordsResponse struct {
	Words []string `json:"words"`
}

// List godoc
// @Summary      List the server's banned words
// @Description  Requires MANAGE_MESSAGES (the owner and administrators hold it implicitly). Words come lowercased, in the order they were saved.
// @Tags         moderation
// @Produce      json
// @Security     BearerAuth
// @Param        serverId  path  string  true  "Server ID"
// @Success      200  {object}  BannedWordsResponse
// @Failure      401  {object}  apierr.ErrorResponse
// @Failure      403  {object}  apierr.ErrorResponse  "missing MANAGE_MESSAGES"
// @Failure      404  {object}  apierr.ErrorResponse  "server not found or not a member"
// @Router       /v1/servers/{serverId}/banned-words [get]
func (h *Handler) List(c *gin.Context) {
	serverID, ok := apierr.ParseUUIDParam(c, "serverId")
	if !ok {
		return
	}
	words, err := h.uc.List(c.Request.Context(), word_filter_service.ListParams{
		ServerID: serverID, ActorUserID: middleware.UserID(c),
	})
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}
	c.JSON(http.StatusOK, BannedWordsResponse{Words: words})
}

// Replace godoc
// @Summary      Replace the server's banned words
// @Description  Requires MANAGE_MESSAGES (the owner and administrators hold it implicitly). Sends the whole list: it replaces the previous one, and an empty list removes the filter. Each entry is a word or a phrase, at most 50 characters, up to 100 entries; they are trimmed, lowercased and deduplicated. Messages that contain one are rejected by messaging from then on (matching ignores case and accents and only takes whole words); messages already sent are not touched.
// @Tags         moderation
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        serverId  path  string              true  "Server ID"
// @Param        body      body  BannedWordsRequest  true  "The whole list"
// @Success      200  {object}  BannedWordsResponse  "the list as saved"
// @Failure      400  {object}  apierr.ErrorResponse  "empty, too long or invalid word, or more than 100"
// @Failure      401  {object}  apierr.ErrorResponse
// @Failure      403  {object}  apierr.ErrorResponse  "missing MANAGE_MESSAGES"
// @Failure      404  {object}  apierr.ErrorResponse  "server not found or not a member"
// @Router       /v1/servers/{serverId}/banned-words [put]
func (h *Handler) Replace(c *gin.Context) {
	serverID, ok := apierr.ParseUUIDParam(c, "serverId")
	if !ok {
		return
	}
	var req BannedWordsRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Words == nil {
		apierr.RespondError(c, http.StatusBadRequest, "invalid request body", map[string]any{"reason": "invalid_json"})
		return
	}
	words, err := h.uc.Replace(c.Request.Context(), word_filter_service.ReplaceParams{
		ServerID: serverID, ActorUserID: middleware.UserID(c), Words: req.Words,
	})
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}
	c.JSON(http.StatusOK, BannedWordsResponse{Words: words})
}
