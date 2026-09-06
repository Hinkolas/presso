// Package platform reconciles the shared infrastructure every site depends
// on: the docker network, the MariaDB and Redis containers and the Caddy
// edge. pressod runs the reconciler on boot and periodically, so a fresh
// host needs nothing but Docker and the engine binary, and a container that
// disappears is recreated. Volumes persist across recreates.
//
// Platform-level secrets (MariaDB root password, Redis default password) are
// generated once and stored encrypted in platform_kv. Per-site database and
// Redis ACL provisioning lives here too because it needs those credentials.
package platform

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql" // registers the "mysql" driver
	"github.com/redis/go-redis/v9"

	"github.com/Hinkolas/presso/internal/config"
	"github.com/Hinkolas/presso/internal/docker"
	"github.com/Hinkolas/presso/internal/edge"
	"github.com/Hinkolas/presso/internal/events"
	"github.com/Hinkolas/presso/internal/secrets"
	"github.com/Hinkolas/presso/internal/store"
	"github.com/Hinkolas/presso/internal/utils"
)

// Container names are fixed: the engine finds them by name after a restart.
const (
	NameMariaDB = "presso-mariadb"
	NameRedis   = "presso-redis"
	NameEdge    = "presso-edge"

	labelHash = "presso.config_hash"

	kvMariaDBRoot     = "mariadb.root_password"
	kvRedisPassword   = "redis.default_password"
	kvEdgeLastApplied = "edge.last_applied_at"
	kvEdgeLastError   = "edge.last_error"
)

// Component is the observed state of one shared piece.
type Component struct {
	Name   string `json:"name"`
	State  string `json:"state"`
	Health string `json:"health"`
	Image  string `json:"image"`
	Error  string `json:"error,omitempty"`
}

// EdgeRenderer returns the full Caddyfile for the current site list. It is
// injected by internal/site to avoid an import cycle.
type EdgeRenderer func(ctx context.Context) (string, error)

type Service struct {
	cfg  *config.API
	dk   docker.Client
	st   *store.Store
	box  *secrets.Box
	hub  *events.Hub
	edge *edge.Client

	renderEdge EdgeRenderer

	mu         sync.Mutex
	components []Component
	db         *sql.DB
	rdb        *redis.Client
	startedAt  time.Time
}

func New(cfg *config.API, dk docker.Client, st *store.Store, box *secrets.Box, hub *events.Hub, edgeClient *edge.Client) *Service {
	return &Service{cfg: cfg, dk: dk, st: st, box: box, hub: hub, edge: edgeClient, startedAt: time.Now()}
}

// SetEdgeRenderer wires the site list into the reconciler's edge config push.
func (s *Service) SetEdgeRenderer(r EdgeRenderer) { s.renderEdge = r }

// Uptime of this engine process.
func (s *Service) Uptime() time.Duration { return time.Since(s.startedAt) }

