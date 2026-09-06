// Package api is the HTTP layer of cmd/pressod: routing, middleware, request
// decoding, and the error envelope. Business logic stays in internal/auth
// (and later feature packages: sites, docker, backups); handlers only
// translate between HTTP and those services.
package api

import (
	"database/sql"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/Hinkolas/presso/internal/auth"
	"github.com/Hinkolas/presso/internal/store"
)

// Deps is everything the router needs. Each optional field documents what its
// zero value means.
type Deps struct {
	Auth  *auth.Service
	Store *store.Store
	// DB backs the health check; nil reports the database as unreachable.
	DB *sql.DB
	// Version is the engine build version reported on /v1/system/meta and
	// stamped onto every response as the Presso-Version header.
	Version string
}

// StripAPIPrefix serves the router both at the root and under /api, so the
// same handler works behind an edge that routes /api to the engine while the
// web UI owns /. Only a whole /api path segment is stripped; lookalike paths
// like /apifoo pass through untouched.
func StripAPIPrefix(next http.Handler) http.Handler {
	stripped := http.StripPrefix("/api", next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api":
			clone := r.Clone(r.Context())
			clone.URL.Path = "/"
			clone.URL.RawPath = ""
			next.ServeHTTP(w, clone)
		case strings.HasPrefix(r.URL.Path, "/api/"):
			stripped.ServeHTTP(w, r)
		default:
			next.ServeHTTP(w, r)
		}
	})
}

func NewRouter(d Deps) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	// realIP must precede everything that reads RemoteAddr (rate-limit keys,
	// session metadata).
	r.Use(realIP)
	r.Use(requestLogger)
	r.Use(middleware.Recoverer)
	r.Use(versionHeader(d.Version))
	// The request timeout is applied per group below, not globally, so that
	// future streaming routes (logs, events) can live outside it.

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if d.DB == nil || d.DB.PingContext(r.Context()) != nil {
			writeError(w, http.StatusServiceUnavailable, codeInternal, "database unreachable")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	})

	ah := &authHandlers{auth: d.Auth}
	sh := &systemHandlers{version: d.Version}
	r.Route("/v1", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(middleware.Timeout(30 * time.Second))

			// Public: everything a client can reach without a session.
			r.Post("/auth/login", ah.login)

			// Bearer-protected. RequireAuth stays on this group only.
			r.Group(func(r chi.Router) {
				r.Use(RequireAuth(d.Auth))

				r.Post("/auth/logout", ah.logout)
				r.Get("/auth/session", ah.currentSession)
				r.Get("/auth/sessions", ah.listSessions)
				r.Delete("/auth/sessions/{id}", ah.revokeSession)
				r.Post("/auth/password", ah.changePassword)

				r.Get("/system/meta", sh.meta)

				// Product surface (sites, settings, operations) is registered
				// here as it lands; management routes go under RequireAdmin.
			})
		})
	})

	return r
}
