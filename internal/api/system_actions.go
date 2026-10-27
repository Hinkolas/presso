package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/Hinkolas/presso/internal/control"
	"github.com/Hinkolas/presso/internal/jobs"
	"github.com/Hinkolas/presso/internal/update"
)

// --- panel guard ---

// peerKey carries the raw TCP peer of the connection. It is captured by
// PeerContext (http.Server.ConnContext) before any middleware runs, so the
// guard below cannot be fooled by X-Forwarded-For / X-Real-IP, which realIP
// folds into r.RemoteAddr afterwards.
type peerKey struct{}

// PeerContext is meant for http.Server.ConnContext.
func PeerContext(ctx context.Context, c net.Conn) context.Context {
	return context.WithValue(ctx, peerKey{}, c.RemoteAddr().String())
}

func peerAddr(r *http.Request) string {
	if v, ok := r.Context().Value(peerKey{}).(string); ok && v != "" {
		return v
	}
	return r.RemoteAddr
}

// InternalOnly wraps the TLS listener. Only callbacks may omit an edge certificate.
func InternalOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/api")
		if !control.IsEdge(r.TLS) && !(r.Method == http.MethodPost && strings.HasPrefix(p, "/v1/internal/sites/") && strings.HasSuffix(p, "/purge")) {
			writeError(w, http.StatusForbidden, codePanelViaEdge, "edge client certificate required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// panelGuard always requires loopback or an authenticated edge for panel access.
func panelGuard(_ string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host := peerAddr(r)
			if h, _, err := net.SplitHostPort(host); err == nil {
				host = h
			}
			ip := net.ParseIP(strings.Trim(host, "[]"))
			if (ip != nil && ip.IsLoopback()) || control.IsEdge(r.TLS) || r.URL.Path == "/healthz" || strings.HasPrefix(r.URL.Path, "/v1/internal/") {
				next.ServeHTTP(w, r)
				return
			}
			writeError(w, http.StatusForbidden, codePanelViaEdge, "use the local tunnel or configured HTTPS panel")
		})
	}
}

// --- system actions ---

// POST /v1/system/actions/{action}: check-update, update, rollout.
func (h *systemHandlers) action(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if h.update == nil {
		writeError(w, http.StatusNotFound, codeNotFound, "unknown action")
		return
	}
	by := actorFrom(ctx)
	switch chi.URLParam(r, "action") {
	case "check-update":
		st, err := h.update.Check(ctx, true)
		if err != nil && !errors.Is(err, update.ErrNoRelease) {
			// The status carries the error text; the client still gets the
			// (unchanged) state so the panel can render it.
			writeJSON(w, http.StatusOK, map[string]any{"update": st})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"update": st})
	case "update":
		job, err := h.update.StartUpdate(ctx, by)
		if err != nil {
			writeUpdateError(ctx, w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"job": jobs.JobPayload(job)})
	case "rollout":
		job, err := h.update.StartRollout(ctx, by)
		if err != nil {
			writeUpdateError(ctx, w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"job": jobs.JobPayload(job)})
	default:
		writeError(w, http.StatusNotFound, codeNotFound, "unknown action")
	}
}

// PUT /v1/system/update/settings: {"channel": "stable"|"beta",
// "auto_update": bool}; either field may be omitted. Answers with the update
// status, rechecked when the channel changed.
func (h *systemHandlers) updateSettings(w http.ResponseWriter, r *http.Request) {
	if h.update == nil {
		writeError(w, http.StatusNotFound, codeNotFound, "self-update is not wired on this engine")
		return
	}
	var in update.SettingsPatch
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	st, err := h.update.UpdateSettings(r.Context(), in)
	if err != nil {
		writeUpdateError(r.Context(), w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"update": st})
}

func writeUpdateError(ctx context.Context, w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, update.ErrInvalidSettings):
		writeError(w, http.StatusBadRequest, codeBadRequest, strings.TrimPrefix(err.Error(), update.ErrInvalidSettings.Error()+": "))
	case errors.Is(err, update.ErrUnavailable):
		writeError(w, http.StatusConflict, codeUpdateUnavailable, err.Error())
	case errors.Is(err, update.ErrCurrent):
		writeError(w, http.StatusConflict, codeUpdateCurrent, "presso is already on the latest release")
	case errors.Is(err, update.ErrBusy):
		writeError(w, http.StatusConflict, codeJobRunning, "an engine update or rollout is already in progress")
	case errors.Is(err, update.ErrNoRelease):
		writeError(w, http.StatusNotFound, codeNoRelease, "no release has been published yet")
	default:
		writeSiteError(ctx, w, err)
	}
}
