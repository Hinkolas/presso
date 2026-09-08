# RUNTIME — The optimized per-site WordPress runtime

Status: active design. This is the foundation for both HARNESS and ORCHESTRATION.

## Goal

A container image that runs one WordPress site as fast as WordPress can
reasonably go, with production-safe defaults, and that doubles as the local dev
image via a profile switch. Optimizations are applied at the runtime level so
that sites do not depend on caching plugins or tuning by the site owner.

## Why WordPress is slow (summary)

- Shared-nothing PHP lifecycle: the whole app boots on every request. OPcache
  removes compile cost, not execution cost.
- No persistent object cache by default; options, posts, terms, users are
  re-read from MySQL every request.
- Autoloaded wp_options bloat, unserialized on every request.
- Eager plugin loading: every active plugin loads on every request.
- Postmeta EAV schema with unindexed LONGTEXT values; joins and scans.
- Hook fan-out: tens of thousands of callback invocations per request.
- Pseudo-cron runs inside visitor requests via loopback HTTP.
- Outbound HTTP (update checks, license pings) inside page requests.
- admin-ajax and REST pay the full bootstrap for tiny operations.
- Front-end asset weight from globally enqueued CSS/JS.

## Optimization vectors

Each vector lists options and a proposed default. "Decision" is filled in as we
agree. Unmarked items are open.

### 1. PHP execution model
Options:
- PHP-FPM behind nginx. Mature, well understood, per-request process model.
- FrankenPHP classic mode. Caddy + PHP in one binary/process, built-in TLS and
  HTTP/3, threads instead of worker processes. Same per-request semantics as FPM.
- FrankenPHP worker mode / RoadRunner / Swoole. App stays in memory between
  requests. WordPress is not designed for this (global state everywhere);
  experimental, fragile with plugins.
