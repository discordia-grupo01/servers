// Package apierr formats domain errors as the service's standard HTTP error
// body. It lives outside internal/handler (and its role_handler /
// member_role_handler subpackages) so any of them can depend on it without
// creating an import cycle back into internal/handler.
package apierr

import (
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/moderation"
)

// ErrorResponse is the service's standard error body, exported (rather than
// kept package-private) so swag can reference it in `@Failure` annotations
// across handler packages.
type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

type ErrorDetail struct {
	Code    int            `json:"code" example:"404"`
	Message string         `json:"message" example:"server not found"`
	Details map[string]any `json:"details"`
}

// RespondError writes the standard {"error":{"code","message","details"}}
// body. code is always the same number as the HTTP status.
func RespondError(c *gin.Context, status int, message string, details map[string]any) {
	if details == nil {
		details = map[string]any{}
	}
	c.JSON(status, ErrorResponse{Error: ErrorDetail{Code: status, Message: message, Details: details}})
}

// FieldErr builds a {"field","reason"} details map for validation errors.
func FieldErr(field, reason string) map[string]any {
	return map[string]any{"field": field, "reason": reason}
}

// ParseUUIDParam parses the named gin path parameter as a UUID, writing a
// standard 400 response and returning ok=false on failure.
func ParseUUIDParam(c *gin.Context, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil {
		RespondError(c, http.StatusBadRequest, "invalid "+name, FieldErr(name, "invalid_uuid"))
		return uuid.UUID{}, false
	}
	return id, true
}

// ParseNonNegativeIntQuery reads an optional non-negative integer query
// param, falling back to def when absent. It writes a 400 response and
// returns ok=false when the value is present but not a non-negative integer.
func ParseNonNegativeIntQuery(c *gin.Context, name string, def int) (int, bool) {
	raw := c.Query(name)
	if raw == "" {
		return def, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		RespondError(c, http.StatusBadRequest, "invalid "+name, FieldErr(name, "must_be_a_non_negative_integer"))
		return 0, false
	}
	return n, true
}

