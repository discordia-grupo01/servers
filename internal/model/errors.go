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

	ErrServerNotFound    = errors.New("server not found")
	ErrForbiddenNotOwner = errors.New("only the server owner can perform this action")

	ErrRoleNotFound       = errors.New("role not found")
	ErrRoleNameRequired   = errors.New("role name must be provided")
	ErrRoleNameTooLong    = errors.New("role name is too long")
	ErrColorInvalid       = errors.New("color must be a hex value like #RRGGBB")
	ErrInvalidPermission  = errors.New("unknown permission")
	ErrRoleServerMismatch = errors.New("role does not belong to this server")

	ErrMemberNotFound     = errors.New("member not found")
	ErrMemberRoleNotFound = errors.New("member does not have this role")
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