// Run reconciles now and then every interval until ctx ends.
func (s *Service) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := s.Reconcile(ctx); err != nil && ctx.Err() == nil {
			slog.WarnContext(ctx, "platform: reconcile", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Reconcile ensures every shared component exists and runs, re-applies the
// Redis ACLs and pushes the edge config when the edge is not carrying ours.
// Errors on one component are recorded on it and do not stop the others.
func (s *Service) Reconcile(ctx context.Context) error {
	var comps []Component
	var errs []error
	record := func(name string, info *docker.ContainerInfo, image string, err error) {
		c := Component{Name: name, Image: image, State: "missing", Health: "none"}
		if info != nil {
			c.State, c.Health, c.Image = info.State, info.Health, info.Image
		}
		if err != nil {
			c.Error = err.Error()
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
		}
		comps = append(comps, c)
	}

	if _, err := s.dk.EnsureNetwork(ctx, s.cfg.Network, managedLabels("network")); err != nil {
		record("network", nil, "", err)
	} else {
		record("network", &docker.ContainerInfo{State: "ready", Health: "none"}, "", nil)
	}

	if s.cfg.Managed {
		spec, err := s.mariadbSpec(ctx)
		var info *docker.ContainerInfo
		if err == nil {
			info, err = s.ensureContainer(ctx, spec)
		}
		record("mariadb", info, s.cfg.MariaDBImage, err)

		spec, err = s.redisSpec(ctx)
		if err == nil {
			info, err = s.ensureContainer(ctx, spec)
		}
		record("redis", info, s.cfg.RedisImage, err)

		info, err = s.ensureContainer(ctx, s.edgeSpec())
		record("edge", info, s.cfg.Edge.Image, err)
	} else {
		for _, n := range []string{NameMariaDB, NameRedis, NameEdge} {
			info, err := s.dk.InspectContainer(ctx, n)
			if errors.Is(err, docker.ErrNotFound) {
				err = nil
			}
			record(strings.TrimPrefix(n, "presso-"), info, "", err)
		}
	}

	if err := s.applyAllRedisACLs(ctx); err != nil && ctx.Err() == nil {
		errs = append(errs, err)
		slog.WarnContext(ctx, "platform: redis acls", "err", err)
	}
	if err := s.ensureEdgeConfig(ctx, false); err != nil && ctx.Err() == nil {
		errs = append(errs, err)
	}

	s.mu.Lock()
	changed := fmt.Sprint(s.components) != fmt.Sprint(comps)
	s.components = comps
	s.mu.Unlock()
	if changed && s.hub != nil {
		s.hub.Publish(events.TypePlatformStatus, comps)
	}
	return errors.Join(errs...)
}

// Components returns the last observed state.
func (s *Service) Components() []Component {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Component, len(s.components))
	copy(out, s.components)
	return out
}

// --- container specs ---

func managedLabels(kind string) map[string]string {
	return map[string]string{docker.LabelManaged: "true", docker.LabelKind: kind}
}

func splitListen(addr string) (string, int, error) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return "", 0, fmt.Errorf("listen address %q: %w", addr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 {
		return "", 0, fmt.Errorf("listen address %q: bad port", addr)
	}
	return host, port, nil
}

func (s *Service) mariadbSpec(ctx context.Context) (docker.Spec, error) {
	root, err := s.secret(ctx, kvMariaDBRoot)
	if err != nil {
		return docker.Spec{}, err
	}
	hostIP, port, err := splitListen(s.cfg.MariaDBListen)
	if err != nil {
		return docker.Spec{}, err
	}
	return docker.Spec{
		Name:  NameMariaDB,
		Image: s.cfg.MariaDBImage,
		Env: []string{
			"MARIADB_ROOT_PASSWORD=" + root,
			"MARIADB_AUTO_UPGRADE=1",
		},
		Cmd: []string{
			"--character-set-server=utf8mb4",
			"--collation-server=utf8mb4_unicode_ci",
			"--max-connections=500",
			"--skip-name-resolve",
		},
		Labels:         managedLabels("mariadb"),
		Network:        s.cfg.Network,
		NetworkAliases: []string{"mariadb"},
		Mounts:         []docker.Mount{{Type: docker.MountVolume, Source: NameMariaDB + "-data", Target: "/var/lib/mysql"}},
		Ports:          []docker.PortBinding{{HostIP: hostIP, HostPort: port, ContainerPort: 3306}},
		RestartPolicy:  "unless-stopped",
		StopTimeout:    30 * time.Second,
		LogMaxSize:     "20m",
		LogMaxFile:     3,
		Healthcheck: &docker.Healthcheck{
			Test:     []string{"CMD", "healthcheck.sh", "--connect", "--innodb_initialized"},
			Interval: 10 * time.Second, Timeout: 5 * time.Second, Retries: 5, StartPeriod: 60 * time.Second,
		},
	}, nil
}

func (s *Service) redisSpec(ctx context.Context) (docker.Spec, error) {
	pw, err := s.secret(ctx, kvRedisPassword)
	if err != nil {
		return docker.Spec{}, err
	}
	hostIP, port, err := splitListen(s.cfg.RedisListen)
	if err != nil {
		return docker.Spec{}, err
	}
	return docker.Spec{
		Name:  NameRedis,
		Image: s.cfg.RedisImage,
		Cmd: []string{
			"redis-server",
			"--requirepass", pw,
			"--maxmemory", s.cfg.RedisMaxMemory,
			"--maxmemory-policy", "volatile-lru",
			"--save", "",
			"--appendonly", "no",
		},
		Labels:         managedLabels("redis"),
		Network:        s.cfg.Network,
		NetworkAliases: []string{"redis"},
		Ports:          []docker.PortBinding{{HostIP: hostIP, HostPort: port, ContainerPort: 6379}},
		RestartPolicy:  "unless-stopped",
		StopTimeout:    10 * time.Second,
		LogMaxSize:     "10m",
		LogMaxFile:     2,
		Healthcheck: &docker.Healthcheck{
			Test:     []string{"CMD", "redis-cli", "-a", pw, "--no-auth-warning", "ping"},
			Interval: 10 * time.Second, Timeout: 3 * time.Second, Retries: 3, StartPeriod: 10 * time.Second,
		},
	}, nil
}

func (s *Service) edgeSpec() docker.Spec {
	spec := docker.Spec{
		Name:  NameEdge,
		Image: s.cfg.Edge.Image,
		// CADDY_ADMIN makes the bootstrap config's admin API reachable on the
		// published port; --resume brings back the last pushed config after a
		// restart (persisted in /config).
		Env:            []string{"CADDY_ADMIN=0.0.0.0:2019"},
		Cmd:            []string{"caddy", "run", "--resume", "--config", "/etc/caddy/Caddyfile", "--adapter", "caddyfile"},
		Labels:         managedLabels("edge"),
		Network:        s.cfg.Network,
		NetworkAliases: []string{"edge"},
		Mounts: []docker.Mount{
			{Type: docker.MountVolume, Source: NameEdge + "-data", Target: "/data"},
			{Type: docker.MountVolume, Source: NameEdge + "-config", Target: "/config"},
			{Type: docker.MountVolume, Source: NameEdge + "-cache", Target: "/cache"},
		},
		RestartPolicy: "unless-stopped",
		StopTimeout:   15 * time.Second,
		LogMaxSize:    "50m",
		LogMaxFile:    5,
		Healthcheck: &docker.Healthcheck{
			Test:     []string{"CMD", "wget", "-q", "-O", "/dev/null", "http://127.0.0.1:2019/config/"},
			Interval: 15 * time.Second, Timeout: 3 * time.Second, Retries: 3, StartPeriod: 10 * time.Second,
		},
	}
	for _, p := range []struct {
		listen string
		port   int
		proto  string
	}{
		{s.cfg.Edge.HTTPListen, 80, "tcp"},
		{s.cfg.Edge.HTTPSListen, 443, "tcp"},
		{s.cfg.Edge.HTTPSListen, 443, "udp"},
		{s.cfg.Edge.AdminListen, 2019, "tcp"},
		{s.cfg.Edge.CacheAdminListen, 2020, "tcp"},
	} {
		hostIP, port, err := splitListen(p.listen)
		if err != nil {
			continue // validated by config; skip defensively
		}
		spec.Ports = append(spec.Ports, docker.PortBinding{HostIP: hostIP, HostPort: port, ContainerPort: p.port, Protocol: p.proto})
	}
	return spec
}

// specHash identifies a spec; a changed hash means recreate.
func specHash(spec docker.Spec) string {
	var b strings.Builder
	b.WriteString(spec.Image)
	b.WriteString("|")
	b.WriteString(strings.Join(spec.Cmd, "\x00"))
	b.WriteString("|")
	env := append([]string(nil), spec.Env...)
	sort.Strings(env)
	b.WriteString(strings.Join(env, "\x00"))
	b.WriteString("|")
	for _, p := range spec.Ports {
		fmt.Fprintf(&b, "%s:%d:%d/%s;", p.HostIP, p.HostPort, p.ContainerPort, p.Protocol)
	}
	for _, m := range spec.Mounts {
		fmt.Fprintf(&b, "%s:%s:%s;", m.Type, m.Source, m.Target)
	}
	fmt.Fprintf(&b, "|%d|%d|%s", spec.MemoryBytes, spec.NanoCPUs, spec.RestartPolicy)
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:8])
}

