package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Hinkolas/presso/internal/auth"
	"github.com/Hinkolas/presso/internal/store"
)

type authHandlers struct {
	auth *auth.Service
}

// --- payloads ---

type userPayload struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

func newUserPayload(u *store.User) userPayload {
	return userPayload{ID: u.ID.String(), Email: u.Email, Name: u.Name, Role: u.Role, CreatedAt: u.CreatedAt}
}

// sessionCreatedPayload is the one and only place a bearer token leaves the
// server.
type sessionCreatedPayload struct {
	Token     string      `json:"token"`
	ExpiresAt time.Time   `json:"expires_at"`
	User      userPayload `json:"user"`
}

type sessionPayload struct {
	ID        string    `json:"id"`
	ExpiresAt time.Time `json:"expires_at"`
	IPAddress string    `json:"ip_address"`
	UserAgent string    `json:"user_agent"`
	CreatedAt time.Time `json:"created_at"`
	Current   bool      `json:"current"`
}

func newSessionPayload(s *store.Session, currentID uuid.UUID) sessionPayload {
	return sessionPayload{
		ID:        s.ID.String(),
		ExpiresAt: s.ExpiresAt,
		IPAddress: s.IpAddress,
		UserAgent: s.UserAgent,
		CreatedAt: s.CreatedAt,
		Current:   s.ID == currentID,
	}
}

// --- handlers ---

func (h *authHandlers) sessionMeta(r *http.Request) auth.SessionMeta {
	ua := r.UserAgent()
	if len(ua) > 512 {
		ua = ua[:512]
	}
	return auth.SessionMeta{IPAddress: clientIP(r), UserAgent: ua}
}

// POST /v1/auth/login
func (h *authHandlers) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	if req.Email == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, codeBadRequest, "email and password are required")
		return
	}
	sess, err := h.auth.Login(r.Context(), req.Email, req.Password, h.sessionMeta(r))
	if err != nil {
		writeAuthError(r.Context(), w, err)
		return
	}
	// The browser UI relies on the cookie; CLI clients read the token.
	setSessionCookie(w, r, sess.Token, sess.ExpiresAt)
	writeJSON(w, http.StatusOK, struct {
		Session sessionCreatedPayload `json:"session"`
	}{sessionCreatedPayload{Token: sess.Token, ExpiresAt: sess.ExpiresAt, User: newUserPayload(&sess.User)}})
}

// POST /v1/auth/logout
func (h *authHandlers) logout(w http.ResponseWriter, r *http.Request) {
	if cookieAuthFrom(r.Context()) {
		clearSessionCookie(w, r)
	}
	if err := h.auth.Logout(r.Context(), tokenFrom(r.Context())); err != nil {
		writeAuthError(r.Context(), w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GET /v1/auth/session
func (h *authHandlers) currentSession(w http.ResponseWriter, r *http.Request) {
	user := UserFrom(r.Context())
	sess := SessionFrom(r.Context())
	writeJSON(w, http.StatusOK, struct {
		User    userPayload    `json:"user"`
		Session sessionPayload `json:"session"`
	}{newUserPayload(user), newSessionPayload(sess, sess.ID)})
}

// GET /v1/auth/sessions
func (h *authHandlers) listSessions(w http.ResponseWriter, r *http.Request) {
	user := UserFrom(r.Context())
	current := SessionFrom(r.Context())
	sessions, err := h.auth.ListSessions(r.Context(), user.ID)
	if err != nil {
		writeAuthError(r.Context(), w, err)
		return
	}
	payload := make([]sessionPayload, len(sessions))
	for i := range sessions {
		payload[i] = newSessionPayload(&sessions[i], current.ID)
	}
	writeJSON(w, http.StatusOK, struct {
		Sessions []sessionPayload `json:"sessions"`
	}{payload})
}

// DELETE /v1/auth/sessions/{id}
func (h *authHandlers) revokeSession(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, codeNotFound, "not found")
		return
	}
	if err := h.auth.RevokeSession(r.Context(), UserFrom(r.Context()).ID, id); err != nil {
		writeAuthError(r.Context(), w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /v1/auth/password
func (h *authHandlers) changePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	if req.CurrentPassword == "" || req.NewPassword == "" {
		writeError(w, http.StatusBadRequest, codeBadRequest, "current_password and new_password are required")
		return
	}
	err := h.auth.ChangePassword(r.Context(), UserFrom(r.Context()).ID, req.CurrentPassword, req.NewPassword, SessionFrom(r.Context()).ID)
	if err != nil {
		writeAuthError(r.Context(), w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
