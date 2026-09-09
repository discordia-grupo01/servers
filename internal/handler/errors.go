package handler

import (
	"errors"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/discordia-grupo01/servers/internal/model"
)

type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    int            `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details"`
}

func respondError(c *gin.Context, status int, message string, details map[string]any) {
	if details == nil {
		details = map[string]any{}
	}
	c.JSON(status, errorBody{Error: errorDetail{Code: status, Message: message, Details: details}})
}

func fieldErr(field, reason string) map[string]any {
	return map[string]any{"field": field, "reason": reason}
}

func writeDomainError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, model.ErrNameRequired):
		respondError(c, http.StatusBadRequest, err.Error(), fieldErr("name", "name_required"))
	case errors.Is(err, model.ErrNameTooShort):
		respondError(c, http.StatusBadRequest, err.Error(), fieldErr("name", "name_too_short"))
	case errors.Is(err, model.ErrNameTooLong):
		respondError(c, http.StatusBadRequest, err.Error(), fieldErr("name", "name_too_long"))
	case errors.Is(err, model.ErrNameInvalidChars):
		respondError(c, http.StatusBadRequest, err.Error(), fieldErr("name", "name_invalid_chars"))
	case errors.Is(err, model.ErrNameTaken):
		respondError(c, http.StatusConflict, err.Error(), fieldErr("name", "name_taken"))
	case errors.Is(err, model.ErrIconTooLarge):
		respondError(c, http.StatusBadRequest, err.Error(), fieldErr("icon", "icon_too_large"))
	case errors.Is(err, model.ErrIconUnsupportedType):
		respondError(c, http.StatusBadRequest, err.Error(), fieldErr("icon", "icon_unsupported_type"))
	case errors.Is(err, model.ErrUnauthorized):
		respondError(c, http.StatusUnauthorized, err.Error(), nil)
	default:
		log.Printf("unmapped internal error: %v", err)
		respondError(c, http.StatusInternalServerError, "internal service error", nil)
	}
}
