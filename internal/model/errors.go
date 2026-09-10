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

	ErrUnauthorized = errors.New("authentication required")

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
)
