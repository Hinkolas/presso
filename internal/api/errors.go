package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/Hinkolas/presso/internal/auth"
	"github.com/Hinkolas/presso/internal/docker"
	"github.com/Hinkolas/presso/internal/edge"
	"github.com/Hinkolas/presso/internal/jobs"
	"github.com/Hinkolas/presso/internal/platform"
	"github.com/Hinkolas/presso/internal/shots"
	"github.com/Hinkolas/presso/internal/site"
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

	codeSlugTaken          = "slug_taken"
	codeDomainTaken        = "domain_taken"
	codeJobRunning         = "job_running"
	codeSiteNotReady       = "site_not_ready"
	codeDockerUnavailable  = "docker_unavailable"
	codeEdgeUnavailable    = "edge_unavailable"
	codeBrowserUnavailable = "browser_unavailable"
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

// writeSiteError maps site/jobs/docker sentinel errors onto the envelope.
func writeSiteError(ctx context.Context, w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, site.ErrNotFound), errors.Is(err, jobs.ErrNotFound):
		writeError(w, http.StatusNotFound, codeNotFound, "not found")
	case errors.Is(err, site.ErrSlugTaken):
		writeError(w, http.StatusConflict, codeSlugTaken, "a site with this slug already exists")
	case errors.Is(err, site.ErrDomainTaken):
		writeError(w, http.StatusConflict, codeDomainTaken, "this domain is already routed to another site")
	case errors.Is(err, site.ErrJobRunning):
		writeError(w, http.StatusConflict, codeJobRunning, "another operation is in progress for this site")
	case errors.Is(err, site.ErrNotReady):
		writeError(w, http.StatusConflict, codeSiteNotReady, err.Error())
	case errors.Is(err, site.ErrNotRetryable):
		writeError(w, http.StatusConflict, codeSiteNotReady, "nothing to retry for this site")
	case errors.Is(err, site.ErrWrongConfirm):
		writeError(w, http.StatusBadRequest, codeBadRequest, "confirmation does not match the site slug")
	case errors.Is(err, site.ErrInvalidToken):
		writeError(w, http.StatusUnauthorized, codeInvalidToken, "invalid purge token")
	case errors.Is(err, site.ErrInvalidInput), errors.Is(err, jobs.ErrUnknownKind):
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
	case errors.Is(err, jobs.ErrQueueFull):
		writeError(w, http.StatusServiceUnavailable, codeConflict, "the job queue is full, try again shortly")
	case errors.Is(err, platform.ErrNotReady):
		writeError(w, http.StatusServiceUnavailable, codeDockerUnavailable, "a platform service is not ready: "+err.Error())
	case errors.Is(err, edge.ErrUnavailable):
		writeError(w, http.StatusServiceUnavailable, codeEdgeUnavailable, "the edge proxy is not reachable")
	case errors.Is(err, shots.ErrUnavailable):
		writeError(w, http.StatusServiceUnavailable, codeBrowserUnavailable, "the preview browser is not reachable")
	case errors.Is(err, docker.ErrNotFound):
		writeError(w, http.StatusNotFound, codeNotFound, "docker object not found")
	case errors.Is(err, context.DeadlineExceeded):
		writeError(w, http.StatusGatewayTimeout, codeInternal, "operation timed out")
	default:
		writeInternalError(ctx, w, "site error", err)
	}
}
