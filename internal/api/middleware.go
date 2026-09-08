package api

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/Hinkolas/presso/internal/auth"
	"github.com/Hinkolas/presso/internal/control"
)

// SessionCookie carries the bearer token for the embedded web UI. The API
// also accepts the token as an Authorization header (CLI clients). The
// cookie is httpOnly, so the SPA never sees the token.
const SessionCookie = "presso_session"

// CSRFHeader must accompany cookie-authenticated state changes. Browsers do
// not let cross-site pages set custom headers, so its presence proves the
// request came from our own origin.
const (
	CSRFHeader = "X-Requested-With"
	CSRFValue  = "presso"
)

// RequireAuth guards a route group with bearer-token authentication (header
// or session cookie) and puts the resolved user, session, and raw token on
// the request context. It must wrap only the protected group, never the
// router root, or login locks itself out.
func RequireAuth(a auth.Authenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, fromCookie, ok := requestToken(r)
			if !ok {
				writeError(w, http.StatusUnauthorized, codeInvalidToken, "missing or malformed Authorization header")
				return
			}
			if fromCookie && !safeMethod(r.Method) && r.Header.Get(CSRFHeader) != CSRFValue {
				writeError(w, http.StatusForbidden, codeForbidden, "missing "+CSRFHeader+" header")
				return
			}
			user, sess, err := a.Authenticate(r.Context(), token)
			if err != nil {
				if fromCookie {
					clearSessionCookie(w, r)
				}
				writeAuthError(r.Context(), w, err)
				return
			}
			ctx := context.WithValue(r.Context(), ctxKeyUser, user)
			ctx = context.WithValue(ctx, ctxKeySession, sess)
			ctx = context.WithValue(ctx, ctxKeyToken, token)
			ctx = context.WithValue(ctx, ctxKeyCookieAuth, fromCookie)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func safeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}

// requestToken prefers the Authorization header and falls back to the
// session cookie.
func requestToken(r *http.Request) (token string, fromCookie bool, ok bool) {
	if t, ok := bearerToken(r); ok {
		return t, false, true
	}
	if c, err := r.Cookie(SessionCookie); err == nil && c.Value != "" {
		return c.Value, true, true
	}
	return "", false, false
}

// RequireAdmin guards management routes; it must sit inside RequireAuth so
// the user is already on the context. The role is read from the per-request
// user row, so a demotion takes effect immediately.
func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user := UserFrom(r.Context()); user == nil || user.Role != auth.RoleAdmin {
			writeError(w, http.StatusForbidden, codeForbidden, "admin role required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// bearerToken extracts the token from "Authorization: Bearer <token>",
// matching the scheme case-insensitively per RFC 9110.
func bearerToken(r *http.Request) (string, bool) {
	scheme, rest, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	rest = strings.TrimSpace(rest)
	return rest, rest != ""
}

// setSessionCookie stores the token for the browser. Secure is set whenever
// the request arrived over TLS (directly or via a proxy that says so).
func setSessionCookie(w http.ResponseWriter, r *http.Request, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name: SessionCookie, Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: isHTTPS(r), Expires: expires,
	})
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: SessionCookie, Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: isHTTPS(r), MaxAge: -1,
	})
}

func isHTTPS(r *http.Request) bool {
	return r.TLS != nil
}

// requestLogger emits one slog line per request. It sits inside RequestID so
// the id is available, and outside the handlers so panics recovered further
// in still get logged with a 500 status by Recoverer. Streaming endpoints
// (SSE) are logged at debug level to keep the log readable.
func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		start := time.Now()
		next.ServeHTTP(ww, r)
		level := slog.LevelInfo
		if strings.HasPrefix(r.URL.Path, "/v1/events") || strings.HasPrefix(r.URL.Path, "/api/v1/events") || strings.HasSuffix(r.URL.Path, "/logs") || !strings.Contains(r.URL.Path, "/v1/") {
			level = slog.LevelDebug
		}
		slog.Log(r.Context(), level, "http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"bytes", ww.BytesWritten(),
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", middleware.GetReqID(r.Context()),
		)
	})
}

// VersionHeader carries the engine build version on every response so
// clients can diagnose version skew pre-auth and on failures. Diagnostics
// only: compatibility gates stay on explicit signals, never on versions.
const VersionHeader = "Presso-Version"

func versionHeader(version string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if version != "" {
				w.Header().Set(VersionHeader, version)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// realIP folds the proxy-reported client address into r.RemoteAddr: X-Real-IP
// first, else the rightmost X-Forwarded-For entry, the one appended by the
// nearest hop. These headers are trusted because the engine is documented to
// run behind the operator's reverse proxy when exposed. (chi's RealIP
// middleware is deprecated for using the leftmost, client-spoofable value.)
func realIP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !control.IsEdge(r.TLS) {
			next.ServeHTTP(w, r)
			return
		}
		if ip := strings.TrimSpace(r.Header.Get("X-Real-IP")); ip != "" {
			r.RemoteAddr = ip
		} else if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if ip := strings.TrimSpace(parts[len(parts)-1]); ip != "" {
				r.RemoteAddr = ip
			}
		}
		next.ServeHTTP(w, r)
	})
}

// clientIP returns the request's client address without the port; realIP has
// already resolved proxy headers into RemoteAddr.
func clientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