// ensureContainer creates, recreates (on spec change) or starts a named
// container and returns its inspected state.
func (s *Service) ensureContainer(ctx context.Context, spec docker.Spec) (*docker.ContainerInfo, error) {
	if spec.Labels == nil {
		spec.Labels = map[string]string{}
	}
	spec.Labels[labelHash] = specHash(spec)

	info, err := s.dk.InspectContainer(ctx, spec.Name)
	switch {
	case err == nil && info.Labels[labelHash] != spec.Labels[labelHash]:
		slog.InfoContext(ctx, "platform: recreating container with changed spec", "name", spec.Name)
		if err := s.dk.RemoveContainer(ctx, info.ID, true); err != nil {
			return info, err
		}
		fallthrough
	case errors.Is(err, docker.ErrNotFound):
		if err := s.dk.EnsureImage(ctx, spec.Image, nil); err != nil {
			return nil, err
		}
		for _, m := range spec.Mounts {
			if m.Type == docker.MountVolume {
				if err := s.dk.EnsureVolume(ctx, m.Source, managedLabels(spec.Labels[docker.LabelKind])); err != nil {
					return nil, err
				}
			}
		}
		id, err := s.dk.CreateContainer(ctx, spec)
		if err != nil {
			return nil, err
		}
		if err := s.dk.StartContainer(ctx, id); err != nil {
			return nil, err
		}
		slog.InfoContext(ctx, "platform: started container", "name", spec.Name, "image", spec.Image)
		return s.dk.InspectContainer(ctx, id)
	case err != nil:
		return nil, err
	}
	if !info.Running() {
		if err := s.dk.StartContainer(ctx, info.ID); err != nil {
			return info, err
		}
		return s.dk.InspectContainer(ctx, info.ID)
	}
	return info, nil
}

