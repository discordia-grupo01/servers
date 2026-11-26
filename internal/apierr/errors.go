// Package apierr formats domain errors as the service's standard HTTP error
// body. It lives outside internal/handler (and its role_handler /
// member_role_handler subpackages) so any of them can depend on it without
// creating an import cycle back into internal/handler.
package apierr

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/mention"
	"github.com/discordia-grupo01/servers/internal/model/moderation"
)

// ErrorResponse is the service's standard error body, exported (rather than
// kept package-private) so swag can reference it in `@Failure` annotations
// across handler packages.
type ErrorResponse struct {
	Error ErrorDetail `json:"error" binding:"required"`
}

type ErrorDetail struct {
	Code    string         `json:"code" binding:"required" example:"SERVER_NOT_FOUND"`
	Message string         `json:"message" binding:"required" minLength:"1" example:"server not found"`
	Details map[string]any `json:"details" binding:"required"`
}

// RespondError writes the standard {"error":{"code","message","details"}}
// body. code is a SCREAMING_SNAKE_CASE string (see codes.go) and a nil details
// is serialized as {}. It is the only place that builds the error body.
func RespondError(c *gin.Context, status int, code, message string, details map[string]any) {
	if details == nil {
		details = map[string]any{}
	}
	c.JSON(status, ErrorResponse{Error: ErrorDetail{Code: code, Message: message, Details: details}})
}

// Abort is RespondError followed by c.Abort, for middlewares and recovery.
func Abort(c *gin.Context, status int, code, message string, details map[string]any) {
	c.Abort()
	RespondError(c, status, code, message, details)
}

// InvalidJSON responds 400 INVALID_BODY for a body that cannot be parsed.
func InvalidJSON(c *gin.Context) {
	RespondError(c, http.StatusBadRequest, CodeInvalidBody, "invalid request body", map[string]any{"reason": "invalid_json"})
}

// InvalidUUIDBody responds 400 INVALID_BODY for a body field that is not a UUID.
func InvalidUUIDBody(c *gin.Context, field, message string) {
	RespondError(c, http.StatusBadRequest, CodeInvalidBody, message, FieldErr(field, "invalid_uuid"))
}

// Unauthorized responds 401 UNAUTHORIZED and aborts the request.
func Unauthorized(c *gin.Context, message string) {
	Abort(c, http.StatusUnauthorized, CodeUnauthorized, message, nil)
}

// NotFound responds 404 NOT_FOUND for a route that does not exist.
func NotFound(c *gin.Context) {
	RespondError(c, http.StatusNotFound, CodeNotFound, "route not found", nil)
}

// MethodNotAllowed responds 405 METHOD_NOT_ALLOWED.
func MethodNotAllowed(c *gin.Context) {
	RespondError(c, http.StatusMethodNotAllowed, CodeMethodNotAllowed, "method not allowed", nil)
}

// Internal responds 500 INTERNAL_ERROR with a generic message and aborts.
func Internal(c *gin.Context) {
	Abort(c, http.StatusInternalServerError, CodeInternal, "internal service error", nil)
}

// ServiceUnavailable responds 503 SERVICE_UNAVAILABLE and aborts.
func ServiceUnavailable(c *gin.Context, message string) {
	Abort(c, http.StatusServiceUnavailable, CodeServiceUnavailable, message, nil)
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
		RespondError(c, http.StatusBadRequest, CodeInvalidParameter, "invalid "+name,
			map[string]any{"param": name, "field": name, "reason": "invalid_uuid"})
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
		RespondError(c, http.StatusBadRequest, CodeInvalidQuery, "invalid "+name,
			map[string]any{"param": name, "field": name, "reason": "must_be_a_non_negative_integer"})
		return 0, false
	}
	return n, true
}

