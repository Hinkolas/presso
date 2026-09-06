// Package config loads strongly-typed, env-driven configuration for every
// binary. Values come from the environment (12-factor); a local .env is loaded
// for developer convenience only and never overrides real environment
// variables.
package config

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"time"

	"github.com/joho/godotenv"
	"github.com/sethvargo/go-envconfig"
)

// Logging is shared by every binary.
type Logging struct {
	LogLevel  string `env:"LOG_LEVEL,default=info"`
	LogFormat string `env:"LOG_FORMAT,default=text"`
}

// Validate checks enum-like fields so misconfiguration fails fast at startup.
func (l *Logging) Validate() error {
	if err := oneOf("LOG_LEVEL", l.LogLevel, "debug", "info", "warn", "error"); err != nil {
		return err
	}
	return oneOf("LOG_FORMAT", l.LogFormat, "text", "json")
}

// Base is configuration shared by every command that touches the engine
// state (serve, migrate, user).
type Base struct {
	// DataDir is the engine's writable state directory: the SQLite database
	// lives here (presso.db), and later per-site artifacts the engine
	// generates (rendered configs, snapshots metadata). Relative paths
	// resolve against the working directory.
	DataDir string `env:"DATA_DIR,default=./data"`

	Logging
}

// Validate shadows Logging.Validate, so it must chain to it explicitly.
func (b *Base) Validate() error {
	if b.DataDir == "" {
		return fmt.Errorf("DATA_DIR: must not be empty")
	}
	return b.Logging.Validate()
}

// DatabasePath is the SQLite file inside DataDir. One place decides the
// name so migrate, user and serve always open the same database.
func (b *Base) DatabasePath() string {
	return filepath.Join(b.DataDir, "presso.db")
}

// API is the configuration for cmd/pressod serve.
type API struct {
	Base

	// HTTPAddr is where the engine API listens. 7080 by default: 7000
	// collides with macOS AirPlay on dev machines and 7070 is taken by the
	// sibling skali daemon on the same machines.
	HTTPAddr string `env:"HTTP_ADDR,default=:7080"`

	// AuthSecret keys everything the auth system signs or encrypts at rest
	// (later: one-click WordPress login tokens, stored site credentials).
	// Rotating it invalidates those, never passwords.
	AuthSecret string `env:"AUTH_SECRET,required"`

	// SessionTTL is the bearer session lifetime; sessions slide (extend on
	// use) once their last touch is older than a day.
	SessionTTL time.Duration `env:"SESSION_TTL,default=720h"`

	// DockerHost overrides where the engine reaches the Docker daemon. Empty
	// lets the Docker client resolve it (DOCKER_HOST, then the local socket).
	DockerHost string `env:"DOCKER_HOST_OVERRIDE,default="`
}

// Validate shadows Base.Validate, so it must chain to it explicitly.
func (a *API) Validate() error {
	if err := a.Base.Validate(); err != nil {
		return err
	}
	if len(a.AuthSecret) < 32 {
		return fmt.Errorf("AUTH_SECRET: must be at least 32 characters (generate with `openssl rand -base64 32`)")
	}
	if a.SessionTTL <= 0 {
		return fmt.Errorf("SESSION_TTL: must be positive")
	}
	return nil
}

type validator interface{ Validate() error }

// Load reads the environment (plus a dev .env) into a *T and validates it.
func Load[T any](ctx context.Context) (*T, error) {
	// Dev convenience: load .env if present. Load (not Overload) never
	// clobbers real environment variables, and no-ops when the file is absent.
	_ = godotenv.Load()

	var cfg T
	if err := envconfig.Process(ctx, &cfg); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	if v, ok := any(&cfg).(validator); ok {
		if err := v.Validate(); err != nil {
			return nil, fmt.Errorf("config: %w", err)
		}
	}
	return &cfg, nil
}

func oneOf(name, val string, allowed ...string) error {
	if slices.Contains(allowed, val) {
		return nil
	}
	return fmt.Errorf("%s: %q must be one of %v", name, val, allowed)
}
