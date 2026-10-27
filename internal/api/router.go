// Package api is the HTTP layer of cmd/pressod: routing, middleware, request
// decoding, and the error envelope. Business logic stays in the service
// packages (internal/auth, internal/site, internal/jobs, internal/platform);
// handlers only translate between HTTP and those services. The embedded web
// UI is mounted at / so one binary serves both.
package api

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/Hinkolas/presso/internal/auth"
	"github.com/Hinkolas/presso/internal/compat"
	"github.com/Hinkolas/presso/internal/config"
	"github.com/Hinkolas/presso/internal/docker"
	"github.com/Hinkolas/presso/internal/events"
	"github.com/Hinkolas/presso/internal/platform"
	"github.com/Hinkolas/presso/internal/site"
	"github.com/Hinkolas/presso/internal/store"
	"github.com/Hinkolas/presso/internal/update"
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
	// Config is the engine configuration (nil is tolerated by the handlers
	// that read it). Catalog feeds /v1/system/runtime; nil reports an empty
	// offer.
	Config  *config.API
	Catalog *compat.Catalog
	// Sites, Hub, Docker and Platform back the product surface; nil disables
	// the corresponding routes (auth-only deployments and tests).
	Sites    *site.Service
	Hub      *events.Hub
	Docker   docker.Client
	Platform *platform.Service
	// Update backs the self-update status and actions; nil hides them.
	Update *update.Service
	// WebUI, when set, serves every path the API does not claim.
	WebUI http.Handler
	// Shutdown, when set, ends long-lived streams (events, logs) as soon as
	// the server starts shutting down, so graceful shutdown does not wait on
	// browsers that would otherwise hold their connections open.
	Shutdown context.Context
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
	// The panel guard reads the raw TCP peer (PeerContext), so it must run
	// before realIP rewrites RemoteAddr from proxy headers.
	panelDomain := ""
	if d.Config != nil {
		panelDomain = d.Config.Panel.Domain
	}
	r.Use(panelGuard(panelDomain))
	// realIP must precede everything that reads RemoteAddr (rate-limit keys,
	// session metadata).
	r.Use(realIP)
	r.Use(requestLogger)
	r.Use(middleware.Recoverer)
	r.Use(versionHeader(d.Version))
	// The request timeout is applied per group below, not globally, so that
	// streaming routes (events, logs) live outside it.

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if d.DB == nil || d.DB.PingContext(r.Context()) != nil {
			writeError(w, http.StatusServiceUnavailable, codeInternal, "database unreachable")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	})

	ah := &authHandlers{auth: d.Auth}
	sh := &systemHandlers{version: d.Version, cfg: d.Config, catalog: d.Catalog, dk: d.Docker, platform: d.Platform, st: d.Store, update: d.Update, started: time.Now()}
	r.Route("/v1", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(middleware.Timeout(30 * time.Second))

			// Public: everything a client can reach without a session.
			r.Post("/auth/login", ah.login)

			// System plugin callbacks, authenticated by per-site tokens.
			if d.Sites != nil {
				ih := &internalHandlers{sites: d.Sites}
				r.Post("/internal/sites/{id}/purge", ih.purge)
			}

			// Bearer-protected. RequireAuth stays on this group only.
			r.Group(func(r chi.Router) {
				r.Use(RequireAuth(d.Auth))

				r.Post("/auth/logout", ah.logout)
				r.Get("/auth/session", ah.currentSession)
				r.Get("/auth/sessions", ah.listSessions)
				r.Delete("/auth/sessions/{id}", ah.revokeSession)
				r.Post("/auth/password", ah.changePassword)

				r.Get("/system/meta", sh.meta)
				r.Get("/system/runtime", sh.runtime)
				r.Get("/system/status", sh.status)
				r.With(RequireAdmin).Post("/system/actions/{action}", sh.action)
				r.With(RequireAdmin).Put("/system/update/settings", sh.updateSettings)

				if d.Sites != nil {
					siteH := &siteHandlers{sites: d.Sites, st: d.Store}
					jobH := &jobHandlers{st: d.Store, hub: d.Hub}

					r.Get("/sites", siteH.list)
					r.Get("/sites/{id}", siteH.get)
					r.Get("/sites/{id}/activity", siteH.activity)
					r.Get("/sites/{id}/stats", siteH.stats)
					r.Get("/sites/{id}/metrics", siteH.metrics)
					r.Get("/sites/{id}/screenshot", siteH.screenshot)
					r.Get("/jobs", jobH.list)
					r.Get("/jobs/{id}", jobH.get)

					// Mutations are admin-only; members can observe.
					r.Group(func(r chi.Router) {
						r.Use(RequireAdmin)
						r.Post("/sites", siteH.create)
						r.Patch("/sites/{id}", siteH.update)
						r.Delete("/sites/{id}", siteH.delete)
						r.Post("/sites/{id}/actions/{action}", siteH.action)
						r.Post("/sites/{id}/login-link", siteH.loginLink)
					})
					// WP-CLI can run for minutes: its own timeout.
					r.With(RequireAdmin, middleware.Timeout(6*time.Minute)).Post("/sites/{id}/wp", siteH.wp)
				}
			})
		})

		// Streaming routes: authenticated, no request timeout.
		if d.Sites != nil && d.Hub != nil {
			r.Group(func(r chi.Router) {
				r.Use(RequireAuth(d.Auth))
				stop := d.Shutdown
				if stop == nil {
					stop = context.Background()
				}
				jobH := &jobHandlers{st: d.Store, hub: d.Hub, shutdown: stop.Done()}
				siteH := &siteHandlers{sites: d.Sites, st: d.Store, shutdown: stop.Done()}
				r.Get("/events", jobH.events)
				r.Get("/sites/{id}/logs", siteH.logs)
			})
		}
	})

	if d.WebUI != nil {
		r.NotFound(func(w http.ResponseWriter, req *http.Request) {
			if strings.HasPrefix(req.URL.Path, "/v1/") || req.URL.Path == "/v1" {
				writeError(w, http.StatusNotFound, codeNotFound, "not found")
				return
			}
			d.WebUI.ServeHTTP(w, req)
		})
	}

	return r
}
