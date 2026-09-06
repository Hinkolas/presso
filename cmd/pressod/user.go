package main

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/Hinkolas/presso/internal/auth"
	"github.com/Hinkolas/presso/internal/config"
	"github.com/Hinkolas/presso/internal/store"
	"github.com/Hinkolas/presso/migrations"
)

// runUser implements the operator CLI for engine accounts. It loads only
// config.Base: user management needs the data directory and must not demand
// AUTH_SECRET or other serve-only settings. The first admin is created here.
func runUser(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: pressod user <create|list|set-role|reset-password|delete> [flags]")
	}
	ctx := context.Background()

	cfg, err := config.Load[config.Base](ctx)
	if err != nil {
		return err
	}
	db, err := store.Open(ctx, cfg.DatabasePath())
	if err != nil {
		return err
	}
	defer db.Close()
	// A fresh install creates its first admin before the first serve, so the
	// schema must exist here too.
	if _, err := migrations.Up(ctx, db); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	st := store.NewStore(db)

	switch args[0] {
	case "create":
		return userCreate(ctx, st, args[1:])
	case "list":
		return userList(ctx, st)
	case "set-role":
		return userSetRole(ctx, st, args[1:])
	case "reset-password":
		return userResetPassword(ctx, st, args[1:])
	case "delete":
		return userDelete(ctx, st, args[1:])
	default:
		return fmt.Errorf("unknown user command %q (available: create, list, set-role, reset-password, delete)", args[0])
	}
}

func userCreate(ctx context.Context, st *store.Store, args []string) error {
	fs := flag.NewFlagSet("user create", flag.ContinueOnError)
	email := fs.String("email", "", "login email (required)")
	name := fs.String("name", "", "display name")
	role := fs.String("role", auth.RoleMember, "role: admin or member (the first user needs admin)")
	passwordStdin := fs.Bool("password-stdin", false, "read the password from stdin instead of prompting")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *email == "" {
		return errors.New("usage: pressod user create --email <address> [--name <display>] [--role admin|member] [--password-stdin]")
	}
	password, err := readPassword(*passwordStdin)
	if err != nil {
		return err
	}
	user, err := auth.CreateUser(ctx, st, *email, *name, password, *role)
	if err != nil {
		return err
	}
	fmt.Printf("created %s %s (%s)\n", user.Role, user.Email, user.ID)
	return nil
}

func userList(ctx context.Context, st *store.Store) error {
	users, err := st.ListUsers(ctx)
	if err != nil {
		return err
	}
	if len(users) == 0 {
		fmt.Println("no users")
		return nil
	}
	for _, u := range users {
		fmt.Printf("%s  %-6s  %s  created %s\n", u.ID, u.Role, u.Email, u.CreatedAt.Format("2006-01-02"))
	}
	return nil
}

// userSetRole is the operator override for role changes: it has no caller
// identity, so only the last-admin guard applies. It exists so a lost-admin
// install is recoverable from the server itself.
func userSetRole(ctx context.Context, st *store.Store, args []string) error {
	fs := flag.NewFlagSet("user set-role", flag.ContinueOnError)
	email := fs.String("email", "", "login email (required)")
	role := fs.String("role", "", "role: admin or member (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *email == "" || *role == "" {
		return errors.New("usage: pressod user set-role --email <address> --role admin|member")
	}
	user, err := lookupUser(ctx, st, *email)
	if err != nil {
		return err
	}
	user, err = auth.SetUserRole(ctx, st, user.ID, *role)
	if err != nil {
		return err
	}
	fmt.Printf("%s is now %s\n", user.Email, user.Role)
	return nil
}

// userResetPassword is the recovery path for a forgotten password: no old
// password, only database access. Every session of the user is revoked.
func userResetPassword(ctx context.Context, st *store.Store, args []string) error {
	fs := flag.NewFlagSet("user reset-password", flag.ContinueOnError)
	email := fs.String("email", "", "login email (required)")
	passwordStdin := fs.Bool("password-stdin", false, "read the new password from stdin instead of prompting")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *email == "" {
		return errors.New("usage: pressod user reset-password --email <address> [--password-stdin]")
	}
	user, err := lookupUser(ctx, st, *email)
	if err != nil {
		return err
	}
	password, err := readPassword(*passwordStdin)
	if err != nil {
		return err
	}
	if err := auth.ResetUserPassword(ctx, st, user.ID, password); err != nil {
		return err
	}
	fmt.Printf("password reset for %s, sessions revoked\n", user.Email)
	return nil
}

func userDelete(ctx context.Context, st *store.Store, args []string) error {
	fs := flag.NewFlagSet("user delete", flag.ContinueOnError)
	email := fs.String("email", "", "login email (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *email == "" {
		return errors.New("usage: pressod user delete --email <address>")
	}
	user, err := lookupUser(ctx, st, *email)
	if err != nil {
		return err
	}
	// Through the service so the last-admin guard applies: the operator CLI
	// is the recovery path, not a way around it.
	if err := auth.DeleteUser(ctx, st, user.ID); err != nil {
		if errors.Is(err, auth.ErrLastAdmin) {
			return errors.New("refusing to delete the last admin; promote another user first")
		}
		return err
	}
	fmt.Printf("deleted user %s\n", user.Email)
	return nil
}

func lookupUser(ctx context.Context, st *store.Store, email string) (store.User, error) {
	user, err := st.GetUserByEmail(ctx, email)
	if errors.Is(err, sql.ErrNoRows) {
		return store.User{}, fmt.Errorf("no user with email %q", email)
	}
	return user, err
}

// readPassword collects the password without it ending up in shell history:
// interactively via a no-echo double prompt, or from stdin for scripting
// (printf '%s' "$PW" | pressod user create --email a@b.c --password-stdin).
func readPassword(fromStdin bool) (string, error) {
	if fromStdin {
		data, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && data == "" {
			return "", fmt.Errorf("read password from stdin: %w", err)
		}
		return strings.TrimRight(data, "\r\n"), nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", errors.New("read password: not a terminal (use --password-stdin)")
	}
	fmt.Fprint(os.Stderr, "Password: ")
	first, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	fmt.Fprint(os.Stderr, "Repeat password: ")
	second, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	if string(first) != string(second) {
		return "", errors.New("passwords do not match")
	}
	if len(first) == 0 {
		return "", errors.New("password must not be empty")
	}
	return string(first), nil
}
