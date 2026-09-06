package auth

import "errors"

// Sentinel errors returned by the Service. The HTTP layer maps these onto the
// error envelope; anything not listed here is treated as an internal error.
var (
	ErrInvalidCredentials = errors.New("auth: invalid email or password")
	ErrInvalidToken       = errors.New("auth: invalid or expired token")
	ErrRateLimited        = errors.New("auth: too many attempts")
	ErrNotFound           = errors.New("auth: not found")
	ErrEmailTaken         = errors.New("auth: email already taken")
	ErrInvalidEmail       = errors.New("auth: invalid email address")
	ErrInvalidRole        = errors.New("auth: role must be admin or member")
	ErrWeakPassword       = errors.New("auth: password must be at least 12 characters")
	ErrLastAdmin          = errors.New("auth: cannot demote or delete the last admin")
)
