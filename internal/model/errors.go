package model

import (
	"errors"
	"strings"
)

var (
	ErrNameRequired     = errors.New("name must be provided")
	ErrNameTooShort     = errors.New("name is too short")
	ErrNameTooLong      = errors.New("name is too long")
	ErrNameInvalidChars = errors.New("name contains control characters")
	ErrNameTaken        = errors.New("you already have a server with that name")

	ErrIconTooLarge        = errors.New("icon file is too large")
	ErrIconUnsupportedType = errors.New("icon file type is not supported")
	ErrIconNotFound        = errors.New("icon not found")

	ErrUnauthorized = errors.New("authentication required")

	ErrServerNotFound     = errors.New("server not found")
	ErrForbiddenNotOwner  = errors.New("only the server owner can perform this action")
	ErrForbiddenNotMember = errors.New("you must be a member of this server to view this")
	ErrForbiddenNotAdmin  = errors.New("you must be a server administrator to perform this action")

	ErrDeleteConfirmationMismatch = errors.New("the confirmation does not match the server name")

	ErrCannotRemoveOwnAdmin = errors.New("you cannot remove the administrator permission from yourself")

	ErrLeaveForbidden   = errors.New("you can only remove your own membership")
	ErrOwnerCannotLeave = errors.New("the server owner cannot leave; transfer ownership or delete the server first")

	ErrTransferTargetRequired          = errors.New("to_user_id must be provided")
	ErrTransferTargetNotMember         = errors.New("the target user is not a member of this server")
	ErrTransferTargetIsOwner           = errors.New("the target user is already the owner")
	ErrOwnershipTransferNotFound       = errors.New("ownership transfer not found")
	ErrOwnershipTransferServerMismatch = errors.New("ownership transfer does not belong to this server")
	ErrOwnershipTransferWrongTarget    = errors.New("only the recipient of the transfer can respond to it")
	ErrOwnershipTransferNotInitiator   = errors.New("only the owner who initiated the transfer can cancel it")
	ErrOwnershipTransferNotPending     = errors.New("ownership transfer is no longer pending")
	ErrOwnershipTransferAlreadyPending = errors.New("this server already has a pending ownership transfer")

	ErrRoleNotFound        = errors.New("role not found")
	ErrRoleNameRequired    = errors.New("role name must be provided")
	ErrRoleNameTooLong     = errors.New("role name is too long")
	ErrColorInvalid        = errors.New("color must be a hex value like #RRGGBB")
	ErrInvalidPermission   = errors.New("unknown permission")
	ErrRoleServerMismatch  = errors.New("role does not belong to this server")
	ErrRoleIsServerDefault = errors.New("role is the server's default role; set a different default role before deleting it")
	ErrRoleReorderInvalid  = errors.New("the role list must match exactly the roles currently in this server")
	ErrRoleReorderAboveOwn = errors.New("you can only reorder roles below your highest role")
	ErrRoleAboveOwn        = errors.New("you can only manage roles below your highest role")
	ErrPermissionNotHeld   = errors.New("you cannot grant permissions you do not have")

	ErrPermissionNotOverridable = errors.New("permission cannot be overridden per channel")
	ErrPermissionConflict       = errors.New("a permission cannot be both allowed and denied")
	ErrChannelOverrideNotFound  = errors.New("this role has no permission override in this channel")

	ErrEveryoneRoleIsAutomatic = errors.New("@everyone is managed automatically: it can't be deleted, renamed, recolored, or assigned/unassigned by hand")

	ErrForbiddenMissingPermission = errors.New("you do not have permission to perform this action")
	ErrBanTargetRequired          = errors.New("user_id must be provided")
	ErrCannotBanSelf              = errors.New("you cannot ban yourself")
	ErrCannotBanOwner             = errors.New("the server owner cannot be banned")
	ErrTargetOutranksActor        = errors.New("target has an equal or higher role")
	ErrAlreadyBanned              = errors.New("user is already banned from this server")
	ErrBanNotFound                = errors.New("user is not banned from this server")
	ErrAlreadyMuted               = errors.New("user is already muted in this server")
	ErrMuteNotFound               = errors.New("user is not muted in this server")
	ErrMuteUntilInvalid           = errors.New("mute expiration must be after the current time")
	ErrMuteTargetRequired         = errors.New("user_id must be provided to mute a member")
	ErrUnmuteTargetRequired       = errors.New("user_id must be provided to unmute a member")
	ErrCannotMuteSelf             = errors.New("you cannot mute yourself")
	ErrCannotMuteOwner            = errors.New("the server owner cannot be muted")
	ErrCannotKickSelf             = errors.New("you cannot kick yourself; leave the server instead")
	ErrCannotKickOwner            = errors.New("the server owner cannot be kicked")

	ErrReportKindInvalid     = errors.New("kind must be 'message' or 'user'")
	ErrReportReasonRequired  = errors.New("reason must be provided")
	ErrReportChannelRequired = errors.New("channel_id must be provided to report a message")
	ErrReportMessageRequired = errors.New("message_id must be provided to report a message")
	ErrReportUserRequired    = errors.New("user_id must be provided to report a user")
	ErrReportTextChannel     = errors.New("only messages of a text channel can be reported")
	ErrCannotReportSelf      = errors.New("you cannot report yourself")
	ErrReportAlreadyPending  = errors.New("you already have a pending report about this")
	ErrReportNotFound        = errors.New("report not found")
	ErrReportNotPending      = errors.New("report was already closed")
	ErrReportStatusInvalid   = errors.New("status must be 'resolved' or 'dismissed'")

	ErrEmojiNameInvalid      = errors.New("emoji name must be 2 to 32 letters, numbers or underscores")
	ErrEmojiNameTaken        = errors.New("this server already has an emoji with that name")
	ErrEmojiLimitReached     = errors.New("this server has no emoji slots left")
	ErrEmojiNotFound         = errors.New("emoji not found")
	ErrEmojiImageRequired    = errors.New("emoji image must be provided")
	ErrEmojiImageTooLarge    = errors.New("emoji image is too large (max 5 MB)")
	ErrEmojiImageUnsupported = errors.New("emoji image must be PNG, WebP or GIF")

	ErrMemberNotFound     = errors.New("member not found")
	ErrMemberRoleNotFound = errors.New("member does not have this role")

	ErrInviteServerNotFound   = errors.New("server not found")
	ErrInvitePermissionDenied = errors.New("you do not have permission to invite to this server")
	ErrInviteMaxUsesInvalid   = errors.New("max uses must be a positive number")
	ErrInviteCodeUnavailable  = errors.New("could not allocate a unique invitation code")

	ErrInvitationNotFound  = errors.New("invitation is not valid")
	ErrInvitationRevoked   = errors.New("invitation is not valid: it has been revoked")
	ErrInvitationExpired   = errors.New("invitation is not valid: it has expired")
	ErrInvitationExhausted = errors.New("invitation is not valid: it reached its use limit")

	ErrAlreadyMember = errors.New("already a member of this server")
	ErrUserBanned    = errors.New("you are banned from this server")

	ErrChannelNotFound         = errors.New("channel not found")
	ErrChannelNameRequired     = errors.New("channel name must be provided")
	ErrChannelNameTooLong      = errors.New("channel name is too long")
	ErrChannelNameInvalidChars = errors.New("channel name contains control characters")
	ErrChannelNameTaken        = errors.New("a channel with that name already exists in this category")
	ErrChannelKindInvalid      = errors.New("channel kind must be 'text' or 'voice'")
	ErrChannelServerMismatch   = errors.New("channel does not belong to this server")
	ErrVoiceChannelRequired    = errors.New("voice channel access requires a voice channel")
	ErrVoiceConnectForbidden   = errors.New("you do not have permission to connect to this voice channel")

	ErrCategoryNotFound         = errors.New("category not found")
	ErrCategoryNameRequired     = errors.New("category name must be provided")
	ErrCategoryNameTooLong      = errors.New("category name is too long")
	ErrCategoryNameInvalidChars = errors.New("category name contains control characters")
	ErrCategoryNameTaken        = errors.New("a category with that name already exists in this server")
	ErrCategoryServerMismatch   = errors.New("category does not belong to this server")

	ErrChannelReorderInvalid  = errors.New("the channel list must match exactly the channels currently in that category")
	ErrCategoryReorderInvalid = errors.New("the category list must match exactly the categories currently in this server")
)

