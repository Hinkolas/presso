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
	"strings"
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

	Platform
	Runtime
	Edge
	Observe
	Shots
}

// Platform configures the shared containers pressod reconciles on the host
// (network, MariaDB, Redis). Listen addresses are host-side published ports:
// the engine is a host process and cannot rely on reaching container IPs
// (impossible on macOS), so everything it talks to publishes on loopback.
type Platform struct {
	// Managed=false skips the reconciler entirely (tests, or an operator
	// running the shared services themselves).
	Managed bool `env:"PLATFORM_MANAGED,default=true"`
	// Network is the docker bridge network every presso container joins.
	Network string `env:"PLATFORM_NETWORK,default=presso"`

	MariaDBImage  string `env:"MARIADB_IMAGE,default=mariadb:11.4"`
	MariaDBListen string `env:"MARIADB_LISTEN,default=127.0.0.1:3306"`

	RedisImage     string `env:"REDIS_IMAGE,default=redis:7-alpine"`
	RedisListen    string `env:"REDIS_LISTEN,default=127.0.0.1:6379"`
	RedisMaxMemory string `env:"REDIS_MAXMEMORY,default=256mb"`

	// EngineInternalURL is how site containers reach this engine (system
	// plugin purge callbacks). host.docker.internal resolves on Docker Desktop
	// and, with the host-gateway extra host the engine adds, on Linux too.
	EngineInternalURL string `env:"ENGINE_INTERNAL_URL,default=http://host.docker.internal:7080"`
}

// Runtime configures which presso-runtime images sites can run. The
// WordPress/PHP compatibility matrix itself is embedded (internal/compat);
// these knobs are the server's policy on top of it.
type Runtime struct {
	// ImageTemplate renders the runtime image for a (PHP, WordPress) pair:
	// {php} is e.g. "8.4", {wp} the pinned WordPress release e.g. "6.8.8",
	// and the optional {runtime} is frankenphp|legacy for operators who keep
	// one repository per flavor.
	ImageTemplate string `env:"RUNTIME_IMAGE_TEMPLATE,default=ghcr.io/hinkolas/presso-runtime:php{php}-wp{wp}"`
	// LegacyEnabled offers the nginx + PHP-FPM runtime (PHP 7.4-8.1), which
	// WordPress releases before 6.1 need. Off, those releases and PHP
	// versions disappear from the UI and are rejected by the API.
	LegacyEnabled bool `env:"RUNTIME_LEGACY_ENABLED,default=true"`
	// DefaultWP is the WordPress branch preselected for new sites; empty
	// means the newest one in the matrix.
	DefaultWP string `env:"RUNTIME_DEFAULT_WP,default="`
	// PHPVersions optionally restricts the shipped PHP versions offered
	// (e.g. "8.4,8.3"); empty offers every version the matrix ships.
	PHPVersions []string `env:"RUNTIME_PHP_VERSIONS,default="`
}

// Edge configures the shared Caddy edge container.
type Edge struct {
	Image string `env:"EDGE_IMAGE,default=presso-edge:dev"`
	// HTTPListen/HTTPSListen are the public host ports.
	HTTPListen  string `env:"EDGE_HTTP_LISTEN,default=0.0.0.0:80"`
	HTTPSListen string `env:"EDGE_HTTPS_LISTEN,default=0.0.0.0:443"`
	// AdminListen is the Caddy admin API (config pushes); loopback only.
	AdminListen string `env:"EDGE_ADMIN_LISTEN,default=127.0.0.1:2019"`
	// CacheAdminListen is where the page-cache purge API answers; loopback only.
	CacheAdminListen string `env:"EDGE_CACHE_ADMIN_LISTEN,default=127.0.0.1:2020"`
	// ACMEEmail is passed to Caddy for certificate issuance on real domains.
	ACMEEmail string `env:"ACME_EMAIL,default="`
	// DefaultCacheTTL is the page-cache TTL for sites without an override.
	DefaultCacheTTL time.Duration `env:"EDGE_DEFAULT_CACHE_TTL,default=12h"`
}

// Observe configures the background workers that collect metrics.
type Observe struct {
	CronInterval     time.Duration `env:"CRON_INTERVAL,default=5m"`
	StatsInterval    time.Duration `env:"STATS_INTERVAL,default=30s"`
	MetricsRetention time.Duration `env:"METRICS_RETENTION,default=168h"`
}

// Shots configures the headless browser that captures front-page previews
// of every site for the UI. The browser runs as one shared container on the
// presso network and reaches sites through the edge, so previews show what a
// visitor sees (page cache, TLS and all). Listen is the host-side DevTools
// port; loopback only, since DevTools grants full browser control.
type Shots struct {
	Enabled bool   `env:"SHOTS_ENABLED,default=true"`
	Image   string `env:"SHOTS_IMAGE,default=chromedp/headless-shell:stable"`
	Listen  string `env:"SHOTS_LISTEN,default=127.0.0.1:9222"`
	// Interval is how often running sites are re-captured in the background.
	Interval time.Duration `env:"SHOTS_INTERVAL,default=6h"`
	// Timeout bounds one capture (navigation + render).
	Timeout time.Duration `env:"SHOTS_TIMEOUT,default=45s"`
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
	// Only the syntax is checked here; whether the default WordPress branch
	// and the PHP allowlist make sense against the matrix is compat.New's
	// job (cmd/pressod wires it), so config need not import compat.
	if !strings.Contains(a.ImageTemplate, "{php}") || !strings.Contains(a.ImageTemplate, "{wp}") {
		return fmt.Errorf("RUNTIME_IMAGE_TEMPLATE: must contain {php} and {wp}")
	}
	if a.CronInterval <= 0 || a.StatsInterval <= 0 || a.MetricsRetention <= 0 {
		return fmt.Errorf("CRON_INTERVAL, STATS_INTERVAL, METRICS_RETENTION: must be positive")
	}
	if a.Shots.Interval <= 0 || a.Shots.Timeout <= 0 {
		return fmt.Errorf("SHOTS_INTERVAL, SHOTS_TIMEOUT: must be positive")
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
