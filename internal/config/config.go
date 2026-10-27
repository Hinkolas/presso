// Package config loads strongly-typed, env-driven configuration for every
// binary. Values come from the environment (12-factor); a local .env is loaded
// for developer convenience only and never overrides real environment
// variables.
package config

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/sethvargo/go-envconfig"

	"github.com/Hinkolas/presso/internal/edge"
	"github.com/Hinkolas/presso/internal/version"
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
	HTTPAddr          string `env:"HTTP_ADDR,default=127.0.0.1:7080"`
	InternalHTTPSAddr string `env:"INTERNAL_HTTPS_ADDR,default=:7081"`

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
	Panel
	Update
	Observe
	Shots
	Warm
}

// Platform configures the shared containers pressod reconciles on the host
// (network, MariaDB, Valkey). Database and Valkey ports publish on host loopback;
// edge and browser management use Docker exec, including on Docker Desktop.
//
// Like the edge, the MariaDB, Valkey and browser images ship with every
// release (build/*.Dockerfile): {release} is replaced by the engine's release
// tag at load time, and a dev build must name locally built images.
type Platform struct {
	// Managed=false skips the reconciler entirely (tests, or an operator
	// running the shared services themselves).
	Managed bool `env:"PLATFORM_MANAGED,default=true"`
	// Network is the docker bridge network every presso container joins.
	Network string `env:"PLATFORM_NETWORK,default=presso"`

	MariaDBImage  string `env:"MARIADB_IMAGE,default=ghcr.io/hinkolas/presso-mariadb:{release}"`
	MariaDBListen string `env:"MARIADB_LISTEN,default=127.0.0.1:3306"`
	// MariaDBRawImage keeps the unresolved MARIADB_IMAGE. Set by Validate.
	MariaDBRawImage string

	ValkeyImage string `env:"VALKEY_IMAGE,default=ghcr.io/hinkolas/presso-valkey:{release}"`
	// ValkeyRawImage keeps the unresolved VALKEY_IMAGE. Set by Validate.
	ValkeyRawImage  string
	ValkeyListen    string `env:"VALKEY_LISTEN,default=127.0.0.1:6379"`
	ValkeyMaxMemory string `env:"VALKEY_MAXMEMORY,default=256mb"`

	// EngineInternalURL is how site containers reach this engine (system
	// plugin purge callbacks). host.docker.internal resolves on Docker Desktop
	// and, with the host-gateway extra host the engine adds, on Linux too.
	EngineInternalURL string `env:"ENGINE_INTERNAL_URL,default=https://host.docker.internal:7081"`
}

// Runtime configures which presso-runtime images sites can run. The
// WordPress/PHP compatibility matrix itself is embedded (internal/compat);
// these knobs are the server's policy on top of it.
type Runtime struct {
	// ImageTemplate renders the runtime image for a (PHP, WordPress) pair:
	// {php} is e.g. "8.4", {wp} the pinned WordPress release e.g. "6.8.8",
	// {release} the engine's own release tag (runtime images are published
	// per release, so updating the engine moves every site to that release's
	// images), and the optional {runtime} is frankenphp|legacy for operators
	// who keep one repository per flavor. Dev builds have no release and must
	// point this at locally built images (see .env.example).
	ImageTemplate string `env:"RUNTIME_IMAGE_TEMPLATE,default=ghcr.io/hinkolas/presso-runtime:{release}-php{php}-wp{wp}"`
	// LegacyEnabled offers the nginx + PHP-FPM runtime (PHP 7.4-8.1), which
	// WordPress releases before 6.1 need. Off, those releases and PHP
	// versions disappear from the UI and are rejected by the API.
	LegacyEnabled bool `env:"RUNTIME_LEGACY_ENABLED,default=false"`
	// DefaultWP is the WordPress branch preselected for new sites; empty
	// means the newest one in the matrix.
	DefaultWP string `env:"RUNTIME_DEFAULT_WP,default="`
	// PHPVersions optionally restricts the shipped PHP versions offered
	// (e.g. "8.4,8.3"); empty offers every version the matrix ships.
	PHPVersions []string `env:"RUNTIME_PHP_VERSIONS,default="`
}

