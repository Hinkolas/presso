// Package auth implements operator authentication for the engine: email +
// password login with argon2id hashing and opaque DB-backed bearer sessions.
// It is deliberately self-contained (no external IdP); the Authenticator
// interface is the seam a future OIDC integration would slot into.
//
// There is no registration or password-recovery flow by design: users are
// created by the operator (see CreateUser, wired to `pressod user`).
//
// Two-factor authentication and sudo mode are intentionally absent from the
// MVP; the session model leaves room for both.
package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Hinkolas/presso/internal/store"
)

// Roles are code, not data: the database stores which role a user has, never
// what a role means.
const (
	RoleAdmin  = "admin"
	RoleMember = "member"
)

// Config carries the auth policy. Zero values fall back to the defaults below.
type Config struct {
	SessionTTL       time.Duration // bearer session lifetime
	SessionUpdateAge time.Duration // sliding refresh: extend when a session's last touch is older than this
}

const (
	defaultSessionTTL       = 30 * 24 * time.Hour
	defaultSessionUpdateAge = 24 * time.Hour
)

// Login attempt limits (fixed windows, per process).
const (
	loginUserLimit  = 10
	loginIPLimit    = 30
	rateLimitWindow = 15 * time.Minute
)

// Service is the concrete Authenticator over the SQLite store.
type Service struct {
	st      *store.Store
	cfg     Config
	limiter *rateLimiter
	now     func() time.Time // test seam; always UTC (timestamps compare as text in SQLite)
}

// Authenticator validates bearer tokens; the HTTP middleware depends on this
// interface rather than on *Service.
type Authenticator interface {
	Authenticate(ctx context.Context, token string) (*store.User, *store.Session, error)
}

func New(st *store.Store, cfg Config) *Service {
	if cfg.SessionTTL == 0 {
		cfg.SessionTTL = defaultSessionTTL
	}
	if cfg.SessionUpdateAge == 0 {
		cfg.SessionUpdateAge = defaultSessionUpdateAge
	}
	s := &Service{st: st, cfg: cfg, now: utcNow}
	s.limiter = newRateLimiter(func() time.Time { return s.now() })
	return s
}

func utcNow() time.Time { return time.Now().UTC() }

// SessionMeta is client context recorded on new sessions.
type SessionMeta struct {
	IPAddress string
	UserAgent string
}

// Session is a freshly created login session. Token is the plaintext bearer
// token; this is the only place it ever exists.
type Session struct {
	ID        uuid.UUID
	Token     string
	ExpiresAt time.Time
	User      store.User
}

// Login verifies email + password and opens a session.
func (s *Service) Login(ctx context.Context, email, password string, meta SessionMeta) (*Session, error) {
	userOK := s.limiter.allow("login:user:"+strings.ToLower(email), loginUserLimit, rateLimitWindow)
	ipOK := s.limiter.allow("login:ip:"+meta.IPAddress, loginIPLimit, rateLimitWindow)
	if !userOK || !ipOK {
		return nil, ErrRateLimited
	}

	user, err := s.st.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Pay the argon2 cost anyway so response timing does not reveal
			// whether the email exists.
			verifyPassword(password, dummyHash())
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}
	if !verifyPassword(password, user.PasswordHash) {
		return nil, ErrInvalidCredentials
	}
	return s.createSession(ctx, user, meta)
}

// Authenticate resolves a bearer token to its user and session, sliding the
// expiry forward when the session has not been touched for SessionUpdateAge.
func (s *Service) Authenticate(ctx context.Context, token string) (*store.User, *store.Session, error) {
	row, err := s.st.GetSessionAndUserByTokenHash(ctx, hashToken(token))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, ErrInvalidToken
		}
		return nil, nil, err
	}
	now := s.now()
	if !row.Session.ExpiresAt.After(now) {
		return nil, nil, ErrInvalidToken
	}
	if now.Sub(row.Session.UpdatedAt) > s.cfg.SessionUpdateAge {
		exp := now.Add(s.cfg.SessionTTL)
		err := s.st.ExtendSession(ctx, store.ExtendSessionParams{ID: row.Session.ID, ExpiresAt: exp, UpdatedAt: now})
		if err != nil {
			// Best effort: the session is still valid, only the refresh failed.
			slog.WarnContext(ctx, "auth: extend session", "err", err)
		} else {
			row.Session.ExpiresAt = exp
		}
	}
	return &row.User, &row.Session, nil
}

// Logout revokes the session behind token.
func (s *Service) Logout(ctx context.Context, token string) error {
	n, err := s.st.DeleteSessionByTokenHash(ctx, hashToken(token))
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrInvalidToken
	}
	return nil
}

// ListSessions returns the user's live sessions, newest first.
func (s *Service) ListSessions(ctx context.Context, userID uuid.UUID) ([]store.Session, error) {
	return s.st.ListSessionsByUser(ctx, store.ListSessionsByUserParams{UserID: userID, ExpiresAt: s.now()})
}

// RevokeSession deletes one of the user's own sessions.
func (s *Service) RevokeSession(ctx context.Context, userID, sessionID uuid.UUID) error {
	n, err := s.st.DeleteSessionByID(ctx, store.DeleteSessionByIDParams{ID: sessionID, UserID: userID})
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ChangePassword verifies the current password, stores the new hash, and
// revokes every other session of the user.
func (s *Service) ChangePassword(ctx context.Context, userID uuid.UUID, current, newPassword string, keepSession uuid.UUID) error {
	user, err := s.st.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}
	if !verifyPassword(current, user.PasswordHash) {
		return ErrInvalidCredentials
	}
	if err := validatePassword(newPassword); err != nil {
		return err
	}
	hash, err := hashPassword(newPassword)
	if err != nil {
		return err
	}
	now := s.now()
	return s.st.WithTx(ctx, func(q *store.Queries) error {
		if err := q.SetUserPasswordHash(ctx, store.SetUserPasswordHashParams{PasswordHash: hash, UpdatedAt: now, ID: userID}); err != nil {
			return err
		}
		sessions, err := q.ListSessionsByUser(ctx, store.ListSessionsByUserParams{UserID: userID, ExpiresAt: now})
		if err != nil {
			return err
		}
		for _, sess := range sessions {
			if sess.ID == keepSession {
				continue
			}
			if _, err := q.DeleteSessionByID(ctx, store.DeleteSessionByIDParams{ID: sess.ID, UserID: userID}); err != nil {
				return err
			}
		}
		return nil
	})
}

// SweepExpired deletes sessions past their expiry; run it periodically.
func (s *Service) SweepExpired(ctx context.Context) error {
	_, err := s.st.DeleteExpiredSessions(ctx, s.now())
	return err
}

func (s *Service) createSession(ctx context.Context, user store.User, meta SessionMeta) (*Session, error) {
	token, err := newToken()
	if err != nil {
		return nil, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("auth: session id: %w", err)
	}
	now := s.now()
	row, err := s.st.CreateSession(ctx, store.CreateSessionParams{
		ID:        id,
		UserID:    user.ID,
		TokenHash: hashToken(token),
		ExpiresAt: now.Add(s.cfg.SessionTTL),
		IpAddress: meta.IPAddress,
		UserAgent: meta.UserAgent,
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		return nil, err
	}
	return &Session{ID: row.ID, Token: token, ExpiresAt: row.ExpiresAt, User: user}, nil
}
