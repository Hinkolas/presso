# presso

presso is an optimized WordPress runtime, an orchestration engine that runs many sites
on one host, and a development harness for AI coding agents. The plans live in
[RUNTIME.md](RUNTIME.md), [ORCHESTRATION.md](ORCHESTRATION.md) and
[HARNESS.md](HARNESS.md).

## Naming

The product is **presso**; its parts carry the prefix: **presso-runtime** (the
per-site image, RUNTIME.md), **presso-engine** (the orchestration engine, this
repo's Go code, binary `pressod`), **presso-web** (the UI), **presso-harness**
(the agent-facing CLI, binary `presso`, not started yet).

## Components

- **pressod** (`cmd/pressod`, Go): the engine. One host process that talks to
  the local Docker Engine: REST API, embedded web UI, SQLite state, the site
  lifecycle jobs, the shared-platform reconciler (MariaDB, Redis, edge), the
  WP cron runner and the observability workers.
- **web** (`web/`, SvelteKit): the UI, built as a static SPA and embedded in
  the `pressod` binary (`internal/webui`). It talks to `/api/v1` on the same
  origin; the session lives in an httpOnly cookie set by the API.
- **runtime** (`runtime/`): the `presso-runtime:php<x.y>-wp<release>` images,
  one container per site (WordPress core + WP-CLI + the `presso` system
  plugin on FrankenPHP for PHP 8.2+, or nginx + PHP-FPM for the legacy
  7.4-8.1 flavor). Which pairs exist is decided by the compatibility matrix in
  `internal/compat`; CI pre-builds them. See RUNTIME.md.
- **edge** (`build/edge.Dockerfile`): `presso-edge`, Caddy with the Souin page
  cache. pressod renders and pushes its whole Caddyfile.
- **browser**: `presso-browser`, a stock headless Chrome
  (`chromedp/headless-shell`) that captures each site's front page for the
  UI previews. Optional (`SHOTS_ENABLED=false`); see `SHOTS_*` in
  `.env.example`.

## Topology (single host)

```
host: pressod (127.0.0.1:7080 API + UI, :7081 internal HTTPS, docker.sock, SQLite)
docker network "presso":
  presso-edge       :80 :443 public; admin/cache on container loopback only
  presso-mariadb    127.0.0.1:3306      one database + user per site
  presso-redis      127.0.0.1:6379      one ACL user + key prefix per site
  presso-browser    no published ports; DevTools on container loopback only
  presso-site-<slug>  no published ports; the edge proxies to site-<id>:8080
```

pressod reaches edge and browser management through streaming Docker exec
connections. MariaDB and Redis publish host-loopback ports. Site containers
send purge callbacks to `https://host.docker.internal:7081`, verify the engine
using their mounted public CA, and authenticate using per-site bearer tokens.
Panel requests on that listener additionally require the edge's client
certificate. The local panel always binds a loopback IP, including without a
panel domain. Forwarding headers are trusted only from the authenticated edge.

Trust lives under `DATA_DIR/control`: keep the CA and keys together. The edge
receives only `control/edge`; sites receive only `control/ca.pem`. Leaf
certificates renew automatically before expiry. Missing or invalid trust files
are an error; restore them rather than replacing the CA. These changes target
fresh installations; no development installation is reset or migrated for you.

## Layout

```
cmd/pressod/          daemon entrypoint + operator subcommands (serve, user, migrate)
internal/api/         HTTP layer: chi router, cookie/bearer auth, handlers, SSE
internal/auth/        operator accounts: argon2id passwords, bearer sessions
internal/config/      env-driven config (go-envconfig), .env for dev only
internal/cron/        wp cron runner (DISABLE_WP_CRON is set in the runtime)
internal/docker/      Docker Engine wrapper (interface + moby client + fake for tests)
internal/edge/        Caddyfile rendering, admin API push, page-cache purges
internal/events/      in-process pub/sub feeding the /v1/events SSE stream
internal/jobs/        durable background jobs with per-site serialization
internal/obs/         slog setup
internal/observe/     status watcher, container stats, edge access-log rollups, retention
internal/platform/    reconciler for network, MariaDB, Redis, edge; per-site DB/ACL provisioning
internal/secrets/     AES-GCM at-rest encryption keyed from AUTH_SECRET
internal/site/        site model, validation, provisioning/lifecycle jobs, WP-CLI, login links
internal/store/       sqlc-generated queries + handwritten Open/WithTx glue
internal/testdb/      fresh migrated SQLite database per test
internal/webui/       embedded SPA (dist/ is filled by `task build:web`)
migrations/           goose SQL migrations, embedded; also sqlc's schema source
query/                sqlc query files, one per table group
runtime/              presso-runtime image sources (Dockerfile, Caddyfile, wp-config, system plugin)
build/                Dockerfiles for pressod and the edge
web/                  SvelteKit UI (adapter-static)
```

## Install

One command on a fresh Linux host (Debian/Ubuntu tested; amd64 or arm64,
systemd). It installs Docker Engine when missing, the `pressod` release
binary, a systemd unit and the first admin account:

```sh
curl -fsSL https://raw.githubusercontent.com/Hinkolas/presso/main/install.sh | sudo bash
```

Non-interactive form:

```sh
curl -fsSL https://raw.githubusercontent.com/Hinkolas/presso/main/install.sh | sudo bash -s -- \
  --panel-domain panel.example.com --acme-email you@example.com --email you@example.com --yes
```

What it leaves behind: `/usr/local/bin/pressod`, `/etc/presso/presso.env`
(secrets, generated once and never rewritten: `AUTH_SECRET` encrypts the
database and per-site credentials at rest), `/var/lib/presso` (SQLite,
screenshots) and `presso.service`. Ports 80 and 443 are the edge; 7080 is the
loopback panel; 7081 is the internal TLS listener. With a panel domain, the edge serves the admin UI at
`https://<panel-domain>` with an automatic certificate (point the DNS record
at the host first) and authenticates to the engine with a client certificate; without a domain,
reach the panel through an SSH tunnel:
`ssh -L 7080:127.0.0.1:7080 root@host` → http://127.0.0.1:7080.

Upgrades happen from the panel (System → Updates, see below). Re-running the
installer also upgrades (or `--version vX.Y.Z` pins one); `install.sh
--uninstall [--purge]` removes the service and binary (and, with `--purge`,
every presso container, volume and the data).

## Releases and updates

Everything ships from a version tag. `task release:tag V=v0.1.0` (or `git
tag v0.1.0 && git push origin v0.1.0`) triggers `.github/workflows/release.yml`:

1. goreleaser builds `pressod` for linux/amd64 and linux/arm64 with the UI
   embedded and creates a **draft** GitHub release with the tarballs, a
   SHA-256 checksum file and `install.sh`.
2. The edge image is pushed as `ghcr.io/hinkolas/presso-edge:v0.1.0`.
3. The runtime images are built for every pair in the compat matrix's build
   policy and pushed as `ghcr.io/hinkolas/presso-runtime:v0.1.0-php8.5-wp7.1`
   (bases as `presso-runtime-base:v0.1.0-php8.5`), then smoke-tested.
4. The draft is published. Hosts poll `/releases/latest`, which never returns
   drafts, so an update is only offered once every image exists.

A release is one version string that names everything: the engine derives
its edge and runtime image references from its own version (`{release}` in
`EDGE_IMAGE` / `RUNTIME_IMAGE_TEMPLATE`). Updating the engine therefore
updates the runtimes. From System → Updates, "Update to vX.Y.Z" runs the
`engine.update` job: it waits for site jobs to finish, downloads and verifies
the binary, pre-pulls the new images, swaps the binary atomically (the old
one stays as `pressod.previous`) and re-execs. The new engine notices the
version change on boot and enqueues `engine.rollout`, which recreates every
running site on the new runtime image (stopped sites move on their next
start) and runs `wp core update-db`; the platform reconciler recreates the
edge. Pre-release tags (`v0.2.0-rc.1`) are published as pre-releases and are
never offered to stable hosts. `task release:snapshot` rehearses the binary
build locally.

## Development

Requirements: Go 1.26, Node 22, Docker, [task](https://taskfile.dev), [sqlc](https://sqlc.dev).

```sh
cp .env.example .env            # set AUTH_SECRET (openssl rand -base64 32)
task build:edge                 # the edge image the engine starts (minutes)
task build:runtime PHP=8.5 WP=7.1   # one local runtime image (dev builds must use local images; see .env.example)
task user -- create --email you@example.com --role admin
task dev                        # engine on :7080; creates network, MariaDB, Redis, edge
task dev:web                    # UI dev server on :5173 (proxies /api to :7080)
task check                      # vet + tests + web check/lint
task build                      # web build embedded into bin/pressod
```

Open http://localhost:5173 (dev) or http://localhost:7080 (embedded build),
create a site, and visit `http://<slug>.localhost` once the provisioning job
finishes. `task nuke` removes every presso container and volume (dev only).

Ports 80/443 (edge), 3306/6379/7080 (loopback), and 7081 (internal TLS) must
be free on the host.

Conventions: every package states its responsibility and boundary in a doc
comment, business logic lives in service packages and handlers only translate
HTTP, errors carry a package prefix and sentinel errors are mapped to stable
codes in `internal/api/errors.go`.