func ParseBoolQuery(c *gin.Context, name string, def bool) (bool, bool) {
	raw := c.Query(name)
	if raw == "" {
		return def, true
	}
	b, err := strconv.ParseBool(raw)
	if err != nil {
		RespondError(c, http.StatusBadRequest, CodeInvalidQuery, "invalid "+name,
			map[string]any{"param": name, "field": name, "reason": "must_be_a_boolean"})
		return false, false
	}
	return b, true
}

// WriteDomainError maps a domain error (from internal/model) to its HTTP
// response. Unrecognized errors are logged and returned as a generic 500 so
// driver/internal details never leak to the client.
func WriteDomainError(c *gin.Context, err error) {
	var levelRequired *model.BoostLevelRequiredError
	switch {
	case errors.Is(err, model.ErrNameRequired):
		RespondError(c, http.StatusBadRequest, CodeNameRequired, err.Error(), FieldErr("name", "name_required"))
	case errors.Is(err, model.ErrNameTooShort):
		RespondError(c, http.StatusBadRequest, CodeNameTooShort, err.Error(), FieldErr("name", "name_too_short"))
	case errors.Is(err, model.ErrNameTooLong):
		RespondError(c, http.StatusBadRequest, CodeNameTooLong, err.Error(), FieldErr("name", "name_too_long"))
	case errors.Is(err, model.ErrNameInvalidChars):
		RespondError(c, http.StatusBadRequest, CodeNameInvalidChars, err.Error(), FieldErr("name", "name_invalid_chars"))
	case errors.Is(err, model.ErrNameTaken):
		RespondError(c, http.StatusConflict, CodeNameTaken, err.Error(), FieldErr("name", "name_taken"))
	case errors.Is(err, model.ErrIconTooLarge):
		RespondError(c, http.StatusBadRequest, CodeIconTooLarge, err.Error(), FieldErr("icon", "icon_too_large"))
	case errors.Is(err, model.ErrIconUnsupportedType):
		RespondError(c, http.StatusBadRequest, CodeIconUnsupportedType, err.Error(), FieldErr("icon", "icon_unsupported_type"))
	case errors.Is(err, model.ErrUnauthorized):
		RespondError(c, http.StatusUnauthorized, CodeUnauthorized, err.Error(), nil)

	case errors.Is(err, model.ErrDeleteConfirmationMismatch):
		RespondError(c, http.StatusBadRequest, CodeDeleteConfirmMismatch, err.Error(), FieldErr("confirm_name", "confirmation_mismatch"))
	case errors.Is(err, model.ErrForbiddenNotOwner):
		RespondError(c, http.StatusForbidden, CodeNotServerOwner, err.Error(), nil)
	case errors.Is(err, model.ErrForbiddenNotMember):
		RespondError(c, http.StatusForbidden, CodeNotServerMember, err.Error(), nil)
	case errors.Is(err, model.ErrForbiddenNotAdmin):
		RespondError(c, http.StatusForbidden, CodeNotServerAdmin, err.Error(), nil)
	case errors.Is(err, model.ErrCannotRemoveOwnAdmin):
		RespondError(c, http.StatusForbidden, CodeCannotRemoveOwnAdmin, err.Error(), map[string]any{"reason": "cannot_remove_own_admin"})
	case errors.Is(err, model.ErrLeaveForbidden):
		RespondError(c, http.StatusForbidden, CodeLeaveForbidden, err.Error(), nil)
	case errors.Is(err, model.ErrOwnerCannotLeave):
		RespondError(c, http.StatusConflict, CodeOwnerMustTransferOrDel, err.Error(), map[string]any{"reason": "owner_must_transfer_or_delete"})
	case errors.Is(err, model.ErrServerNotFound):
		RespondError(c, http.StatusNotFound, CodeServerNotFound, err.Error(), nil)

	case errors.Is(err, model.ErrTransferTargetRequired):
		RespondError(c, http.StatusBadRequest, CodeTransferTargetRequired, err.Error(), FieldErr("to_user_id", "required"))
	case errors.Is(err, model.ErrTransferTargetNotMember):
		RespondError(c, http.StatusBadRequest, CodeTransferTargetNotMember, err.Error(), FieldErr("to_user_id", "not_a_member"))
	case errors.Is(err, model.ErrTransferTargetIsOwner):
		RespondError(c, http.StatusBadRequest, CodeTransferTargetIsOwner, err.Error(), FieldErr("to_user_id", "already_owner"))
	case errors.Is(err, model.ErrOwnershipTransferNotFound):
		RespondError(c, http.StatusNotFound, CodeTransferNotFound, err.Error(), nil)
	case errors.Is(err, model.ErrOwnershipTransferServerMismatch):
		RespondError(c, http.StatusBadRequest, CodeTransferServerMismatch, err.Error(), nil)
	case errors.Is(err, model.ErrOwnershipTransferWrongTarget):
		RespondError(c, http.StatusForbidden, CodeTransferWrongTarget, err.Error(), nil)
	case errors.Is(err, model.ErrOwnershipTransferNotInitiator):
		RespondError(c, http.StatusForbidden, CodeTransferNotInitiator, err.Error(), nil)
	case errors.Is(err, model.ErrOwnershipTransferNotPending):
		RespondError(c, http.StatusConflict, CodeTransferNotPending, err.Error(), map[string]any{"reason": "transfer_not_pending"})
	case errors.Is(err, model.ErrOwnershipTransferAlreadyPending):
		RespondError(c, http.StatusConflict, CodeTransferAlreadyPending, err.Error(), map[string]any{"reason": "transfer_already_pending"})
	case errors.Is(err, model.ErrRoleNotFound):
		RespondError(c, http.StatusNotFound, CodeRoleNotFound, err.Error(), nil)
	case errors.Is(err, model.ErrRoleNameRequired):
		RespondError(c, http.StatusBadRequest, CodeRoleNameRequired, err.Error(), FieldErr("name", "name_required"))
	case errors.Is(err, model.ErrRoleNameTooLong):
		RespondError(c, http.StatusBadRequest, CodeRoleNameTooLong, err.Error(), FieldErr("name", "name_too_long"))
	case errors.Is(err, model.ErrColorInvalid):
		RespondError(c, http.StatusBadRequest, CodeColorInvalidFormat, err.Error(), FieldErr("color", "color_invalid_format"))
	case errors.Is(err, model.ErrInvalidPermission):
		details := map[string]any{"field": "permissions"}
		var ipe *model.InvalidPermissionError
		if errors.As(err, &ipe) {
			details["invalid_value"] = ipe.Value
		}
		RespondError(c, http.StatusBadRequest, CodeInvalidPermission, err.Error(), details)
	case errors.Is(err, model.ErrRoleServerMismatch):
		RespondError(c, http.StatusBadRequest, CodeRoleServerMismatch, err.Error(), nil)
	case errors.Is(err, model.ErrRoleReorderInvalid):
		RespondError(c, http.StatusBadRequest, CodeRoleReorderInvalid, err.Error(), FieldErr("role_ids", "reorder_invalid"))
	case errors.Is(err, model.ErrRoleReorderAboveOwn):
		RespondError(c, http.StatusForbidden, CodeReorderAboveOwn, err.Error(), map[string]any{"reason": "reorder_above_own"})
	case errors.Is(err, model.ErrRoleAboveOwn):
		RespondError(c, http.StatusForbidden, CodeRoleAboveOwn, err.Error(), map[string]any{"reason": "role_above_own"})
	case errors.Is(err, model.ErrPermissionNotHeld):
		details := map[string]any{"reason": "permission_not_held"}
		var pnh *model.PermissionNotHeldError
		if errors.As(err, &pnh) {
			details["permissions"] = pnh.Permissions
		}
		RespondError(c, http.StatusForbidden, CodePermissionNotHeld, err.Error(), details)
	case errors.Is(err, model.ErrPermissionNotOverridable):
		details := map[string]any{"field": "permissions", "reason": "permission_not_overridable"}
		var e *model.PermissionNotOverridableError
		if errors.As(err, &e) {
			details["permissions"] = e.Permissions
		}
		RespondError(c, http.StatusBadRequest, CodePermissionNotOverridable, err.Error(), details)
	case errors.Is(err, model.ErrPermissionConflict):
		details := map[string]any{"field": "permissions", "reason": "permission_conflict"}
		var e *model.PermissionConflictError
		if errors.As(err, &e) {
			details["permissions"] = e.Permissions
		}
		RespondError(c, http.StatusBadRequest, CodePermissionConflict, err.Error(), details)
	case errors.Is(err, model.ErrChannelOverrideNotFound):
		RespondError(c, http.StatusNotFound, CodeChannelOverrideNotFound, err.Error(), map[string]any{"reason": "channel_override_not_found"})
	case errors.Is(err, model.ErrForbiddenMissingPermission):
		RespondError(c, http.StatusForbidden, CodeMissingPermission, err.Error(), map[string]any{"reason": "missing_permission"})
	case errors.Is(err, model.ErrBanTargetRequired):
		RespondError(c, http.StatusBadRequest, CodeBanTargetRequired, err.Error(), FieldErr("user_id", "required"))
	case errors.Is(err, model.ErrCannotBanSelf):
		RespondError(c, http.StatusBadRequest, CodeCannotBanSelf, err.Error(), FieldErr("user_id", "cannot_ban_self"))
	case errors.Is(err, model.ErrCannotBanOwner):
		RespondError(c, http.StatusForbidden, CodeCannotBanOwner, err.Error(), map[string]any{"reason": "cannot_ban_owner"})
	case errors.Is(err, model.ErrCannotKickSelf):
		RespondError(c, http.StatusBadRequest, CodeCannotKickSelf, err.Error(), FieldErr("user_id", "cannot_kick_self"))
	case errors.Is(err, model.ErrCannotKickOwner):
		RespondError(c, http.StatusForbidden, CodeCannotKickOwner, err.Error(), map[string]any{"reason": "cannot_kick_owner"})
	case errors.Is(err, model.ErrTargetOutranksActor):
		RespondError(c, http.StatusForbidden, CodeTargetOutranks, err.Error(), map[string]any{"reason": "target_outranks_actor"})
	case errors.Is(err, model.ErrAlreadyBanned):
		RespondError(c, http.StatusConflict, CodeAlreadyBanned, err.Error(), map[string]any{"reason": "already_banned"})
	case errors.Is(err, model.ErrBanNotFound):
		RespondError(c, http.StatusNotFound, CodeBanNotFound, err.Error(), map[string]any{"reason": "ban_not_found"})
	case errors.Is(err, model.ErrAlreadyMuted):
		RespondError(c, http.StatusConflict, CodeAlreadyMuted, err.Error(), map[string]any{"reason": "already_muted"})
	case errors.Is(err, model.ErrMuteNotFound):
		RespondError(c, http.StatusNotFound, CodeMuteNotFound, err.Error(), map[string]any{"reason": "mute_not_found"})
	case errors.Is(err, model.ErrMuteTargetRequired):
		RespondError(c, http.StatusBadRequest, CodeMuteTargetReq, err.Error(), FieldErr("user_id", "required"))
	case errors.Is(err, model.ErrCannotMuteSelf):
		RespondError(c, http.StatusBadRequest, CodeCannotMuteSelf, err.Error(), FieldErr("user_id", "cannot_mute_self"))
	case errors.Is(err, model.ErrCannotMuteOwner):
		RespondError(c, http.StatusForbidden, CodeCannotMuteOwner, err.Error(), map[string]any{"reason": "cannot_mute_owner"})
	case errors.Is(err, model.ErrMuteUntilInvalid):
		RespondError(c, http.StatusBadRequest, CodeMuteUntilInvalid, err.Error(), FieldErr("until_at", "mute_until_invalid"))
	case errors.Is(err, model.ErrReportKindInvalid):
		RespondError(c, http.StatusBadRequest, CodeReportKindInvalid, err.Error(), FieldErr("kind", "kind_invalid"))
	case errors.Is(err, model.ErrReportReasonRequired):
		RespondError(c, http.StatusBadRequest, CodeReportReasonRequired, err.Error(), FieldErr("reason", "reason_required"))
	case errors.Is(err, model.ErrReportChannelRequired):
		RespondError(c, http.StatusBadRequest, CodeReportChannelRequired, err.Error(), FieldErr("channel_id", "required"))
	case errors.Is(err, model.ErrReportMessageRequired):
		RespondError(c, http.StatusBadRequest, CodeReportMessageRequired, err.Error(), FieldErr("message_id", "required"))
	case errors.Is(err, model.ErrReportUserRequired):
		RespondError(c, http.StatusBadRequest, CodeReportUserRequired, err.Error(), FieldErr("user_id", "required"))
	case errors.Is(err, model.ErrReportTextChannel):
		RespondError(c, http.StatusBadRequest, CodeReportTextChannelReq, err.Error(), FieldErr("channel_id", "text_channel_required"))
	case errors.Is(err, model.ErrCannotReportSelf):
		RespondError(c, http.StatusBadRequest, CodeCannotReportSelf, err.Error(), FieldErr("user_id", "cannot_report_self"))
	case errors.Is(err, model.ErrReportAlreadyPending):
		RespondError(c, http.StatusConflict, CodeReportAlreadyPending, err.Error(), map[string]any{"reason": "report_already_pending"})
	case errors.Is(err, model.ErrReportNotFound):
		RespondError(c, http.StatusNotFound, CodeReportNotFound, err.Error(), map[string]any{"reason": "report_not_found"})
	case errors.Is(err, model.ErrReportNotPending):
		RespondError(c, http.StatusConflict, CodeReportNotPending, err.Error(), map[string]any{"reason": "report_not_pending"})
	case errors.Is(err, model.ErrReportActionInvalid):
		RespondError(c, http.StatusBadRequest, CodeReportActionInvalid, err.Error(), FieldErr("action", "action_invalid"))
	case errors.Is(err, model.ErrReportActionKind):
		RespondError(c, http.StatusBadRequest, CodeReportActionNotForKnd, err.Error(), FieldErr("action", "action_not_for_kind"))
	case errors.Is(err, model.ErrReportStatusInvalid):
		RespondError(c, http.StatusBadRequest, CodeReportStatusInvalid, err.Error(), FieldErr("status", "status_invalid"))
	case errors.As(err, &levelRequired):
		RespondError(c, http.StatusForbidden, CodeBoostLevelRequired, err.Error(), map[string]any{"reason": "boost_level_required", "required_level": levelRequired.Required})
	case errors.Is(err, model.ErrVanityCodeInvalid):
		RespondError(c, http.StatusBadRequest, CodeVanityCodeInvalid, err.Error(), FieldErr("code", "vanity_code_invalid"))
	case errors.Is(err, model.ErrVanityCodeTaken):
		RespondError(c, http.StatusConflict, CodeVanityCodeTaken, err.Error(), FieldErr("code", "vanity_code_taken"))
	case errors.Is(err, model.ErrEmojiNameInvalid):
		RespondError(c, http.StatusBadRequest, CodeEmojiNameInvalid, err.Error(), FieldErr("name", "emoji_name_invalid"))
	case errors.Is(err, model.ErrEmojiNameTaken):
		RespondError(c, http.StatusConflict, CodeEmojiNameTaken, err.Error(), FieldErr("name", "emoji_name_taken"))
	case errors.Is(err, model.ErrEmojiLimitReached):
		RespondError(c, http.StatusConflict, CodeEmojiLimitReached, err.Error(), map[string]any{"reason": "emoji_limit_reached"})
	case errors.Is(err, model.ErrEmojiNotFound):
		RespondError(c, http.StatusNotFound, CodeEmojiNotFound, err.Error(), map[string]any{"reason": "emoji_not_found"})
	case errors.Is(err, model.ErrEmojiImageRequired):
		RespondError(c, http.StatusBadRequest, CodeEmojiImageRequired, err.Error(), FieldErr("image", "emoji_image_required"))
	case errors.Is(err, model.ErrEmojiImageTooLarge):
		RespondError(c, http.StatusBadRequest, CodeEmojiImageTooLarge, err.Error(), FieldErr("image", "emoji_image_too_large"))
	case errors.Is(err, model.ErrEmojiImageUnsupported):
		RespondError(c, http.StatusBadRequest, CodeEmojiImageUnsupported, err.Error(), FieldErr("image", "emoji_image_unsupported"))
	case errors.Is(err, moderation.ErrBannedWordEmpty):
		RespondError(c, http.StatusBadRequest, CodeWordEmpty, err.Error(), FieldErr("words", "word_empty"))
	case errors.Is(err, moderation.ErrBannedWordTooLong):
		RespondError(c, http.StatusBadRequest, CodeWordTooLong, err.Error(), FieldErr("words", "word_too_long"))
	case errors.Is(err, moderation.ErrBannedWordInvalidChars):
		RespondError(c, http.StatusBadRequest, CodeWordInvalidChars, err.Error(), FieldErr("words", "word_invalid_chars"))
	case errors.Is(err, moderation.ErrTooManyBannedWords):
		RespondError(c, http.StatusBadRequest, CodeTooManyWords, err.Error(), FieldErr("words", "too_many_words"))
	case errors.Is(err, moderation.ErrReasonTooLong):
		RespondError(c, http.StatusBadRequest, CodeReasonTooLong, err.Error(), FieldErr("reason", "reason_too_long"))
	case errors.Is(err, moderation.ErrReasonInvalidChars):
		RespondError(c, http.StatusBadRequest, CodeReasonInvalidChar, err.Error(), FieldErr("reason", "reason_invalid_chars"))
	case errors.Is(err, mention.ErrTooManyUsers):
		RespondError(c, http.StatusBadRequest, CodeMentionTooManyUsers, err.Error(), FieldErr("user_ids", "too_many"))
	case errors.Is(err, mention.ErrTooManyRoles):
		RespondError(c, http.StatusBadRequest, CodeMentionTooManyRoles, err.Error(), FieldErr("role_ids", "too_many"))
	case errors.Is(err, model.ErrRoleIsServerDefault):
		RespondError(c, http.StatusConflict, CodeRoleIsServerDefault, err.Error(), nil)
	case errors.Is(err, model.ErrEveryoneRoleIsAutomatic):
		RespondError(c, http.StatusConflict, CodeEveryoneRoleIsAutomatic, err.Error(), map[string]any{"reason": "everyone_role_is_automatic"})
	case errors.Is(err, model.ErrMemberNotFound):
		RespondError(c, http.StatusNotFound, CodeMemberNotFound, err.Error(), nil)
	case errors.Is(err, model.ErrMemberRoleNotFound):
		RespondError(c, http.StatusNotFound, CodeMemberRoleNotFound, err.Error(), nil)

	case errors.Is(err, model.ErrChannelNotFound):
		RespondError(c, http.StatusNotFound, CodeChannelNotFound, err.Error(), nil)
	case errors.Is(err, model.ErrChannelNameRequired):
		RespondError(c, http.StatusBadRequest, CodeChannelNameRequired, err.Error(), FieldErr("name", "name_required"))
	case errors.Is(err, model.ErrChannelNameTooLong):
		RespondError(c, http.StatusBadRequest, CodeChannelNameTooLong, err.Error(), FieldErr("name", "name_too_long"))
	case errors.Is(err, model.ErrChannelNameInvalidChars):
		RespondError(c, http.StatusBadRequest, CodeChannelNameInvalidChars, err.Error(), FieldErr("name", "name_invalid_chars"))
	case errors.Is(err, model.ErrChannelNameTaken):
		RespondError(c, http.StatusBadRequest, CodeChannelNameTaken, err.Error(), FieldErr("name", "name_taken"))
	case errors.Is(err, model.ErrChannelKindInvalid):
		RespondError(c, http.StatusBadRequest, CodeChannelKindInvalid, err.Error(), FieldErr("kind", "kind_invalid"))
	case errors.Is(err, model.ErrVoiceChannelRequired):
		RespondError(c, http.StatusForbidden, CodeVoiceChannelRequired, err.Error(), map[string]any{"reason": "voice_channel_required"})
	case errors.Is(err, model.ErrVoiceConnectForbidden):
		RespondError(c, http.StatusForbidden, CodeVoiceConnectForbidden, err.Error(), map[string]any{"reason": "missing_permission", "permission": "CONNECT"})
	case errors.Is(err, model.ErrChannelServerMismatch):
		RespondError(c, http.StatusBadRequest, CodeChannelServerMismatch, err.Error(), nil)
	case errors.Is(err, model.ErrChannelReorderInvalid):
		RespondError(c, http.StatusBadRequest, CodeChannelReorderInvalid, err.Error(), FieldErr("channel_ids", "reorder_invalid"))

	case errors.Is(err, model.ErrCategoryNotFound):
		RespondError(c, http.StatusNotFound, CodeCategoryNotFound, err.Error(), nil)
	case errors.Is(err, model.ErrCategoryNameRequired):
		RespondError(c, http.StatusBadRequest, CodeCategoryNameRequired, err.Error(), FieldErr("name", "name_required"))
	case errors.Is(err, model.ErrCategoryNameTooLong):
		RespondError(c, http.StatusBadRequest, CodeCategoryNameTooLong, err.Error(), FieldErr("name", "name_too_long"))
	case errors.Is(err, model.ErrCategoryNameInvalidChars):
		RespondError(c, http.StatusBadRequest, CodeCategoryNameInvalidChars, err.Error(), FieldErr("name", "name_invalid_chars"))
	case errors.Is(err, model.ErrCategoryNameTaken):
		RespondError(c, http.StatusBadRequest, CodeCategoryNameTaken, err.Error(), FieldErr("name", "name_taken"))
	case errors.Is(err, model.ErrCategoryServerMismatch):
		RespondError(c, http.StatusBadRequest, CodeCategoryServerMismatch, err.Error(), FieldErr("category_id", "category_server_mismatch"))
	case errors.Is(err, model.ErrCategoryReorderInvalid):
		RespondError(c, http.StatusBadRequest, CodeCategoryReorderInvalid, err.Error(), FieldErr("category_ids", "reorder_invalid"))

	case errors.Is(err, model.ErrInviteServerNotFound):
		RespondError(c, http.StatusNotFound, CodeServerNotFound, err.Error(), FieldErr("server_id", "server_not_found"))
	case errors.Is(err, model.ErrInvitePermissionDenied):
		RespondError(c, http.StatusForbidden, CodeInvitePermissionDenied, err.Error(), map[string]any{"reason": "invite_permission_denied"})
	case errors.Is(err, model.ErrInviteMaxUsesInvalid):
		RespondError(c, http.StatusBadRequest, CodeMaxUsesInvalid, err.Error(), FieldErr("max_uses", "max_uses_invalid"))
	case errors.Is(err, model.ErrInvitationNotFound),
		errors.Is(err, model.ErrInvitationRevoked),
		errors.Is(err, model.ErrInvitationExpired),
		errors.Is(err, model.ErrInvitationExhausted):
		RespondError(c, http.StatusNotFound, CodeInvitationInvalid, err.Error(), map[string]any{"reason": "invitation_invalid"})
	case errors.Is(err, model.ErrUserBanned):
		RespondError(c, http.StatusForbidden, CodeUserBanned, err.Error(), map[string]any{"reason": "user_banned"})

	default:
		slog.Error(fmt.Sprintf("unmapped internal error: %v", err))
		RespondError(c, http.StatusInternalServerError, CodeInternal, "internal service error", nil)
	}
}
