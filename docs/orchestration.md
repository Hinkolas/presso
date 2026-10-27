# ORCHESTRATION — Running many WordPress sites on shared infrastructure

Status: technical design notes, including historical proposals. Shipped features
are listed in [README.md](../README.md); current planned work is in
[ROADMAP.md](../ROADMAP.md). Backups, managed site-software updates, snapshots,
and automatic rollback described below are not implemented. All pre-1.0
releases are experimental and are not recommended for production workloads.

## Goal

A UI and management engine that provisions, runs, updates, backs up and monitors
many WordPress sites efficiently on one host first, multiple hosts later. Sites
are isolated from each other. Operators get the diagnostics that predict trouble
(autoloaded option size, postmeta growth, slow queries, error rates).

## Substrate and stack

Docker Engine on Linux. The engine talks to the Docker API directly, not Compose
files. Containers on Linux are native processes, so there is no file system
penalty (that is a macOS-only problem, see harness.md).

Engine: Go. Single binary, uses the official Docker Go client against the
daemon socket, exposes an HTTP/JSON API consumed by the UI and by the harness
CLI. Keeps its durable state (site model, credentials, tokens, jobs, backups
index, metrics rollups) in SQLite. Decided: single-host scope for now; one
engine per host, one SQLite file. Postgres only if multi-host ever needs a
shared store. Consumes telemetry/events from Valkey. Runs the
cron runner and the update/snapshot/rollback jobs as internal workers.

UI: SvelteKit, talking only to the engine's API. No direct Docker or
database access from the UI.

Harness CLI (harness.md) is a thin client of the same engine API / the same
Go packages, so local and production provisioning share code.

## Topology (single host)

| Component            | Cardinality  | Notes                                             |
|----------------------|--------------|---------------------------------------------------|
| Edge proxy           | 1 per host   | TLS, hostname routing, full-page cache, rate limit|
| Site container       | 1 per site   | Runtime image (FrankenPHP), see runtime.md        |
| Valkey                | shared       | One container, one ACL user per site              |
| MariaDB              | shared       | One container, one database + user per site       |
| Cron runner          | 1 per host   | Invokes WP-CLI cron in each site container        |
| Management engine    | 1 per host   | Provisioning, updates, backups, metrics, API      |
| UI                   | 1            | Talks to the engine's API                         |

```
                    internet
                       |
              +--------+--------+
              |   edge proxy    |   the ONLY container publishing :80 :443
              |  TLS, hostname  |   Caddy (admin API for live route changes)
              |  routing, page  |   or nginx (stronger fastcgi/proxy cache)
              |  cache, limits  |   no PHP inside
              +--------+--------+
                       |  internal docker network, plain HTTP
     +---------+-------+-------+---------+
     |         |               |         |
+----+---+ +---+----+     +----+---+ +---+-----+
| site-a | | site-b |     | site-n | | engine  |  docker socket,
|Franken | |Franken | ... |Franken | | + UI    |  provisions everything
| PHP    | | PHP    |     | PHP    | +---------+
+----+---+ +---+----+     +----+---+
     |         |               |
     +---------+-------+-------+
                       |
          +------------+------------+
          |                         |
     +----+----+              +-----+----+
     | mariadb |              |  valkey  |
     | shared  |              |  shared  |
     +---------+              +----------+

     + cron runner: docker exec into each site container, runs wp cron
```

Request flow:
1. Client connects to the edge. TLS terminates here.
2. Edge checks the page cache. Hit: respond, no PHP runs.
3. Miss: route by Host header to the site container over plain HTTP.
4. Site container's FrankenPHP serves the static file or runs PHP, using its
   own MySQL database and Valkey ACL user over the internal network.
5. Edge stores the response in the page cache if cookies/path rules allow.

Two web servers per request on a miss (edge Caddy, then the Caddy embedded in
FrankenPHP) is deliberate. The site container is self-contained and can run
without the edge (this is what HARNESS does). The edge never touches site files
and only needs a hostname to upstream map.

