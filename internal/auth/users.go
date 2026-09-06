package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/mail"
	"strings"

	"github.com/google/uuid"

	"github.com/Hinkolas/presso/internal/store"
)

// The user-management functions are package-level (not Service methods) so
// the operator CLI can call them with nothing but database access: creating
// the first admin must not require the serve-only configuration.

const minPasswordLen = 12

func validateEmail(email string) error {
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email || addr.Name != "" {
		return ErrInvalidEmail
	}
	return nil
}

func validatePassword(password string) error {
	if len(password) < minPasswordLen {
		return ErrWeakPassword
	}
	return nil
}

func validateRole(role string) error {
	if role != RoleAdmin && role != RoleMember {
		return ErrInvalidRole
	}
	return nil
}

// CreateUser adds an operator account. Email is stored as given but compared
// case-insensitively.
func CreateUser(ctx context.Context, st *store.Store, email, name, password, role string) (*store.User, error) {
	email = strings.TrimSpace(email)
	if err := validateEmail(email); err != nil {
		return nil, err
	}
	if err := validatePassword(password); err != nil {
		return nil, err
	}
	if err := validateRole(role); err != nil {
		return nil, err
	}
	hash, err := hashPassword(password)
	if err != nil {
		return nil, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("auth: user id: %w", err)
	}
	now := utcNow()
	user, err := st.CreateUser(ctx, store.CreateUserParams{
		ID: id, Email: email, Name: name, Role: role, PasswordHash: hash,
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		if store.IsUniqueViolation(err) {
			return nil, ErrEmailTaken
		}
		return nil, err
	}
	return &user, nil
}

// SetUserRole changes a user's role, refusing to demote the last admin.
func SetUserRole(ctx context.Context, st *store.Store, userID uuid.UUID, role string) (store.User, error) {
	if err := validateRole(role); err != nil {
		return store.User{}, err
	}
	var updated store.User
	err := st.WithTx(ctx, func(q *store.Queries) error {
		user, err := q.GetUserByID(ctx, userID)
		if err != nil {
			return notFound(err)
		}
		if user.Role == RoleAdmin && role != RoleAdmin {
			if err := ensureAdminRemains(ctx, q); err != nil {
				return err
			}
		}
		updated, err = q.SetUserRole(ctx, store.SetUserRoleParams{Role: role, UpdatedAt: utcNow(), ID: userID})
		return err
	})
	return updated, err
}

// ResetUserPassword is the operator recovery path: no old password, only
// database access. Every session of the user is revoked.
func ResetUserPassword(ctx context.Context, st *store.Store, userID uuid.UUID, password string) error {
	if err := validatePassword(password); err != nil {
		return err
	}
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	return st.WithTx(ctx, func(q *store.Queries) error {
		if err := q.SetUserPasswordHash(ctx, store.SetUserPasswordHashParams{PasswordHash: hash, UpdatedAt: utcNow(), ID: userID}); err != nil {
			return err
		}
		_, err := q.DeleteSessionsByUser(ctx, userID)
		return err
	})
}

// DeleteUser removes an account (sessions cascade), refusing to delete the
// last admin.
func DeleteUser(ctx context.Context, st *store.Store, userID uuid.UUID) error {
	return st.WithTx(ctx, func(q *store.Queries) error {
		user, err := q.GetUserByID(ctx, userID)
		if err != nil {
			return notFound(err)
		}
		if user.Role == RoleAdmin {
			if err := ensureAdminRemains(ctx, q); err != nil {
				return err
			}
		}
		_, err = q.DeleteUserByID(ctx, userID)
		return err
	})
}

// ensureAdminRemains guards the last-admin invariant for a transaction that
// is about to remove one admin. The single-connection SQLite store serializes
// transactions, so count-then-mutate cannot race.
func ensureAdminRemains(ctx context.Context, q *store.Queries) error {
	n, err := q.CountAdmins(ctx)
	if err != nil {
		return err
	}
	if n <= 1 {
		return ErrLastAdmin
	}
	return nil
}

func notFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
