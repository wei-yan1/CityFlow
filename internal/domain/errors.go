package domain

import "errors"

var (
	ErrRateLimited    = errors.New("location update too frequent")
	ErrUnavailable    = errors.New("location service unavailable")
	ErrNotFound       = errors.New("resource not found")
	ErrUnauthorized   = errors.New("invalid email or password")
	ErrForbidden      = errors.New("insufficient role")
	ErrConflict       = errors.New("resource state conflict")
	ErrInvalidRequest = errors.New("invalid request")
)