Edge configuration is a per-site route (domains, upstream container:port) plus
global TLS, cache, and rate-limit policy. The engine adds/removes routes at
runtime via the proxy's API; no restart.

Networks: one internal network is sufficient. Site containers publish no
ports and are isolated from each other by credentials (DB user grants, Valkey
ACL key patterns), not by network. Per-site networks are possible later but
hit Docker's default address pool limits after a few dozen.

Sharing Valkey and MariaDB saves memory (buffer pool, process overhead).
Isolation is by credentials: separate DB users with grants scoped to their
database, separate Valkey ACL users restricted to a per-site key prefix.
Trade-off accepted: a compromise of the shared database server itself affects
all sites; per-site PHP containers cannot reach other sites' data through
WordPress-level exploits.

Shared services (decided, details in runtime.md sections 4-6, 14):
- MariaDB: one instance, utf8mb4, per-site db + user, slow query log on,
  buffer pool sized for the host.
- Valkey: one instance, ACL user per site (`~site-<id>:*`), maxmemory with
  volatile-lru so non-TTL data (telemetry, events) is never evicted. Cache and
  fire-and-forget messaging only; the engine keeps its own durable database.
- Edge page cache: see "Edge page cache" below.

## MVP status (September 2026)

Implemented in this repo (see README.md for the dev loop):

- Engine on the host (`pressod`), embedded web UI, SQLite state, jobs runner
  with live progress over SSE.
- Platform reconciler: docker network, MariaDB, Valkey and the edge container
  are created and kept running by the engine; per-site database/user and
  Valkey ACL user provisioning; ACLs re-applied every minute (Valkey keeps them
  in memory only).
- Sites: create (provision job: image, db, valkey, volume, container, health,
  `wp core install`, edge route, warm), start/stop/restart/recreate/retry,
  delete (typed-slug confirm, optional volume keep), settings with automatic
  recreate, WP-CLI runner, one-click admin login link, cache purge.
- Edge: Caddy + Souin page cache, `.localhost` sites on plain HTTP, real
  domains on automatic HTTPS, surrogate-key purges from the system plugin.
- Observability: Docker events drive site status, container stats and
  edge access-log rollups per minute, live log tail, system status page.
- Cron runner (`wp cron event run --due-now` per running site).
- Page previews: a shared headless Chrome container (`presso-browser`)
  captures the front page of every site (after provision/start/recreate, on
  demand, and every `SHOTS_INTERVAL`); the JPEG is stored under
  `DATA_DIR/screenshots` and served by `GET /v1/sites/{id}/screenshot`. The
  browser runs with `--host-resolver-rules=MAP * presso-edge`, so it visits a
  site's real URL (`demo.localhost`, a public domain) and every hostname
  resolves to the edge container on the presso network: correct Host header
  and SNI, no dependence on public DNS, and the preview shows what visitors
  get through the page cache.

- Install and update: `install.sh` (Docker, binary, systemd unit, first
  admin), tag-driven releases, self-update from the UI with an automatic
  runtime rollout; see "Release and update" below. Rollback of the engine is
  manual (`pressod.previous`, `install.sh --version`).

Deferred features and current priorities are tracked in [ROADMAP.md](../ROADMAP.md).

Edge cache notes learned while building (Souin `cache-handler`):

- The module splits `Surrogate-Key` on commas; the system plugin emits
  `k1, k2, k3`, not the space-separated Fastly form.
- `api { ... }` is only accepted in the global block and never reaches the
  route handlers, so the engine adapts the Caddyfile to JSON and injects the
  API configuration into the cache handler of the loopback-only `:2020`
  server (`internal/edge`). Purges are `PURGE /-/cache/souin` with a
  `Surrogate-Key` header (by tag) or `PURGE /-/cache/souin/<regex>` (by key,
  used for whole-host purges).
- `PURGE /-/cache/souin/flush` breaks the Badger store until the edge
  restarts (insertion errors); the engine never calls it.
- Every edge restart serves the safe bootstrap until reconciliation. Configuration
  generations are compared against the full desired render, with dirty pushes retried.

## Release and update