Decision: FrankenPHP classic mode, one instance per site (see "Container
structure" below) for PHP >= 8.2. nginx + PHP-FPM is a first-class second
build of the same image layout for PHP 7.4 / 8.0 / 8.1, because FrankenPHP
does not support those and legacy sites must run (ORCHESTRATION.md,
"Compatibility and migration"). Same volume layout, same env contract, same
system plugin; only the execution layer differs. Worker mode is a research
track, not a dependency.

### 2. PHP version and OPcache
Decision: newest PHP version WordPress fully supports (8.3/8.4 at time of
writing). OPcache settings:
- validate_timestamps ON, revalidate_freq 60 in prod, 0 in dev. Rationale:
  with validation off, plugin updates written by WP-CLI (a separate process)
  are never seen by the web process until restart. On a Linux file system the
  stat cost is negligible; the coordination cost is not. (Revises the earlier
  "off in prod" idea.)
- memory_consumption, max_accelerated_files, interned_strings_buffer set per
  site size class (roughly 128/256/512 MB; file limit well above the PHP
  default since core + WooCommerce alone is thousands of files).
- Telemetry exports hit rate, wasted memory and cache-full state. A full
  OPcache recompiles every request and looks like an unexplained slowdown.
- opcache.preload: skip. WordPress defines functions conditionally and has
  top-level side effects; steady-state gain is a few percent. Instead the
  entrypoint fires one warm-up request against the home page after start.
- opcache.file_cache baked into the image: evaluate. Core sits at the same
  path in every container, so a build step can precompile core into the
  on-disk cache; containers start warm and WP-CLI (with opcache.enable_cli)
  benefits too.
- JIT off. Low single-digit gain for WordPress, history of edge-case bugs.

### 3. Edge / web server
Options: nginx, Caddy (bundled with FrankenPHP), Apache (no).
Concerns: static file serving, TLS, HTTP/2 and 3, brotli/gzip, cache headers
for static assets, request buffering, rate limiting login/xmlrpc, bot filtering.
Decision: two layers. The Caddy embedded in FrankenPHP serves the site itself
(static files + PHP) inside the site container over plain HTTP. A separate
shared edge proxy (owned by ORCHESTRATION) does hostname routing, TLS
termination and the page cache. The site container never terminates TLS in
production and knows nothing about other sites.

### 4. Full-page cache
Options:
- At the shared edge (Caddy cache module / nginx proxy_cache / Varnish).
- Inside the site container. Rejected: cache per site container, no shared
  policy, still boots the container's web server per request.
- WordPress plugin (WP Super Cache, WP Rocket, etc.). Rejected: pays bootstrap
  cost or requires per-site configuration; conflicts with the platform cache.
Decision: page cache at the edge, driven by the system plugin (section 14).
The cache and WordPress communicate in both directions:

- WordPress -> edge, per response: Cache-Control (no-store on anything
  personal: logged-in, cart/checkout/account, previews, responses that set a
  cookie, POST; otherwise `public, s-maxage=<PRESSO_CACHE_TTL>, max-age=0,
  stale-if-error=86400`, plus `stale-while-revalidate=86400` when
  PRESSO_CACHE_SWR=on) and a surrogate-key header listing what the page
  depended on (post IDs in the main query, term IDs, author, home/archive
  flags, a site-wide tag; sitemaps count as archives so publishing purges
  them for the engine's cache warmer).
- WordPress -> edge, on change: purge by tag (post/comment/term changes) or
  purge site (menu, widget, theme, customizer, relevant option changes).
  Called on shutdown, non-blocking, authenticated with a per-site token.
  Failure is logged; TTL is the safety net.
- Edge, hard rules independent of the plugin: bypass on well-known cookies
  (wordpress_logged_in_*, wp-postpass_*, comment_author_*, woocommerce_*),
  never cache POST or wp-admin/wp-login/xmlrpc, strip tracking parameters
  (utm_*, fbclid, gclid, ...) from the cache key.

Correctness details the plugin owns:
- TTL shorter than nonce lifetime (12h default; nonces live 12-24h).
- Disable WooCommerce cart fragments until the cart is non-empty.
- Vary key for themes that branch on wp_is_mobile.
- Search results and 404s cacheable with a short TTL.

Edge cache implementation and policy are owned by ORCHESTRATION.md ("Edge
page cache"). Summary of what the runtime relies on: tag purge with a
site-prefixed tag namespace, stale-while-revalidate and stale-if-error,
persistent store, cache status header on every response. Implementation:
prototype Caddy's cache module, fallback Varnish behind the edge Caddy.

### 5. Object cache
Options:
- Redis with a drop-in (redis-cache plugin's object-cache.php, or Object Cache
  Pro).
- Relay (PHP extension, Redis-backed with in-process cache; fastest, licensing
  to check).
- Memcached. Fine, less tooling.
- APCu in-process. Rejected: WP-CLI and cron processes cannot see or flush it.
Decision: Redis, one shared instance per host (ORCHESTRATION), one ACL user
per site restricted to a key prefix (`~site-<id>:*`). The drop-in sets the
prefix from env. The drop-in is copied into the wp-content volume by the
entrypoint (drop-ins must live in WP_CONTENT_DIR; they cannot be relocated
like mu-plugins). Relay remains an evaluation item, not a dependency.

Redis config: maxmemory cap, eviction policy volatile-lru (only evicts keys
with a TTL) so that non-cache data (telemetry, events) is never silently
dropped. Redis is strictly cache + fire-and-forget messaging; the engine's
durable state lives in its own database.

### 6. Database
Options: MySQL 8 vs MariaDB vs Percona; InnoDB buffer pool sizing; TCP over
docker network vs unix socket (socket only if colocated); persistent
connections; slow query log; schema hygiene (revision limits, transient
cleanup, autoload audit); optional index on postmeta meta_value prefix.
Decision: MariaDB, one shared instance per host (ORCHESTRATION), one database
+ one user per site with grants scoped to that database. utf8mb4. TCP over
the internal network. Slow query log on with a threshold, exported per site
where the engine can attribute it (by db user). InnoDB buffer pool sized
generously since it is shared. Hygiene (revisions, expired transients,
autoload audit) runs as WP-CLI jobs from the cron runner, not in the plugin.
Rationale: WordPress officially supports MariaDB, it is lighter than MySQL 8,
and no WordPress query pattern benefits from MySQL-8-only features.

### 7. WordPress configuration baked into the runtime
- WP_HOME / WP_SITEURL from env. The engine's primary domain is authoritative;
  the database values are not used for routing (ORCHESTRATION.md, "Domains").
- DB_CHARSET / DB_COLLATE / table prefix from env, preserved from the source
  site on migration; never silently converted.
- DISABLE_WP_CRON = true; real cron via WP-CLI.
- WP_POST_REVISIONS limited (e.g. 10); AUTOSAVE_INTERVAL raised.
- DISALLOW_FILE_EDIT = true; DISALLOW_FILE_MODS policy (core is immutable in
  the image; plugin/theme installs via WP-CLI or allowed from admin into the
  wp-content volume — to decide).
- WP_AUTO_UPDATE_CORE = false (runtime image owns core).
- XML-RPC disabled by default; pingbacks off.
- Heartbeat API throttled.
- Emoji detection script and other legacy front-end extras removed.
- Bundled plugins (Akismet, Hello Dolly) and the two inactive default themes
  are not seeded; a new site starts with the one theme core activates.
- "WordPress Events and News" dashboard widget removed.
- Outbound HTTP: block or allowlist during front-end requests; run update
  checks from cron only.
- Performant translations (core 6.5+) on.
- Object cache drop-in and page-cache purge helper installed as must-use
  plugins by the runtime.
Decision: pending (each item individually).

### 8. File system layout and modification policy
Layout:
- Core read-only in the image, versioned by image tag.
- wp-content as a volume; uploads optionally offloaded to object storage later.
- Read-only root file system for the container where possible; writable tmp.

Modification policy. Decision: LOCKED by default, per-site flag to OPEN.
- Locked: DISALLOW_FILE_MODS set for web requests. Admin shows no install /
  update / delete controls for plugins, themes, languages. All code changes go
  through WP-CLI driven by the engine, which enables: snapshot (db + volume),
  update, smoke test, automatic rollback on failure. Premium plugins with
  their own updaters mostly hook the standard update API and work via CLI
  once licensed. Zip uploads go through the engine UI -> `wp plugin install
  file.zip`.
- Open: stock behavior for sites whose owners insist on self-service. Loses
  inventory accuracy and rollback; accepted per site.
- The constant is defined conditionally in the generated wp-config (web
  requests only, e.g. keyed on PHP_SAPI or an env var the engine sets when
  exec'ing WP-CLI), because some WP-CLI commands honor it.
- DISALLOW_FILE_EDIT always on (the in-admin code editor), in both modes.

Two users inside the container:
- `web`: runs FrankenPHP. May write only to wp-content/uploads and
  wp-content/cache (and tmp). Everything else, including plugins/ and
  themes/, is read-only for it.
- `ops`: owns plugins/, themes/, mu-plugins target, languages/. WP-CLI is
  exec'ed as this user by the engine, cron runner and harness.
- Caddy refuses to execute PHP under wp-content/uploads.
Effect: a compromised web request cannot write executable code anywhere.
Breaks the minority of plugins that write into their own directory; per-site
escape hatch makes plugins/ writable by `web` for those sites, at the cost of
the guarantee.

### 9. Background work
- Cron runner invokes WP-CLI per site on an interval; concurrency limit.
- Action Scheduler (WooCommerce) run via CLI, not in web requests.
- Image processing: imagick, thumbnails generated in background where possible.
- Mail via SMTP relay, never sendmail in the container.
Decision: pending.

### 10. Observability
- Per-request timing and query count to structured logs (sampling in prod).
- Sampling profiler (Excimer/Tideways) optional.
- PHP error log, access log, slow query log exported per site.
- Health endpoint for the orchestrator.
Decision: pending.

### 11. Front-end delivery
- Compression, long cache headers for versioned static assets.
- Image conversion to WebP/AVIF at upload or on the fly at the edge.
- Optional CDN in front of the edge.
Decision: pending.

### 12. Abuse and bot load
A large share of load on real WordPress sites is bots hitting wp-login,
xmlrpc, and search/query-string URLs that bypass the page cache. Rate limiting
and cache-key normalization at the edge are performance features, not only
security.
Decision: pending.

### 13. Resource sizing
- PHP worker/thread count, memory_limit, container CPU/memory caps.
- Defaults by site size class (small/medium/large) chosen by the orchestrator.
Decision: pending.

### 14. System plugin (platform integration)
One must-use plugin, shipped in the image, that wires a site into the
platform. This is how managed WordPress hosts work; cache invalidation alone
justifies it.

Location: a read-only path inside the image, selected via the WPMU_PLUGIN_DIR
constant in the generated wp-config. Not in the wp-content volume. Site owners
cannot remove or edit it; it updates with the image tag like core.

Scope rule: the plugin only does things that must happen inside a web
request. Anything doable from outside (inventory, hygiene, upgrades, backups)
goes through WP-CLI driven by the engine. This is the guard against the
plugin becoming a kitchen sink.

Modules (one package, internally modular, toggled by env set by the
orchestrator; no site-owner toggles):
- cache: Cache-Control decisions, surrogate-key header, purge calls
  (section 4).
- enforce: disable XML-RPC and pingbacks, throttle heartbeat, remove emoji
  script and the Events and News dashboard widget, block outbound HTTP during front-end requests (pre_http_request
  allowlist), move update checks to cron, disable cart fragments when empty.
- mail: route wp_mail via SMTP relay from env (phpmailer_init).
- login: one-click login for engine and harness. Engine mints a short-lived
  signed token; plugin validates and creates the session. Application
  passwords cover REST.
- telemetry: sampled per-request timing + query count to structured logs or
  Redis, so the engine can show latency and slow requests per site.
- events: fire-and-forget to Redis (plugin activated, user login, fatal
  error, core upgraded) for the engine's audit log.
- domain: output filter rewriting known former domains to the current primary
  as a safety net after a domain change; logs hits. Staging flag: force
  blog_public=0 and route mail to a sink.

Not part of the system plugin: the migration plugin. It runs on foreign hosts
(old PHP, no platform), pushes inventory + db + wp-content to the engine, and
is removed afterwards. Separate codebase with a minimal PHP floor.

Constants that need no plugin stay in the generated wp-config (section 7):
DISABLE_WP_CRON, WP_POST_REVISIONS, DISALLOW_FILE_EDIT, WP_AUTO_UPDATE_CORE,
WPMU_PLUGIN_DIR, cache/redis credentials.

Risks and mitigations:
- Kitchen sink: scope rule above.
- Conflicts with caching plugins (WP Rocket, W3TC, etc.): engine warns or
  refuses known ones.
- Lock-in: every behavior is additive. A site exported without the plugin
  must still work; it only loses performance and platform features.
- WordPress core changes: plugin is versioned with the image, tested against
  the core version in the same image.

## Container structure

The runtime is one container per site. It is self-contained: it can run alone
(HARNESS) or behind the shared edge proxy (ORCHESTRATION). It comes in two
flavors with the same layout, users, env contract and system plugin; only
the execution layer differs. The flavor is a function of the PHP version:
FrankenPHP for PHP >= 8.2, nginx + PHP-FPM ("fpm", shown as "Legacy" in the
engine and UI) for 7.4 / 8.0 / 8.1. Inside the image it is `PRESSO_FLAVOR`
and the OCI label `presso.runtime.flavor`.

```
+------------------------------------------------------------+
| site container  (runtime image, one per site)              |
|                                                            |
|  presso-entrypoint (root)  volume fixup, flavor hooks,     |
|    then setpriv -> web     exec presso-serve               |
|                                                            |
|  presso-serve (web)                                        |
|    frankenphp: Caddy + embedded PHP, one process           |
|    fpm:        bash supervisor -> php-fpm + nginx          |
|    - listens on :8080 over plain HTTP                      |
|    - serves static files from the web root                 |
|    - runs index.php for everything else (classic mode)     |
|    - OPcache lives here, per container                     |
|                                                            |
|  /var/www/html            core, read-only, from the image  |
|  /var/www/html/wp-content volume (plugins, themes, uploads)|
|  wp-config.php            static, in the image, reads env  |
|  system plugin (mu)       read-only in image, section 14   |
|  object-cache.php         drop-in, copied into volume      |
|  WP-CLI                   for cron, upgrades, harness ops  |
|                                                            |
|  no published ports, no TLS, no knowledge of other sites   |
+------------------------------------------------------------+
        |                              |
        v                              v
   MySQL (site db + user)        Redis (site ACL user)
   provided externally           provided externally
```

Why FrankenPHP per site and not one shared web server:
- FrankenPHP cannot be split; Caddy and PHP are one binary. A shared Caddy
  cannot talk to "just the PHP part" the way nginx talks to PHP-FPM.
- A single FrankenPHP serving all sites would be classic shared hosting: one
  PHP version, no isolation between sites, one restart hits everyone.
- With the site container serving its own static files, the edge proxy never
  needs site files mounted into it and stays a pure hostname router.
- Cost: one extra Go process per site (a few MB) and one plain-HTTP hop on a
  page-cache miss (well under a millisecond). Accepted.

The FPM flavor keeps the same shape on purpose: nginx and php-fpm live in the
site container too, so the edge stays a hostname router and nothing about a
legacy site leaks into the platform layout.

Images are built in two stages (`runtime/`, published by
`.github/workflows/runtime.yml`):

1. One **base** per PHP version, `presso-runtime-base:php{php}`.
   `base.Dockerfile` (FrankenPHP: `dunglas/frankenphp:1-php{php}`) or
   `base-legacy.Dockerfile` (`php:{php}-fpm-{debian}` + nginx). Both hold the
   PHP extensions (mysqli, gd, imagick, intl, zip, exif, bcmath, soap,
   opcache, redis), WP-CLI, the two users (web 1000, ops 1001,
   `scripts/users.sh`), `php/presso.ini`, the web server config and
   `/usr/local/bin/presso-serve`. Multi-arch (amd64 + arm64), rebuilt weekly.
2. One thin **image** per WordPress/PHP pair, `presso-runtime:php{php}-wp{wp}`
   (`Dockerfile`): `wp core download --version` (`scripts/wordpress.sh`, core
   root-owned and read-only), wp-config.php, the system plugin, drop-ins,
   health.php and the entrypoint. The pair list is the compatibility matrix
   in `internal/compat` (`pressod runtime-matrix`).

Per-site differences arrive as environment variables (the env contract in
`internal/site/container.go`): db host/name/user/password/charset/collation/
prefix, redis host/ACL credentials, salts, primary domain (WP_HOME/
WP_SITEURL), profile (prod|dev), file-mod policy, and the PHP ini values the
settings UI exposes (`PHP_MEMORY_LIMIT`, `PHP_UPLOAD_MAX_FILESIZE`,
`PHP_POST_MAX_SIZE`, `PHP_MAX_EXECUTION_TIME`, `PHP_MAX_INPUT_VARS`,
`PHP_OPCACHE_MEMORY`, `PHP_OPCACHE_REVALIDATE_FREQ`, `PHP_TIMEZONE`). Nothing
is rendered per site in the FrankenPHP flavor: the static Caddyfile reads the
variables with inline defaults, and wp-config.php reads `PRESSO_*` directly.
The cache module reads `PRESSO_CACHE` (on|off), `PRESSO_CACHE_TTL` (seconds)
and `PRESSO_CACHE_SWR` (on|off); the engine recreates the container when the
operator changes them, so the emitted Cache-Control never lags the setting.

The FPM flavor renders the same values at boot (`legacy/render.sh`, an
entrypoint hook running as root) into `/run/presso/`: `nginx.conf` (mirrors
the Caddyfile block by block: JSON access log, `/-/ping`, uploads never
execute PHP, wp-config.php/debug.log/dotfiles 404, `client_max_body_size` =
max(upload, post size), fastcgi timeout = max_execution_time + 30),
`php-fpm.conf` (unix socket under `/tmp/presso`, `pm.max_children` from the
container's cgroup memory limit at 64 MiB per worker, `clear_env = no` so the
env contract reaches PHP, `ping.path = /-/fpm-ping`) and `php/site.ini` (the
web-only ini values, loaded through `PHP_INI_SCAN_DIR` for php-fpm alone so
WP-CLI keeps php.ini + presso.ini). `nginx -t` runs at boot and a bad value
fails the container loudly. `legacy/serve.sh` is PID 1 as `web`: TERM turns
into a graceful QUIT for both (under the engine's 10s stop timeout); if either
master dies, the other is stopped and the container exits non-zero so the
restart policy and the engine's health wait see it.

Entrypoint on start (`entrypoint.sh`, identical for both flavors):
- Seed the wp-content volume on first boot (the default theme only, no
  bundled plugins; uploads dir).
- Copy/refresh the object-cache.php drop-in into wp-content (drop-ins cannot
  be relocated; the system plugin can, via WPMU_PLUGIN_DIR).
- Fix ownership per the two-user model (section 8).
- Source `/etc/presso/entrypoint.d/*.sh` (the FPM render step; none for
  FrankenPHP).
- Log `{"presso":"boot", "php", "wp", "flavor", "site"}` and exec
  `presso-serve` as `web` via setpriv.

`health.php` (Docker HEALTHCHECK, PHP 7.4-safe) probes `/-/ping` (the web
server alone), `/-/fpm-ping` in the FPM flavor (php-fpm itself), and `SELECT
1` on the site database when one is configured.

Core upgrade = new image tag. Recreate the container with the same volume, run
`wp core update-db`. Rollback = previous tag. Docker shares image layers across
all containers from the same image, so core exists once on disk regardless of
site count; OPcache is still per container.

## Non-goals (for now)

- Making WordPress a long-running application (worker mode) as a hard
  requirement.
- Replacing MySQL with anything else in production.
- Horizontal scaling of a single site across containers.