// --- secrets in platform_kv ---

// secret returns the stored secret for key, generating it on first use.
func (s *Service) secret(ctx context.Context, key string) (string, error) {
	enc, err := s.st.GetKV(ctx, key)
	if err == nil {
		return s.box.DecryptString(enc)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("platform: read %s: %w", key, err)
	}
	pw, err := utils.RandomToken(24)
	if err != nil {
		return "", err
	}
	enc, err = s.box.EncryptString(pw)
	if err != nil {
		return "", err
	}
	if err := s.st.SetKV(ctx, store.SetKVParams{Key: key, Value: enc, UpdatedAt: time.Now().UTC()}); err != nil {
		return "", fmt.Errorf("platform: store %s: %w", key, err)
	}
	return pw, nil
}

// KV helpers for plain (non-secret) values.
func (s *Service) getKV(ctx context.Context, key string) string {
	v, err := s.st.GetKV(ctx, key)
	if err != nil {
		return ""
	}
	return string(v)
}

func (s *Service) setKV(ctx context.Context, key, value string) {
	_ = s.st.SetKV(ctx, store.SetKVParams{Key: key, Value: []byte(value), UpdatedAt: time.Now().UTC()})
}

// --- MariaDB ---

// ErrNotReady is returned when a shared service is not accepting connections
// yet (it may still be starting).
var ErrNotReady = errors.New("platform: service not ready")

