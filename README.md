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

## Naming

The product is **presso**; its parts carry the prefix: **presso-runtime** (the
per-site image, RUNTIME.md), **presso-engine** (the orchestration engine, this
repo's Go code, binary `pressod`), **presso-web** (the UI), **presso-harness**
(the agent-facing CLI, binary `presso`, not started yet).

## Components

- **pressod** (`cmd/pressod`, Go): the engine. REST API for the UI and the
  harness CLI, SQLite state, and (as it lands) the Docker-driven site
  lifecycle. One binary, also carrying the operator commands.
- **web** (`web/`, SvelteKit): the UI. Runs as a BFF (adapter-node): the
  browser talks only to the SvelteKit server, which holds the API bearer token
  in an httpOnly cookie and proxies `/_api/*` to the engine.

## Layout

```
cmd/pressod/          daemon entrypoint + operator subcommands (serve, user, migrate)
internal/api/         HTTP layer: chi router, middleware, error envelope, handlers
internal/auth/        operator accounts: argon2id passwords, bearer sessions
internal/config/      env-driven config (go-envconfig), .env for dev only
internal/obs/         slog setup
internal/store/       sqlc-generated queries + handwritten Open/WithTx glue
internal/testdb/      fresh migrated SQLite database per test
internal/utils/       tiny stdlib-only helpers
internal/version/     build version stamped via -ldflags
migrations/           goose SQL migrations, embedded; also sqlc's schema source
query/                sqlc query files, one per table
build/                Dockerfiles
web/                  SvelteKit UI (BFF)
```

Planned packages, in the order the MVP needs them: `internal/docker` (Docker
API client wrapper), `internal/site` (site model, settings layering,
provisioning), `internal/edge` (edge Caddy config + cache purge), `internal/ops`
(update/rollback, maintenance mode, domain change), `cmd/presso` (harness CLI).

## Development

Requirements: Go 1.26, Node 22, [task](https://taskfile.dev), [sqlc](https://sqlc.dev).

```sh
cp .env.example .env            # set AUTH_SECRET (openssl rand -base64 32)
cp web/.env.example web/.env
task user -- create --email you@example.com --role admin
task dev                        # engine on :7080, migrates on boot
task dev:web                    # UI on :5173
task check                      # vet + tests + web check/lint
```

Conventions follow the sibling project skali: every package states its
responsibility and boundary in a doc comment, business logic lives in service
packages and handlers only translate HTTP, errors carry a package prefix and
sentinel errors are mapped to stable codes in `internal/api/errors.go`.
