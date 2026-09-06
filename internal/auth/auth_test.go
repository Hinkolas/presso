package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Hinkolas/presso/internal/store"
	"github.com/Hinkolas/presso/internal/testdb"
)

func TestLoginAndAuthenticate(t *testing.T) {
	ctx := context.Background()
	st := testdb.New(t)
	svc := New(st, Config{})

	user, err := CreateUser(ctx, st, "Nick@example.com", "Nick", "hunter2hunter2", RoleAdmin)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := CreateUser(ctx, st, "nick@example.com", "", "hunter2hunter2", RoleMember); !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("duplicate email: got %v, want ErrEmailTaken", err)
	}

	if _, err := svc.Login(ctx, "nick@example.com", "wrong-password", SessionMeta{}); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong password: got %v", err)
	}
	if _, err := svc.Login(ctx, "nobody@example.com", "hunter2hunter2", SessionMeta{}); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("unknown user: got %v", err)
	}

	sess, err := svc.Login(ctx, "NICK@example.com", "hunter2hunter2", SessionMeta{IPAddress: "10.0.0.1", UserAgent: "test"})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	gotUser, gotSess, err := svc.Authenticate(ctx, sess.Token)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if gotUser.ID != user.ID || gotSess.ID != sess.ID {
		t.Fatalf("authenticate resolved wrong rows")
	}
	if gotSess.IpAddress != "10.0.0.1" {
		t.Fatalf("ip not recorded: %q", gotSess.IpAddress)
	}

	if err := svc.Logout(ctx, sess.Token); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if _, _, err := svc.Authenticate(ctx, sess.Token); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("after logout: got %v", err)
	}
}

func TestSessionExpiryAndSlidingRefresh(t *testing.T) {
	ctx := context.Background()
	st := testdb.New(t)
	svc := New(st, Config{SessionTTL: time.Hour, SessionUpdateAge: 10 * time.Minute})
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }

	if _, err := CreateUser(ctx, st, "a@example.com", "", "hunter2hunter2", RoleAdmin); err != nil {
		t.Fatal(err)
	}
	sess, err := svc.Login(ctx, "a@example.com", "hunter2hunter2", SessionMeta{})
	if err != nil {
		t.Fatal(err)
	}

	// Within the update age: no refresh.
	now = now.Add(5 * time.Minute)
	_, got, err := svc.Authenticate(ctx, sess.Token)
	if err != nil {
		t.Fatal(err)
	}
	if !got.ExpiresAt.Equal(sess.ExpiresAt) {
		t.Fatalf("expiry moved too early: %v -> %v", sess.ExpiresAt, got.ExpiresAt)
	}

	// Past the update age: expiry slides to now + TTL.
	now = now.Add(10 * time.Minute)
	_, got, err = svc.Authenticate(ctx, sess.Token)
	if err != nil {
		t.Fatal(err)
	}
	if !got.ExpiresAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("expiry not extended: %v", got.ExpiresAt)
	}

	// Past expiry: rejected, and the sweeper removes it.
	now = now.Add(2 * time.Hour)
	if _, _, err := svc.Authenticate(ctx, sess.Token); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expired session: got %v", err)
	}
	if err := svc.SweepExpired(ctx); err != nil {
		t.Fatal(err)
	}
	sessions, err := st.ListSessionsByUser(ctx, listAllParams(sess))
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 0 {
		t.Fatalf("sweep left %d sessions", len(sessions))
	}
}

func TestLastAdminGuard(t *testing.T) {
	ctx := context.Background()
	st := testdb.New(t)

	admin, err := CreateUser(ctx, st, "admin@example.com", "", "hunter2hunter2", RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SetUserRole(ctx, st, admin.ID, RoleMember); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("demote last admin: got %v", err)
	}
	if err := DeleteUser(ctx, st, admin.ID); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("delete last admin: got %v", err)
	}
	second, err := CreateUser(ctx, st, "second@example.com", "", "hunter2hunter2", RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SetUserRole(ctx, st, admin.ID, RoleMember); err != nil {
		t.Fatalf("demote with another admin present: %v", err)
	}
	if err := DeleteUser(ctx, st, second.ID); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("delete the now-last admin: got %v", err)
	}
}

func TestValidation(t *testing.T) {
	ctx := context.Background()
	st := testdb.New(t)
	cases := []struct {
		email, password, role string
		want                  error
	}{
		{"not-an-email", "hunter2hunter2", RoleAdmin, ErrInvalidEmail},
		{"ok@example.com", "short", RoleAdmin, ErrWeakPassword},
		{"ok@example.com", "hunter2hunter2", "root", ErrInvalidRole},
	}
	for _, c := range cases {
		if _, err := CreateUser(ctx, st, c.email, "", c.password, c.role); !errors.Is(err, c.want) {
			t.Errorf("CreateUser(%q, %q, %q) = %v, want %v", c.email, c.password, c.role, err, c.want)
		}
	}
}

// listAllParams lists every session of the session's user regardless of
// expiry (the query's cutoff is set far in the past).
func listAllParams(s *Session) store.ListSessionsByUserParams {
	return store.ListSessionsByUserParams{UserID: s.User.ID, ExpiresAt: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)}
}