// WriteDomainError maps a domain error (from internal/model) to its HTTP
// response. Unrecognized errors are logged and returned as a generic 500 so
// driver/internal details never leak to the client.
func WriteDomainError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, model.ErrNameRequired):
		RespondError(c, http.StatusBadRequest, err.Error(), FieldErr("name", "name_required"))
	case errors.Is(err, model.ErrNameTooShort):
		RespondError(c, http.StatusBadRequest, err.Error(), FieldErr("name", "name_too_short"))
	case errors.Is(err, model.ErrNameTooLong):
		RespondError(c, http.StatusBadRequest, err.Error(), FieldErr("name", "name_too_long"))
	case errors.Is(err, model.ErrNameInvalidChars):
		RespondError(c, http.StatusBadRequest, err.Error(), FieldErr("name", "name_invalid_chars"))
	case errors.Is(err, model.ErrNameTaken):
		RespondError(c, http.StatusConflict, err.Error(), FieldErr("name", "name_taken"))
	case errors.Is(err, model.ErrIconTooLarge):
		RespondError(c, http.StatusBadRequest, err.Error(), FieldErr("icon", "icon_too_large"))
	case errors.Is(err, model.ErrIconUnsupportedType):
		RespondError(c, http.StatusBadRequest, err.Error(), FieldErr("icon", "icon_unsupported_type"))
	case errors.Is(err, model.ErrUnauthorized):
		RespondError(c, http.StatusUnauthorized, err.Error(), nil)

	case errors.Is(err, model.ErrForbiddenNotOwner):
		RespondError(c, http.StatusForbidden, err.Error(), nil)
	case errors.Is(err, model.ErrForbiddenNotMember):
		RespondError(c, http.StatusForbidden, err.Error(), nil)
	case errors.Is(err, model.ErrForbiddenNotAdmin):
		RespondError(c, http.StatusForbidden, err.Error(), nil)
	case errors.Is(err, model.ErrCannotRemoveOwnAdmin):
		RespondError(c, http.StatusForbidden, err.Error(), map[string]any{"reason": "cannot_remove_own_admin"})
	case errors.Is(err, model.ErrLeaveForbidden):
		RespondError(c, http.StatusForbidden, err.Error(), nil)
	case errors.Is(err, model.ErrOwnerCannotLeave):
		RespondError(c, http.StatusConflict, err.Error(), map[string]any{"reason": "owner_must_transfer_or_delete"})
	case errors.Is(err, model.ErrServerNotFound):
		RespondError(c, http.StatusNotFound, err.Error(), nil)

	case errors.Is(err, model.ErrTransferTargetRequired):
		RespondError(c, http.StatusBadRequest, err.Error(), FieldErr("to_user_id", "required"))
	case errors.Is(err, model.ErrTransferTargetNotMember):
		RespondError(c, http.StatusBadRequest, err.Error(), FieldErr("to_user_id", "not_a_member"))
	case errors.Is(err, model.ErrTransferTargetIsOwner):
		RespondError(c, http.StatusBadRequest, err.Error(), FieldErr("to_user_id", "already_owner"))
	case errors.Is(err, model.ErrOwnershipTransferNotFound):
		RespondError(c, http.StatusNotFound, err.Error(), nil)
	case errors.Is(err, model.ErrOwnershipTransferServerMismatch):
		RespondError(c, http.StatusBadRequest, err.Error(), nil)
	case errors.Is(err, model.ErrOwnershipTransferWrongTarget):
		RespondError(c, http.StatusForbidden, err.Error(), nil)
	case errors.Is(err, model.ErrOwnershipTransferNotInitiator):
		RespondError(c, http.StatusForbidden, err.Error(), nil)
	case errors.Is(err, model.ErrOwnershipTransferNotPending):
		RespondError(c, http.StatusConflict, err.Error(), map[string]any{"reason": "transfer_not_pending"})
	case errors.Is(err, model.ErrOwnershipTransferAlreadyPending):
		RespondError(c, http.StatusConflict, err.Error(), map[string]any{"reason": "transfer_already_pending"})
	case errors.Is(err, model.ErrRoleNotFound):
		RespondError(c, http.StatusNotFound, err.Error(), nil)
	case errors.Is(err, model.ErrRoleNameRequired):
		RespondError(c, http.StatusBadRequest, err.Error(), FieldErr("name", "name_required"))
	case errors.Is(err, model.ErrRoleNameTooLong):
		RespondError(c, http.StatusBadRequest, err.Error(), FieldErr("name", "name_too_long"))
	case errors.Is(err, model.ErrColorInvalid):
		RespondError(c, http.StatusBadRequest, err.Error(), FieldErr("color", "color_invalid_format"))
	case errors.Is(err, model.ErrInvalidPermission):
		details := map[string]any{"field": "permissions"}
		var ipe *model.InvalidPermissionError
		if errors.As(err, &ipe) {
			details["invalid_value"] = ipe.Value
		}
		RespondError(c, http.StatusBadRequest, err.Error(), details)
	case errors.Is(err, model.ErrRoleServerMismatch):
		RespondError(c, http.StatusBadRequest, err.Error(), nil)
	case errors.Is(err, model.ErrRoleReorderInvalid):
		RespondError(c, http.StatusBadRequest, err.Error(), FieldErr("role_ids", "reorder_invalid"))
	case errors.Is(err, model.ErrRoleReorderAboveOwn):
		RespondError(c, http.StatusForbidden, err.Error(), map[string]any{"reason": "role_above_own"})
	case errors.Is(err, model.ErrForbiddenMissingPermission):
		RespondError(c, http.StatusForbidden, err.Error(), map[string]any{"reason": "missing_permission"})
	case errors.Is(err, model.ErrBanTargetRequired):
		RespondError(c, http.StatusBadRequest, err.Error(), FieldErr("user_id", "required"))
	case errors.Is(err, model.ErrCannotBanSelf):
		RespondError(c, http.StatusBadRequest, err.Error(), FieldErr("user_id", "cannot_ban_self"))
	case errors.Is(err, model.ErrCannotBanOwner):
		RespondError(c, http.StatusForbidden, err.Error(), map[string]any{"reason": "cannot_ban_owner"})
	case errors.Is(err, model.ErrTargetOutranksActor):
		RespondError(c, http.StatusForbidden, err.Error(), map[string]any{"reason": "target_outranks_actor"})
	case errors.Is(err, model.ErrAlreadyBanned):
		RespondError(c, http.StatusConflict, err.Error(), map[string]any{"reason": "already_banned"})
	case errors.Is(err, model.ErrBanNotFound):
		RespondError(c, http.StatusNotFound, err.Error(), map[string]any{"reason": "ban_not_found"})
	case errors.Is(err, moderation.ErrReasonTooLong):
		RespondError(c, http.StatusBadRequest, err.Error(), FieldErr("reason", "reason_too_long"))
	case errors.Is(err, moderation.ErrReasonInvalidChars):
		RespondError(c, http.StatusBadRequest, err.Error(), FieldErr("reason", "reason_invalid_chars"))
	case errors.Is(err, model.ErrRoleIsServerDefault):
		RespondError(c, http.StatusConflict, err.Error(), nil)
	case errors.Is(err, model.ErrMemberNotFound):
		RespondError(c, http.StatusNotFound, err.Error(), nil)
	case errors.Is(err, model.ErrMemberRoleNotFound):
		RespondError(c, http.StatusNotFound, err.Error(), nil)

	case errors.Is(err, model.ErrChannelNotFound):
		RespondError(c, http.StatusNotFound, err.Error(), nil)
	case errors.Is(err, model.ErrChannelNameRequired):
		RespondError(c, http.StatusBadRequest, err.Error(), FieldErr("name", "name_required"))
	case errors.Is(err, model.ErrChannelNameTooLong):
		RespondError(c, http.StatusBadRequest, err.Error(), FieldErr("name", "name_too_long"))
	case errors.Is(err, model.ErrChannelNameInvalidChars):
		RespondError(c, http.StatusBadRequest, err.Error(), FieldErr("name", "name_invalid_chars"))
	case errors.Is(err, model.ErrChannelNameTaken):
		RespondError(c, http.StatusBadRequest, err.Error(), FieldErr("name", "name_taken"))
	case errors.Is(err, model.ErrChannelKindInvalid):
		RespondError(c, http.StatusBadRequest, err.Error(), FieldErr("kind", "kind_invalid"))
	case errors.Is(err, model.ErrChannelServerMismatch):
		RespondError(c, http.StatusBadRequest, err.Error(), nil)
	case errors.Is(err, model.ErrChannelReorderInvalid):
		RespondError(c, http.StatusBadRequest, err.Error(), FieldErr("channel_ids", "reorder_invalid"))

	case errors.Is(err, model.ErrCategoryNotFound):
		RespondError(c, http.StatusNotFound, err.Error(), nil)
	case errors.Is(err, model.ErrCategoryNameRequired):
		RespondError(c, http.StatusBadRequest, err.Error(), FieldErr("name", "name_required"))
	case errors.Is(err, model.ErrCategoryNameTooLong):
		RespondError(c, http.StatusBadRequest, err.Error(), FieldErr("name", "name_too_long"))
	case errors.Is(err, model.ErrCategoryNameInvalidChars):
		RespondError(c, http.StatusBadRequest, err.Error(), FieldErr("name", "name_invalid_chars"))
	case errors.Is(err, model.ErrCategoryServerMismatch):
		RespondError(c, http.StatusBadRequest, err.Error(), FieldErr("category_id", "category_server_mismatch"))
	case errors.Is(err, model.ErrCategoryReorderInvalid):
		RespondError(c, http.StatusBadRequest, err.Error(), FieldErr("category_ids", "reorder_invalid"))

	default:
		log.Printf("unmapped internal error: %v", err)
		RespondError(c, http.StatusInternalServerError, "internal service error", nil)
	}
}