// Edge configures the shared Caddy edge container.
type Edge struct {
	// Image may contain {release}, replaced by the engine's release tag at
	// load time (the edge ships with every release, like the runtimes).
	Image string `env:"EDGE_IMAGE,default=ghcr.io/hinkolas/presso-edge:{release}"`
	// RawImage keeps the unresolved EDGE_IMAGE so the updater can name
	// the edge image of a release that is not this one. Set by Validate.
	RawImage string
	// HTTPListen/HTTPSListen are the public host ports.
	HTTPListen  string `env:"EDGE_HTTP_LISTEN,default=0.0.0.0:80"`
	HTTPSListen string `env:"EDGE_HTTPS_LISTEN,default=0.0.0.0:443"`
	// ACMEEmail is passed to Caddy for certificate issuance on real domains.
	ACMEEmail string `env:"ACME_EMAIL,default="`
	// DefaultCacheTTL is the page-cache TTL for sites without an override.
	DefaultCacheTTL time.Duration `env:"EDGE_DEFAULT_CACHE_TTL,default=12h"`
	// StaleFor is how long an expired page stays in the cache store after its
	// TTL, available for stale-while-revalidate and stale-if-error. The system
	// plugin advertises 24h in its Cache-Control, so keep this at least that.
	StaleFor time.Duration `env:"EDGE_STALE_FOR,default=24h"`
}

// Panel configures the public HTTPS vhost. The edge authenticates to the
// internal engine listener using its installation-local client certificate.
type Panel struct {
	Domain string `env:"PANEL_DOMAIN,default="`
}

