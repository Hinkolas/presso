package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

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

// privatePeer reports whether addr (ip or ip:port) is loopback, link-local
// or RFC 1918 / ULA: the edge on the Docker bridge, an SSH tunnel, a LAN.
func privatePeer(addr string) bool {
	host := addr
	if h, _, err := net.SplitHostPort(addr); err == nil {
		host = h
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified()
}

// panelGuard, active when PANEL_DOMAIN is set, refuses the panel and API to
// public peers: the panel is served by the edge over TLS on that domain, and
// the engine port stays reachable only from the host, the Docker network and
// private networks. Health and the system-plugin callbacks stay open.
func panelGuard(domain string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if domain == "" {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p := r.URL.Path
			if p == "/healthz" || strings.HasPrefix(p, "/v1/internal/") || privatePeer(peerAddr(r)) {
				next.ServeHTTP(w, r)
				return
			}
			writeError(w, http.StatusForbidden, codePanelViaEdge, "the presso panel is served at https://"+domain)
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

func writeUpdateError(ctx context.Context, w http.ResponseWriter, err error) {
	switch {
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