One version string names everything. A tag `vX.Y.Z` produces the `pressod`
binary (GitHub release), the platform images
`ghcr.io/hinkolas/presso-{edge,mariadb,valkey,browser}:vX.Y.Z` (`build/`) and
`ghcr.io/hinkolas/presso-runtime:vX.Y.Z-php<php>-wp<wp>` for every pair in
the compat build policy; the release is a draft until all images exist. The
engine renders `{release}` in `EDGE_IMAGE`, `MARIADB_IMAGE`, `VALKEY_IMAGE`,
`SHOTS_IMAGE` and `RUNTIME_IMAGE_TEMPLATE` from its own `version.Version`, so
there are no moving tags, no digests to track and no force-pulls: a host on
v0.1.0 runs v0.1.0 images, and only that. Dev builds (`v0.0.0-dev`) have no
release and must point these settings at locally built images; the config
refuses to start otherwise.

The platform images are built from Alpine (MariaDB, Valkey) and Debian stable
(Chromium's headless shell) packages rather than mirrored, so presso decides
when a fix ships and the source pipeline covers them. Each new release
therefore recreates the shared containers once: MariaDB restarts (a few
seconds of database downtime, with `MARIADB_AUTO_UPGRADE` handling a server
version change), Valkey starts empty and gets its ACLs back on the next
reconcile, and the browser restarts.

Install (`install.sh`): host binary under systemd, `/etc/presso/presso.env`
with a generated `AUTH_SECRET`, `/var/lib/presso` for state, Docker via
get.docker.com when missing, first admin via `pressod user create
--password-stdin`. With `PANEL_DOMAIN` the edge gets an HTTPS vhost proxying
to `https://host.docker.internal:7081` with verified TLS and the edge's client
certificate. Local panel access uses `127.0.0.1:7080`; site callbacks use HTTPS
and per-site bearer tokens. The engine accepts forwarded IP headers only from
certificate-authenticated edge requests. Administration and DevTools have no
Docker port publications and are reached through Docker exec/attach.


Update (`internal/update`): the checker reads the 30 most recent releases
(`/repos/<repo>/releases`) every `UPDATE_CHECK_INTERVAL` and keeps the
highest release tag of the admin's channel: stable skips alpha/beta/rc
releases, beta offers them as well as stable ones. Drafts and tags that are
not engine releases (the `sources-<version>` prereleases) are skipped. The
channel and automatic updates (off by default) are set under Settings →
Updates (`PUT /v1/system/update/settings`) and kept in `platform_kv`; before
they are first saved, the channel is `UPDATE_CHANNEL` (written by
`install.sh --channel`) or the running build's. A failed check keeps the last
known release and reports an error kind (offline, not_found, rate_limited,
unavailable, invalid, no_release). Switching from beta back to stable never
downgrades: the status reports `ahead` until a newer stable release exists.
The result is persisted with its channel in `platform_kv` and published as
`system.update`. With automatic updates on, the background check starts the
update itself, once per release: a failed automatic update is not retried
for the same release. `UPDATE_CHECK=false` stops the background check and
automatic updates; checking and updating by hand still work.
`POST /v1/system/actions/update` enqueues the
global `engine.update` job: check → drain (the runner stops accepting site
jobs and waits for running ones, since a restart fails unfinished jobs) →
download + SHA-256 verify + `pressod version` probe → best-effort pre-pull of
the new platform and runtime images → atomic swap next to the current binary
(`pressod.previous` kept) → restart. Once the job row is final the process
shuts down gracefully and `syscall.Exec`s the new binary (same PID; systemd
`Restart=always` is the safety net). On boot the new binary compares
`platform_kv engine.version`, records itself and enqueues `engine.rollout`:
site submissions use the same serialization as settings and lifecycle commands.
A rollout waits for each child job and fails when any application fails or cannot
be queued. Its separate global worker allows this with one site worker. Explicit
retries skip only inspected, successfully applied running sites; stopped sites
remain deferred until Start. Interrupted applications are failed and gated before
reconciliation. No interrupted runtime is automatically downgraded or republished.

Sites persist `desired_state`, `traffic_ready`, `config_revision`,
`applied_revision`, and `applied_image`. `image` remains the desired image.
Provisioning, deliberate stops, deletion, and incomplete changes bypass both the
page cache and upstream and return 503 with `Cache-Control: no-store`. Removal
of the record eventually restores the unknown-host response. Unexpected outages
of a previously validated running site retain stale-cache behavior. Stop and
replacement require a successful unavailable-route push and purge first; after
installation or upgrade, WordPress bootstrap validation and cache invalidation
must succeed before traffic is enabled. Screenshots and warming are best effort.
Settings saved while stopped remain pending until Start applies the full spec.


Not automated: rollback. Migrations are forward-only, so downgrading is safe
only across releases that added none; `install.sh --version vX.Y.Z` or the
kept `pressod.previous` are the manual path.

## Edge page cache

Implementation: prototype Caddy's cache module against a written test list
(bypass rules, tag purge, stale serving, memory with ~100 sites, behavior
across restart). Fallback: Varnish behind the edge Caddy (strongest cache and
tag tooling, no TLS, extra process). nginx proxy_cache rejected: mature but
tag purge is not native.

Policy (decided):
- Storage: persistent (disk or Valkey backed), never memory-only, so an edge
  restart does not cause a cold-start storm across all sites. Per-site size
  quota so one site cannot evict the others.
- stale-if-error ON for every site: the system plugin's Cache-Control always
  carries it, so a site with crashing PHP keeps serving cached pages.
  stale-while-revalidate is a per-site opt-in (`cache_swr` setting): the
  plugin adds the directive and the edge then serves an expired page instantly
  while it refreshes in the background. Both windows are the edge's `stale`
  (EDGE_STALE_FOR, 24h), for which expired entries stay in the store. Souin
  decides both from the cached response's own Cache-Control, so the plugin,
  not the edge config, is where they are switched.
- Cache key: scheme + host + path + normalized query (tracking params such as
  utm_*, fbclid, gclid stripped; remaining params sorted). Compression
  variants stored separately or compressed at the edge; site containers do not
  compress. Vary on device class only if the site opts in. Cookies cause
  bypass, never a variant.
- Hard bypass: non-GET/HEAD; wp-admin, wp-login, wp-cron, xmlrpc, wp-json
  (REST GET may be cached with tags later); preview params; cookies
  wordpress_logged_in_*, wp-postpass_*, comment_author_*, woocommerce_*.
  Everything finer-grained comes from the site via Cache-Control (system
  plugin).
- Default TTL 12h (below nonce lifetime); per site via the `cache_ttl`
  setting, which reaches the plugin as PRESSO_CACHE_TTL and is emitted as
  s-maxage. TTL and SWR changes recreate the container: the header is the
  single source of truth and must not lag the setting.
- Static assets cached too, long TTL for versioned URLs, tagged so a theme /
  plugin update purges them.
- Purge API: by tag, by URL, whole site. Per-site token. Tags carry a site
  prefix and the endpoint enforces it so one site cannot purge another.
- Observability: cache status header (HIT/MISS/BYPASS/STALE) on every
  response; per-site hit ratio in the engine.
- Cache warmer (`internal/warm`), per-site opt-in via the `cache_warm`
  setting: the engine fetches the front page plus every sitemap URL through
  the edge (robots.txt `Sitemap:`, then /wp-sitemap.xml, /sitemap_index.xml,
  /sitemap.xml; same host only, capped by WARM_MAX_URLS) every WARM_INTERVAL,
  staggered per site, and 20s after any purge (debounced, one follow-up if
  triggered mid-run). Requests use the visitor scheme and primary host so
  they hit the same cache key; alias hosts are not warmed. Traffic carries
  the `presso-warmer/1` User-Agent and is dropped from request metrics. A
  still-cached page is an edge hit, so a walk only costs origin renders for
  pages that expired or were purged. Sitemap responses carry the `:archive`
  tag so publishing purges them and new URLs are seen on the next walk.

Per-site memory budget (idle, low traffic): roughly 80–150 MB for PHP plus a
negligible share of Valkey/MySQL. A 32 GB host should hold well over a hundred
small sites.

## Proposed engine responsibilities

- Provision: create DB + user, Valkey ACL, volumes, container, proxy route, TLS.
- Configure: generate wp-config from a site model (constants, salts, cache,
  cron off, debug off, file mods policy).
- Update: swap runtime image tag (core/PHP), run db upgrade via WP-CLI, roll
  back on failure. Plugin/theme/language updates via WP-CLI as `ops` user, on
  a schedule or on demand, always as snapshot -> update -> smoke test ->
  auto-rollback. Sites are LOCKED (no admin file mods) by default; per-site
  flag opens them (runtime.md section 8).
- Install: plugin/theme search + install and zip upload in the UI, executed
  via WP-CLI in the site container.
- Backup: db dump + wp-content volume snapshot, per site, retained per policy.
- Cron: run wp cron events for each site on a schedule; report failures.
- Cache: purge full-page cache (by tag or whole site) and object cache per
  site on demand and on deploy. Content-driven purges come from the system
  plugin directly; the engine issues a site purge after core/plugin updates.
- System plugin integration: issue per-site purge tokens and login tokens,
  consume telemetry and events from Valkey, set module toggles via env.
- Guard: warn or refuse installation of known page-cache plugins that
  conflict with the platform cache.
- Metrics: per-site request rate, p95 latency, PHP errors, slow queries,
  autoloaded option bytes, postmeta row count, transient count, disk usage.
- Access: ephemeral WP-CLI shell, one-click admin login, log tailing.
- Resource limits: CPU and memory caps per container.

## Site settings model

Every important knob is configurable in the UI without touching files. Settings
are layered: platform defaults -> size class defaults -> per-site overrides.
The engine stores them in SQLite, renders them into env vars / mounted config
for the site container, and applies them (most PHP ini changes need a container
recreate; per-site containers restart in a second or two, done gracefully
behind an unavailable route: deliberate changes return 503 with no-store;
stale-cache behavior is reserved for unexpected runtime outages).

Settings surface (initial list):
- Runtime: PHP version (image tag), WordPress version (image tag), size class.
- PHP: memory_limit, upload_max_filesize, post_max_size, max_execution_time,
  max_input_vars (page builders need 3000+), max_input_time, timezone,
  OPcache memory, thread count.
- WordPress constants: WP_MEMORY_LIMIT, WP_MAX_MEMORY_LIMIT,
  WP_POST_REVISIONS, AUTOSAVE_INTERVAL, WP_DEBUG flags (dev), table prefix
  (read-only after creation), DB_CHARSET/DB_COLLATE (see migration).
- Policy: file modification (locked/open), two-user hardening escape hatch,
  XML-RPC on/off, REST for anonymous on/off, heartbeat interval, outbound
  HTTP allowlist, cron interval.
- Cache: page cache on/off, TTL, stale-while-revalidate on/off, cache warmer
  on/off (all shipped); per-site bypass paths, object cache on/off,
  device-class vary.
- Domains: primary, aliases, redirects, staging flag (see Domains).
- Mail: relay credentials, from address.
- Limits: container CPU/memory caps, upload storage quota.
- Escape hatches: extra php.ini lines, extra wp-config constants, extra
  Caddyfile snippet for the site container. Shown with a warning; these are
  what makes "run basically every site" possible.

## Engine tools

Not all MVP. Rough order of value:
1. One-click admin login (system plugin token) and ephemeral WP-CLI shell.
2. Maintenance mode outside WordPress: engine flips a flag, the edge serves a
   static maintenance page for that site regardless of container state.
   Bypass for operators via IP or a signed cookie so the site can be checked
   while "down". Used automatically during migrations, domain changes and
   risky updates.
3. Update with rollback: snapshot (db dump + wp-content volume snapshot) ->
   update -> smoke test (home 200, admin 200, no new PHP fatals, key URLs)
   -> auto-rollback on failure. Same machinery for core image tag changes.
4. Log viewer: PHP errors, access, slow queries, cron output, per site.
5. Domain operations (see Domains).
6. Staging clone: copy of a site under a staging domain with URL rewrite,
   noindex and optional edge password. Promote = the clone replaces prod
   (swap + domain rewrite). Merging database changes back is not solvable in
   general and is not offered; code-only sync (plugins/themes) is.
7. Backup system: scheduled snapshots per site, retention policy, restore to
   same or new site, off-host copy to object storage. Not critical for MVP;
   the snapshot primitive from (3) is the foundation.
8. Integrity: core is immutable, so `wp plugin verify-checksums` and a
   wp-content scan for PHP files in uploads cover most of what "malware
   scanners" do.
9. Health/uptime checks per site, TLS expiry, disk usage, the DB hygiene
   metrics (autoload bytes, postmeta rows, transients, revisions).
10. Redirect management at the edge (old paths, old domains).
11. Search-replace tool (thin UI over `wp search-replace` with dry run).

## Compatibility and migration

Goal: run basically every existing WordPress site, either by giving it what it
needs or by migrating it.

WordPress and PHP versions (implemented, September 2026):
- A site chooses a WordPress release; PHP defaults to the newest version that
  release supports according to wordpress.org. The matrix lives in
  `internal/compat/matrix.json` (one entry per branch, 5.6 to the current
  release, pinned to the newest patch) and drives validation, the UI's
  version picker (`GET /v1/system/runtime`) and the CI build matrix
  (`pressod runtime-matrix`).
- Runtime images are pre-built by CI (`.github/workflows/runtime.yml`) and
  pushed to a registry; the engine only pulls. Which (PHP, WordPress) pairs
  exist is the matrix's `build` policy: currently one branch per WordPress
  major (5.9, 6.9, 7.1) on its newest supported PHP, three images. The engine
  offers exactly that set, so the registry always has what a site can
  request; widening the policy in `matrix.json` widens both. Tags are
  `php<x.y>-wp<release>`, e.g. `php8.5-wp6.9.7`, under one repository for
  both flavors (`RUNTIME_IMAGE_TEMPLATE`). The tag always carries the
  branch's pinned release, so a site on an older patch moves to the pinned
  one when its container is recreated.
- FrankenPHP requires PHP >= 8.2, which WordPress 6.1 introduced support for.
  Older releases (5.6-6.0) and any site an operator pins to PHP 7.4 / 8.0 /
  8.1 run on the **legacy** runtime: the nginx + PHP-FPM build of the same
  image layout (runtime.md). Same volume layout, same env contract, same
  system plugin; only the execution layer differs. Sites on it are marked
  "Legacy" everywhere in the UI. `RUNTIME_LEGACY_ENABLED=false` removes the
  legacy PHP versions and the WordPress releases that need them from the
  offer and rejects them in the API. The legacy runtime is currently disabled
  and unpublished (no upstream fix for CVE-2026-109618/109619 on PHP <= 8.1;
  see README.md "Legacy runtimes").
- The PHP version can be changed later in settings, limited to what the
  site's WordPress release supports; moving across the 8.1/8.2 boundary swaps
  the runtime with the image. EOL PHP versions are allowed; the persistent
  warning and per-site "upgrade readiness" report are still to do.
- Upgrade readiness: PHP compatibility scan of plugins/themes (PHPCompatibility
  via WP-CLI or the migration plugin), plus a trial run in a staging clone on
  the target PHP version with fatals captured.

Charsets and database quirks:
- Preserve the source DB_CHARSET / DB_COLLATE and table prefix on import; do
  not silently convert.
- Offer conversion to utf8mb4 as an explicit step with a dry run. Detect
  double-encoded data (latin1 columns holding utf8 bytes) by mojibake
  heuristics before converting and let the operator choose.
- Convert MyISAM tables to InnoDB (safe, WordPress handles it).
- sql_mode: WordPress strips its known-incompatible modes itself; MariaDB
  strictness is per-session so a per-site override exists in settings if an
  old plugin depends on lenient modes.
- Multisite: out of scope for MVP; flagged during migration inventory.

Migration sources, in priority order:
1. Push via migration plugin (primary; works on shared hosting without SSH).
   Operator installs the plugin on the old site, pastes a one-time token from
   the engine. The plugin: inventories (WP/PHP version, extensions in use,
   plugins/themes, prefix, charset, sizes, multisite), streams a DB dump and
   wp-content to the engine in resumable chunks, and reports progress.
   Engine: provisions a site with matching PHP version, imports, runs the
   domain rewrite (see Domains) to a staging domain, runs checks, and hands
   the operator a staging URL. Cutover is then a domain operation.
   This plugin is separate from the system plugin (it runs on foreign hosts,
   must work on old PHP, and is uninstalled after).
2. Pull via SSH (rsync + mysqldump / remote WP-CLI) for hosts that allow it.
3. Import from archives: All-in-One WP Migration, Duplicator, UpdraftPlus,
   plain db dump + wp-content zip. Later.

Post-import checks: WP_DEBUG logging on in the staging clone, capture PHP
fatals/deprecations, list plugins failing on the target PHP, verify uploads
paths, check for hardcoded absolute paths (old server's filesystem paths in
options/uploads), flush rewrite rules, regenerate builder CSS, purge caches.

## Domains

The engine owns a site's domains: one primary, any number of aliases (served
as-is or 301 to primary), a staging flag. Changing the primary domain is a
first-class engine operation, never a manual search-replace.

