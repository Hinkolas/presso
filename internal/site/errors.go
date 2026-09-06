package site

import "errors"

// Sentinel errors mapped onto the API error envelope by internal/api.
var (
	ErrNotFound      = errors.New("site: not found")
	ErrSlugTaken     = errors.New("site: slug already in use")
	ErrDomainTaken   = errors.New("site: domain already in use")
	ErrJobRunning    = errors.New("site: another operation is in progress")
	ErrNotReady      = errors.New("site: container is not running")
	ErrInvalidInput  = errors.New("site: invalid input")
	ErrWrongConfirm  = errors.New("site: confirmation does not match the slug")
	ErrInvalidToken  = errors.New("site: invalid purge token")
	ErrNotRetryable  = errors.New("site: nothing to retry")
	ErrDockerFailure = errors.New("site: docker unavailable")
)