// MariaDB returns a root connection to the shared server, waiting up to wait
// for it to accept connections (it needs a while after a cold start).
func (s *Service) MariaDB(ctx context.Context, wait time.Duration) (*sql.DB, error) {
	s.mu.Lock()
	db := s.db
	s.mu.Unlock()
	if db == nil {
		root, err := s.secret(ctx, kvMariaDBRoot)
		if err != nil {
			return nil, err
		}
		dsn := fmt.Sprintf("root:%s@tcp(%s)/?timeout=5s&readTimeout=60s&writeTimeout=60s&parseTime=true", root, s.cfg.MariaDBListen)
		db, err = sql.Open("mysql", dsn)
		if err != nil {
			return nil, fmt.Errorf("platform: mariadb: %w", err)
		}
		db.SetMaxOpenConns(4)
		db.SetConnMaxIdleTime(time.Minute)
		s.mu.Lock()
		if s.db == nil {
			s.db = db
		} else {
			db.Close()
			db = s.db
		}
		s.mu.Unlock()
	}
	return db, waitReady(ctx, wait, func(ctx context.Context) error { return db.PingContext(ctx) })
}

// Redis returns a client authenticated as the default user.
func (s *Service) Redis(ctx context.Context, wait time.Duration) (*redis.Client, error) {
	s.mu.Lock()
	rdb := s.rdb
	s.mu.Unlock()
	if rdb == nil {
		pw, err := s.secret(ctx, kvRedisPassword)
		if err != nil {
			return nil, err
		}
		rdb = redis.NewClient(&redis.Options{Addr: s.cfg.RedisListen, Password: pw, DialTimeout: 3 * time.Second, MaxRetries: 1})
		s.mu.Lock()
		if s.rdb == nil {
			s.rdb = rdb
		} else {
			rdb.Close()
			rdb = s.rdb
		}
		s.mu.Unlock()
	}
	return rdb, waitReady(ctx, wait, func(ctx context.Context) error { return rdb.Ping(ctx).Err() })
}