Why moving a WordPress site between domains breaks: the domain is stored in
many places, not just siteurl/home in wp_options.
- wp_options siteurl/home (the obvious two).
- Absolute URLs in post_content (images, links) and in block attributes
  (JSON with escaped slashes: `http:\/\/old`).
- PHP-serialized data in options, postmeta, usermeta, termmeta (page builders,
  widgets, customizer, theme settings). A naive text replace corrupts
  serialized string lengths.
- wp_posts.guid (should not be rewritten per WP guidance; skipped).
- Generated files under wp-content/uploads (page builder CSS with absolute
  URLs, cached CSS/JS from optimizer plugins).
- Transients and object cache entries, page cache entries.
- wp-config constants and .htaccess on the old host (not carried over: our
  wp-config is generated).
- Hardcoded URLs in theme files (reported, not auto-fixed).
- Scheme changes (http -> https) on top of host changes.

How the platform handles it:
- WP_HOME and WP_SITEURL are set from env in the generated wp-config, so the
  engine's primary domain is authoritative for core routing regardless of
  what the database says. General Settings shows them read-only.
- Domain change operation: maintenance mode -> snapshot -> `wp search-replace`
  for every variant of the old origin (https://old, http://old, //old,
  escaped `https:\/\/old`, url-encoded form) with `--all-tables --precise
  --skip-columns=guid` (handles PHP serialization correctly) -> regenerate
  builder assets and flush rewrite rules -> flush object cache, purge page
  cache -> update edge route, issue TLS for the new domain -> 301 the old
  domain to the new one -> exit maintenance -> report changed-row counts.
- Safety net in the system plugin: an output filter rewriting any remaining
  occurrences of known former domains to the current primary on the fly
  (for content the search-replace could not reach, e.g. odd encodings).
  Logged so leftovers can be fixed properly.
- Staging domains: staging flag sets noindex (X-Robots-Tag at the edge plus
  blog_public), optional edge password, and mail redirected to the catcher
  or a sink so staging never emails customers.
- Local (HARNESS): sites run as `<slug>.localhost`; the same domain operation
  is what turns an imported production site into a local one.

## Later (explicitly out of scope for now)

Scope decision: single host. Everything above assumes one machine running the
edge, all site containers, MariaDB, Valkey and the engine. Multi-host is a
later phase and should not shape the current design beyond keeping the site
model host-agnostic (a site is a record; its host is an attribute).

- Multi-host placement and migration of sites between hosts.
- Object storage offload for uploads and backups.
- Staging clones of a site with URL rewrite.
- Whether to adopt an existing scheduler (Nomad, Swarm, Kubernetes) once
  multi-host is needed. Not before.
