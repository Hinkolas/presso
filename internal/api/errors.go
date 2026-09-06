package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/Hinkolas/presso/internal/auth"
)

// errorBody is the error envelope every non-2xx response uses:
// {"error":{"code":"…","message":"…"}}. Clients branch on code, never on
// message.
type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Stable error codes, grouped by domain. Add new codes here, never inline.
const (
	codeBadRequest         = "bad_request"
	codeInvalidCredentials = "invalid_credentials"
	codeInvalidToken       = "invalid_token"
	codeForbidden          = "forbidden"
	codeNotFound           = "not_found"
	codeConflict           = "conflict"
	codeRateLimited        = "rate_limited"
	codeInternal           = "internal"
)

// writeInternalError logs the real error and reports an opaque 500.
func writeInternalError(ctx context.Context, w http.ResponseWriter, what string, err error) {
	slog.ErrorContext(ctx, "api: "+what, "err", err)
	writeError(w, http.StatusInternalServerError, codeInternal, "internal error")
}

// writeAuthError maps auth sentinel errors onto the envelope; anything
// unrecognized is logged and reported as an opaque 500.
func writeAuthError(ctx context.Context, w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		writeError(w, http.StatusUnauthorized, codeInvalidCredentials, "invalid email or password")
	case errors.Is(err, auth.ErrInvalidToken):
		writeError(w, http.StatusUnauthorized, codeInvalidToken, "invalid or expired token")
	case errors.Is(err, auth.ErrRateLimited):
		writeError(w, http.StatusTooManyRequests, codeRateLimited, "too many attempts, try again later")
	case errors.Is(err, auth.ErrNotFound):
		writeError(w, http.StatusNotFound, codeNotFound, "not found")
	case errors.Is(err, auth.ErrEmailTaken):
		writeError(w, http.StatusConflict, codeConflict, "a user with this email already exists")
	case errors.Is(err, auth.ErrLastAdmin):
		writeError(w, http.StatusConflict, codeConflict, "cannot demote or delete the last admin")
	case errors.Is(err, auth.ErrWeakPassword), errors.Is(err, auth.ErrInvalidEmail), errors.Is(err, auth.ErrInvalidRole):
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
	default:
		writeInternalError(ctx, w, "internal error", err)
	}
}