func waitReady(ctx context.Context, wait time.Duration, ping func(context.Context) error) error {
	deadline := time.Now().Add(wait)
	var last error
	for {
		pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		last = ping(pctx)
		cancel()
		if last == nil {
			return nil
		}
		if ctx.Err() != nil || time.Now().After(deadline) {
			return fmt.Errorf("%w: %v", ErrNotReady, last)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

var identRe = regexp.MustCompile(`^[a-z0-9_]{1,60}$`)

// CreateSiteDatabase creates the database and a user scoped to it; both
// calls are idempotent so a retried provisioning job can rerun them.
func (s *Service) CreateSiteDatabase(ctx context.Context, dbName, user, password string) error {
	if !identRe.MatchString(dbName) || !identRe.MatchString(user) {
		return fmt.Errorf("platform: invalid database identifier")
	}
	db, err := s.MariaDB(ctx, 90*time.Second)
	if err != nil {
		return err
	}
	stmts := []string{
		fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci", dbName),
		fmt.Sprintf("CREATE USER IF NOT EXISTS '%s'@'%%' IDENTIFIED BY '%s'", user, password),
		fmt.Sprintf("ALTER USER '%s'@'%%' IDENTIFIED BY '%s'", user, password),
		fmt.Sprintf("GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'%%'", dbName, user),
		"FLUSH PRIVILEGES",
	}
	for _, q := range stmts {
		if _, err := db.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("platform: mariadb: %w", err)
		}
	}
	return nil
}

// DropSiteDatabase removes a site's database and user (idempotent).
func (s *Service) DropSiteDatabase(ctx context.Context, dbName, user string) error {
	if !identRe.MatchString(dbName) || !identRe.MatchString(user) {
		return fmt.Errorf("platform: invalid database identifier")
	}
	db, err := s.MariaDB(ctx, 30*time.Second)
	if err != nil {
		return err
	}
	for _, q := range []string{
		fmt.Sprintf("DROP DATABASE IF EXISTS `%s`", dbName),
		fmt.Sprintf("DROP USER IF EXISTS '%s'@'%%'", user),
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("platform: mariadb: %w", err)
		}
	}
	return nil
}

// --- Redis ACLs ---

// ApplyRedisACL creates or updates a site's Redis user, restricted to its
// key prefix and stripped of admin/dangerous commands (no FLUSHDB, no ACL).
func (s *Service) ApplyRedisACL(ctx context.Context, user, password, keyPrefix string) error {
	rdb, err := s.Redis(ctx, 30*time.Second)
	if err != nil {
		return err
	}
	return rdb.Do(ctx, aclSetUserArgs(user, password, keyPrefix)...).Err()
}

// aclSetUserArgs builds the ACL SETUSER command for a site user: scoped to
// its key prefix, no admin or dangerous commands (no FLUSHDB, KEYS, ACL,
// CONFIG). INFO is re-allowed because the object-cache drop-in calls it once
// per connection to read the server version.
func aclSetUserArgs(user, password, keyPrefix string) []any {
	return []any{"ACL", "SETUSER", user, "reset", "on", ">" + password,
		"~" + keyPrefix + "*", "&*", "+@all", "-@dangerous", "-@admin", "+info", "+client|setname", "+client|setinfo"}
}

func (s *Service) DeleteRedisACL(ctx context.Context, user string) error {
	rdb, err := s.Redis(ctx, 15*time.Second)
	if err != nil {
		return err
	}
	return rdb.Do(ctx, "ACL", "DELUSER", user).Err()
}

// applyAllRedisACLs re-applies every site's ACL: Redis keeps them in memory
// only, so a Redis restart would otherwise lock every site out.
func (s *Service) applyAllRedisACLs(ctx context.Context) error {
	sites, err := s.st.ListSites(ctx)
	if err != nil {
		return err
	}
	if len(sites) == 0 {
		return nil
	}
	rdb, err := s.Redis(ctx, 5*time.Second)
	if err != nil {
		return err
	}
	var errs []error
	for _, site := range sites {
		if site.Status == "deleting" {
			continue
		}
		pw, err := s.box.DecryptString(site.RedisPasswordEnc)
		if err != nil {
			errs = append(errs, fmt.Errorf("site %s: %w", site.Slug, err))
			continue
		}
		if err := rdb.Do(ctx, aclSetUserArgs(site.RedisUser, pw, RedisKeyPrefix(site.ID.String()))...).Err(); err != nil {
			errs = append(errs, fmt.Errorf("site %s: %w", site.Slug, err))
		}
	}
	return errors.Join(errs...)
}

// RedisKeyPrefix is the key namespace of a site in the shared Redis.
func RedisKeyPrefix(siteID string) string { return "site-" + siteID + ":" }

// --- edge config ---

// ApplyEdge renders and pushes the edge configuration now. Called by the
// site service after any change that affects routing.
func (s *Service) ApplyEdge(ctx context.Context) error { return s.ensureEdgeConfig(ctx, true) }

func (s *Service) ensureEdgeConfig(ctx context.Context, force bool) error {
	if s.renderEdge == nil || s.edge == nil {
		return nil
	}
	if !force {
		loaded, err := s.edge.Loaded(ctx)
		if err != nil {
			return err
		}
		if loaded {
			return nil
		}
		slog.InfoContext(ctx, "platform: edge is not running our config, pushing")
	}
	caddyfile, err := s.renderEdge(ctx)
	if err != nil {
		return err
	}
	if err := s.edge.Load(ctx, caddyfile); err != nil {
		s.setKV(ctx, kvEdgeLastError, err.Error())
		return err
	}
	s.setKV(ctx, kvEdgeLastError, "")
	s.setKV(ctx, kvEdgeLastApplied, time.Now().UTC().Format(time.RFC3339))
	return nil
}

// EdgeStatus reports the last apply outcome for the system page.
func (s *Service) EdgeStatus(ctx context.Context) (lastApplied *time.Time, lastError string) {
	if v := s.getKV(ctx, kvEdgeLastApplied); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			lastApplied = &t
		}
	}
	return lastApplied, s.getKV(ctx, kvEdgeLastError)
}

// Close releases the admin connections.
func (s *Service) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		s.db.Close()
	}
	if s.rdb != nil {
		s.rdb.Close()
	}
}