// InvalidPermissionError wraps ErrInvalidPermission with the offending
// permission name, so handlers can report it in the error details.
type InvalidPermissionError struct {
	Value string
}

func (e *InvalidPermissionError) Error() string {
	return "unknown permission: " + e.Value
}

func (e *InvalidPermissionError) Is(target error) bool {
	return target == ErrInvalidPermission
}

// PermissionNotHeldError wraps ErrPermissionNotHeld with the names of exactly
// the permissions the actor tried to grant but does not hold
type PermissionNotHeldError struct {
	Permissions []string
}

func (e *PermissionNotHeldError) Error() string {
	return "cannot grant permissions you do not have: " + strings.Join(e.Permissions, ", ")
}

func (e *PermissionNotHeldError) Is(target error) bool {
	return target == ErrPermissionNotHeld
}

// PermissionNotOverridableError wraps ErrPermissionNotOverridable with the
// names of the permissions that only exist at the server level
type PermissionNotOverridableError struct {
	Permissions []string
}

func (e *PermissionNotOverridableError) Error() string {
	return "permissions cannot be overridden per channel: " + strings.Join(e.Permissions, ", ")
}

func (e *PermissionNotOverridableError) Is(target error) bool {
	return target == ErrPermissionNotOverridable
}

// PermissionConflictError wraps ErrPermissionConflict with the names of the
// permissions that were both allowed and denied
type PermissionConflictError struct {
	Permissions []string
}

func (e *PermissionConflictError) Error() string {
	return "permissions both allowed and denied: " + strings.Join(e.Permissions, ", ")
}

func (e *PermissionConflictError) Is(target error) bool {
	return target == ErrPermissionConflict
}
