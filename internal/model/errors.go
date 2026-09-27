package model

import "errors"

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

	ErrCategoryNotFound         = errors.New("category not found")
	ErrCategoryNameRequired     = errors.New("category name must be provided")
	ErrCategoryNameTooLong      = errors.New("category name is too long")
	ErrCategoryNameInvalidChars = errors.New("category name contains control characters")
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