// Update configures the self-update check against GitHub releases. Check=false
// stops the background check and with it automatic updates (air-gapped
// hosts); checking and updating by hand still work. The channel and automatic
// updates are settings in the panel; Channel (UPDATE_CHANNEL, stable or beta)
// is only the channel a host starts on before the setting is first saved
// (install.sh --channel), empty meaning the running build's channel.
// APIBase is overridable so the whole flow can be exercised against a local
// fake feed.
type Update struct {
	Check    bool          `env:"UPDATE_CHECK,default=true"`
	Channel  string        `env:"UPDATE_CHANNEL,default="`
	Interval time.Duration `env:"UPDATE_CHECK_INTERVAL,default=6h"`
	Repo     string        `env:"UPDATE_REPO,default=Hinkolas/presso"`
	APIBase  string        `env:"UPDATE_API_URL,default=https://api.github.com"`
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
// visitor sees (page cache, TLS and all). The engine reaches container-loopback
// DevTools through Docker exec, without publishing a debugging port.
type Shots struct {
	Enabled bool `env:"SHOTS_ENABLED,default=true"`
	// Image may contain {release}, like EDGE_IMAGE. It must provide
	// presso-browser (Chromium's headless shell) and socat.
	Image string `env:"SHOTS_IMAGE,default=ghcr.io/hinkolas/presso-browser:{release}"`
	// RawImage keeps the unresolved SHOTS_IMAGE. Set by Validate.
	RawImage string
	// Interval is how often running sites are re-captured in the background.
	Interval time.Duration `env:"SHOTS_INTERVAL,default=6h"`
	// Timeout bounds one capture (navigation + render).
	Timeout time.Duration `env:"SHOTS_TIMEOUT,default=45s"`
}

// Warm configures the sitemap cache warmer (internal/warm). Sites opt in one
// by one through their cache_warm setting; these knobs bound every run.
type Warm struct {
	// Interval is how often each opted-in site's sitemap is walked. Re-fetching
	// a still-cached page is an edge hit, so keep this below the page TTL.
	Interval time.Duration `env:"WARM_INTERVAL,default=1h"`
	// Timeout bounds one request through the edge.
	Timeout time.Duration `env:"WARM_TIMEOUT,default=20s"`
	// MaxURLs caps the URLs warmed per site and run (one WordPress core
	// sitemap page holds 2000).
	MaxURLs int `env:"WARM_MAX_URLS,default=2000"`
}

// Validate shadows Base.Validate, so it must chain to it explicitly.
// releaseImage is a shared-container image setting that may name the release.
type releaseImage struct {
	key, dev, task string
	ref, raw       *string
	enabled        bool
}

func (a *API) releaseImages() []releaseImage {
	return []releaseImage{
		{"MARIADB_IMAGE", "presso-mariadb:dev", "task build:services", &a.MariaDBImage, &a.MariaDBRawImage, true},
		{"VALKEY_IMAGE", "presso-valkey:dev", "task build:services", &a.ValkeyImage, &a.ValkeyRawImage, true},
		{"EDGE_IMAGE", "presso-edge:dev", "task build:edge", &a.Edge.Image, &a.Edge.RawImage, true},
		{"SHOTS_IMAGE", "presso-browser:dev", "task build:services", &a.Shots.Image, &a.Shots.RawImage, a.Shots.Enabled},
	}
}

// ReleaseImages names the shared-container images of another release: every
// enabled setting that uses {release}, filled with tag. Settings pinned to a
// fixed image do not change with releases and are omitted.
func (a *API) ReleaseImages(tag string) []string {
	var out []string
	for _, img := range a.releaseImages() {
		if img.enabled && strings.Contains(*img.raw, "{release}") {
			out = append(out, strings.ReplaceAll(*img.raw, "{release}", tag))
		}
	}
	return out
}

func (a *API) Validate() error {
	if err := a.Base.Validate(); err != nil {
		return err
	}
	if len(a.AuthSecret) < 32 {
		return fmt.Errorf("AUTH_SECRET: must be at least 32 characters (generate with `openssl rand -base64 32`)")
	}
	for _, key := range []string{"EDGE_ADMIN_LISTEN", "EDGE_CACHE_ADMIN_LISTEN", "SHOTS_LISTEN"} {
		if v := os.Getenv(key); v != "" {
			return fmt.Errorf("%s is obsolete; management uses the Docker API", key)
		}
	}
	for _, suffix := range []string{"IMAGE", "LISTEN", "MAXMEMORY"} {
		if os.Getenv("REDIS_"+suffix) != "" {
			return fmt.Errorf("REDIS_%s is obsolete; fresh installations use VALKEY_%s (see README.md, Object cache); existing Redis installations are not automatically migrated", suffix, suffix)
		}
	}
	cacheHost, _, cacheErr := net.SplitHostPort(a.ValkeyListen)
	if cacheErr != nil || net.ParseIP(cacheHost) == nil || !net.ParseIP(cacheHost).IsLoopback() {
		return fmt.Errorf("VALKEY_LISTEN must bind a loopback IP")
	}
	host, _, err := net.SplitHostPort(a.HTTPAddr)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		return fmt.Errorf("HTTP_ADDR must bind a loopback IP")
	}
	_, internalPort, err := net.SplitHostPort(a.InternalHTTPSAddr)
	if err != nil {
		return fmt.Errorf("INTERNAL_HTTPS_ADDR: %w", err)
	}
	u, err := url.Parse(a.EngineInternalURL)
	if err != nil || u.Scheme != "https" || u.Hostname() != "host.docker.internal" || u.Port() != internalPort || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("ENGINE_INTERNAL_URL must use https://host.docker.internal:<INTERNAL_HTTPS_ADDR port> without a path")
	}
	a.DataDir, err = filepath.Abs(a.DataDir)
	if err != nil {
		return err
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
	// {release} names the images published with this engine's release. A dev
	// build has none, so it must run against locally built images; failing
	// here beats a confusing pull error on the first site.
	release := version.Release()
	if release == "" && strings.Contains(a.ImageTemplate, "{release}") {
		return fmt.Errorf("RUNTIME_IMAGE_TEMPLATE: uses {release} but %s is not a release build; set RUNTIME_IMAGE_TEMPLATE=presso-runtime:php{php}-wp{wp} (locally built images, see .env.example)", version.Version)
	}
	for _, img := range a.releaseImages() {
		*img.raw = *img.ref
		if strings.Contains(*img.ref, "{release}") {
			if release == "" {
				return fmt.Errorf("%s: uses {release} but %s is not a release build; set %s=%s (%s, see .env.example)", img.key, version.Version, img.key, img.dev, img.task)
			}
			*img.ref = strings.ReplaceAll(*img.ref, "{release}", release)
		}
	}
	if a.Panel.Domain != "" {
		a.Panel.Domain = strings.ToLower(strings.TrimSpace(a.Panel.Domain))
		if !edge.ValidHost(a.Panel.Domain) {
			return fmt.Errorf("PANEL_DOMAIN: %q is not a valid hostname", a.Panel.Domain)
		}
	}
	if a.Update.Check && a.Update.Interval < time.Minute {
		return fmt.Errorf("UPDATE_CHECK_INTERVAL: must be at least 1m")
	}
	if a.Update.Channel != "" && a.Update.Channel != "stable" && a.Update.Channel != "beta" {
		return fmt.Errorf("UPDATE_CHANNEL: must be stable or beta")
	}
	if !strings.Contains(a.Update.Repo, "/") || !strings.HasPrefix(a.Update.APIBase, "http") {
		return fmt.Errorf("UPDATE_REPO must be owner/name and UPDATE_API_URL an http(s) URL")
	}
	if a.CronInterval <= 0 || a.StatsInterval <= 0 || a.MetricsRetention <= 0 {
		return fmt.Errorf("CRON_INTERVAL, STATS_INTERVAL, METRICS_RETENTION: must be positive")
	}
	if a.Shots.Interval <= 0 || a.Shots.Timeout <= 0 {
		return fmt.Errorf("SHOTS_INTERVAL, SHOTS_TIMEOUT: must be positive")
	}
	if a.Edge.StaleFor <= 0 {
		return fmt.Errorf("EDGE_STALE_FOR: must be positive")
	}
	if a.Warm.Interval <= 0 || a.Warm.Timeout <= 0 || a.Warm.MaxURLs <= 0 {
		return fmt.Errorf("WARM_INTERVAL, WARM_TIMEOUT, WARM_MAX_URLS: must be positive")
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
