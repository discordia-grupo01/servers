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
)
