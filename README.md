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
- **runtime** (`runtime/`): the `presso-runtime:php<x.y>` image, one container
  per site (FrankenPHP + WordPress core + WP-CLI + the `presso` system
  plugin). See RUNTIME.md.
- **edge** (`build/edge.Dockerfile`): `presso-edge`, Caddy with the Souin page
  cache. pressod renders and pushes its whole Caddyfile.
- **browser**: `presso-browser`, a stock headless Chrome
  (`chromedp/headless-shell`) that captures each site's front page for the
  UI previews. Optional (`SHOTS_ENABLED=false`); see `SHOTS_*` in
  `.env.example`.

## Topology (single host)

```
host: pressod (:7080 API + UI, docker.sock, SQLite in DATA_DIR)
docker network "presso":
  presso-edge       :80 :443 public · 127.0.0.1:2019 admin · 127.0.0.1:2020 cache API
  presso-mariadb    127.0.0.1:3306      one database + user per site
  presso-redis      127.0.0.1:6379      one ACL user + key prefix per site
  presso-browser    127.0.0.1:9222      headless Chrome (DevTools) for page previews
  presso-site-<slug>  no published ports; the edge proxies to site-<id>:8080
```

pressod runs on the host and reaches the shared services through ports
published on loopback; everything per site goes through the Docker API
(exec, health, logs, stats). Site containers reach the engine at
`host.docker.internal:7080` for cache purges.

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

## Development

Requirements: Go 1.26, Node 22, Docker, [task](https://taskfile.dev), [sqlc](https://sqlc.dev).

```sh
cp .env.example .env            # set AUTH_SECRET (openssl rand -base64 32)
task build:edge build:runtime   # images the engine starts / sites run (minutes)
task user -- create --email you@example.com --role admin
task dev                        # engine on :7080; creates network, MariaDB, Redis, edge
task dev:web                    # UI dev server on :5173 (proxies /api to :7080)
task check                      # vet + tests + web check/lint
task build                      # web build embedded into bin/pressod
```

Open http://localhost:5173 (dev) or http://localhost:7080 (embedded build),
create a site, and visit `http://<slug>.localhost` once the provisioning job
finishes. `task nuke` removes every presso container and volume (dev only).

Ports 80/443 (edge), 3306, 6379, 2019, 2020, 9222 (loopback) and 7080 must
be free on the host.

Conventions: every package states its responsibility and boundary in a doc
comment, business logic lives in service packages and handlers only translate
HTTP, errors carry a package prefix and sentinel errors are mapped to stable
codes in `internal/api/errors.go`.
