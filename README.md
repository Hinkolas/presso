# presso

**Self-hosted WordPress hosting, with one panel for your sites.**

presso runs multiple WordPress sites on a single host, combining per-site
containers with shared routing, caching, databases, and a management UI. It
handles provisioning and routine operations so you can manage sites without
maintaining a separate Docker setup for each one.

> **Initial development (0.x) — not recommended for production workloads.**
> Before `v1.0.0`, expect bugs, breaking changes, downtime, and possible data loss.
> APIs, configuration, storage formats, and upgrade paths may change without
> backward compatibility. Automated backups, restore, and rollback are not
> available yet. Use disposable test sites and keep independent copies of any
> data you need. The software is provided as-is, without warranties, subject to
> the terms of the [Apache 2.0 license](LICENSE).

Following [Semantic Versioning](https://semver.org/spec/v2.0.0.html), `0.x.y`
is the initial-development phase. A tag such as `v0.1.0` is a normal release;
`v0.1.0-alpha.1`, `v0.1.0-beta.1`, and `v0.1.0-rc.1` are prereleases of
`v0.1.0`. A normal 0.x release does not imply production readiness or a stable API.

[Getting started](#getting-started) · [Configuration](#configuration) ·
[Roadmap](ROADMAP.md) · [Security](SECURITY.md) · [Contributing](CONTRIBUTING.md)

## What you can do today

- **Manage sites in one panel:** create, start, stop, restart, and delete sites;
  follow background jobs and retry failed operations.
- **Choose a runtime:** select from the WordPress/PHP pairs published with your
  release. Modern runtimes use FrankenPHP; legacy runtimes use nginx + PHP-FPM.
- **Serve sites through a shared edge:** Caddy routes domains and manages HTTPS
  certificates. Souin provides page caching, with content-driven invalidation
  and optional cache warming.
- **Cache database objects:** Redis Object Cache uses a separate Valkey ACL user
  and key prefix for each site. MariaDB uses a separate database and user per site.
- **Tune individual sites:** configure PHP settings, container CPU/memory limits,
  cache behavior, and file modification policy.
- **Operate WordPress:** run WP-CLI, open a short-lived WordPress admin login
  link, and run due cron events through the engine.
- **See what is happening:** inspect live logs, container statistics, request
  metrics, job history, system status, and optional site screenshots.

The engine also includes release-based self-update and runtime rollout machinery.
This does **not** provide managed WordPress/plugin security updates; see
[Updates and recovery](#updates-and-recovery). Planned capabilities live in
[ROADMAP.md](ROADMAP.md).

## Getting started

Use a **fresh, disposable Linux host** with systemd, an amd64 or arm64 CPU,
root access, `curl`, CA certificates, `tar`, and `sha256sum`. The installer can
install Docker Engine if it is missing. Linux is the deployment target; macOS
with Docker Desktop is supported for [local development](#local-development).

For the first test release, select the version explicitly. Run this once
`v0.1.0-rc.1` is available on the [releases page](https://github.com/Hinkolas/presso/releases):

```sh
curl -fsSL https://github.com/Hinkolas/presso/releases/download/v0.1.0-rc.1/install.sh \
  | sudo bash -s -- --version v0.1.0-rc.1
```

The installer downloads and checks the release binary, writes configuration,
starts `presso.service`, and creates the first admin account. It prompts for
your admin email and an optional panel domain.

For an unattended test installation with a panel domain:

```sh
curl -fsSL https://github.com/Hinkolas/presso/releases/download/v0.1.0-rc.1/install.sh \
  | sudo bash -s -- --version v0.1.0-rc.1 \
    --panel-domain panel.example.com --acme-email you@example.com \
    --email you@example.com --yes
```

Point the panel domain's DNS at the host before installation. The panel will
be available at `https://panel.example.com`. Without a panel domain, use an
SSH tunnel and open <http://127.0.0.1:7080>:

```sh
ssh -L 7080:127.0.0.1:7080 root@your-host
```

Sign in with the generated credentials, create a site, and point its domain
at the host. The site becomes available after WordPress installation and
validation succeed. Real domains use automatic HTTPS; `.localhost` domains
use HTTP for local development.

The installer writes:

| Path | Purpose |
| --- | --- |
| `/usr/local/bin/pressod` | Engine binary with embedded web UI |
| `/etc/presso/presso.env` | Host configuration and generated secrets |
| `/var/lib/presso` | Engine database, management certificates, and screenshots |
| Docker named volumes | Site content, MariaDB data, and shared service data |

An unversioned installation and the panel's update checker use GitHub's latest
non-prerelease feed, which includes normal `v0.x.y` releases. Prereleases are
excluded, so **use `--version` to select an alpha, beta, or release candidate**.
The GitHub release assets and GHCR images must be public for installation
without registry credentials.

## Configuration

Edit `/etc/presso/presso.env` and restart `presso.service` to apply host settings.
For local development, use `.env`. See [.env.example](.env.example) for the full
configuration reference.

| Setting | Default / purpose |
| --- | --- |
| `HTTP_ADDR` | `127.0.0.1:7080`; the local panel must bind to loopback |
| `INTERNAL_HTTPS_ADDR` | `:7081`; authenticated edge traffic and site callbacks |
| `PANEL_DOMAIN` | Optional public hostname for the HTTPS panel |
| `RUNTIME_LEGACY_ENABLED` | `true`; offer published legacy runtime pairs |
| `RUNTIME_PHP_VERSIONS` | Optional allowlist within the published runtime pairs |
| `RUNTIME_DEFAULT_WP` | Default WordPress branch; newest in the catalog if unset |
| `SHOTS_ENABLED` | `true`; enable the shared screenshot browser |

### Legacy runtimes

Legacy support is **enabled by default** for compatibility testing. The legacy
flavor supports PHP 7.4–8.1, but only pairs selected by the release's build
policy are published and offered. Availability is not a security-support promise.

To stop offering legacy runtimes, set:

```ini
RUNTIME_LEGACY_ENABLED=false
```

This restricts the catalog and API selections to published FrankenPHP pairs.
It can discourage new use of old versions and avoid additional legacy image
downloads. It does not delete existing sites, remove downloaded images, or
automatically reduce the resources used by running containers. Review existing
legacy sites before disabling it. Host PHP restrictions can narrow the published
catalog; they cannot create extra image combinations.

## Updates and recovery

A release names the engine, edge image, and runtime images together. Runtime
rollouts recreate running sites, run the required WordPress database upgrade,
and validate WordPress before enabling traffic. Stopped sites defer pending
configuration until their next Start.

Failed or interrupted applications remain pending and require an **explicit
retry**. Deliberately stopped sites and sites with incomplete changes return
503 responses that bypass the cache. There is no automatic rollback.

WordPress core is supplied by the runtime image, and WordPress's automatic
updater is disabled. **Managed WordPress core and plugin updates, including
security updates, are planned for a later release.** Engine updates are not a
guarantee that every site's WordPress, plugins, or themes are current. Testers
are responsible for their installed software; see [SECURITY.md](SECURITY.md).

Read the release notes before every upgrade. To select a specific release or
prerelease, rerun its installer with an explicit `--version`. Compatibility with
older development installations is not guaranteed. Keeping `pressod.previous` is not a backup:
reverting a binary cannot undo database or site-content changes.

Backups, scheduled snapshots, restore tooling, and automatic rollback are
deferred. Preserve your own copies of site databases and content, engine state,
`AUTH_SECRET`, and the management trust material. Do not use presso as the only
copy of important data.

## How it fits together

```text
Linux host: pressod + embedded panel + SQLite + Docker API
  └─ shared Docker network
       ├─ Caddy / Souin edge          public HTTP and HTTPS
       ├─ MariaDB                    database and user per site
       ├─ Valkey                     ACL user and key prefix per site
       ├─ headless Chrome            optional screenshots
       └─ WordPress containers       one runtime and content volume per site
```

The engine connects to edge administration and Chrome DevTools through Docker
exec streams to container loopback endpoints; these ports are not published.
The local panel and the published MariaDB/Valkey ports bind to host loopback.
Site purge callbacks use HTTPS and per-site bearer tokens. Panel requests on
the internal HTTPS listener additionally require the edge's client certificate.

Management trust lives under `DATA_DIR/control`. The CA persists across restarts;
leaf certificates renew automatically. Sites receive only the public CA and the
edge receives its own client credentials. Missing or invalid trust material
fails clearly instead of silently creating a new CA.

These boundaries do not make a shared Docker host a sandbox for hostile tenants.
The engine is privileged through its Docker access. See [SECURITY.md](SECURITY.md)
for scope and reporting.

## Object cache

Fresh installations use `valkey/valkey:8.1.10-alpine`, the BSD-3-Clause licensed
Valkey server, in `presso-valkey`. Presso pulls the upstream image directly.
The host settings are `VALKEY_IMAGE`, `VALKEY_LISTEN` (default
`127.0.0.1:6379`), and `VALKEY_MAXMEMORY` (default `256mb`). Image overrides must
provide `valkey-server` and `valkey-cli`; operators maintain their chosen image.

Each site receives a separate ACL user and key prefix. The WordPress client is
still the GPLv3 **Redis Object Cache** plugin using phpredis. Its upstream name,
`WP_REDIS_*` constants, and the runtime's `PRESSO_REDIS_*` protocol settings stay
unchanged; they connect to Valkey. This does not require a Redis server or
relicense the plugin. See [third-party notices](docs/licensing.md) and
[the Valkey verification record](docs/license-review/valkey.md).

Valkey is a disposable object cache: RDB snapshots and AOF persistence are
disabled. A restart loses cached objects; WordPress rebuilds them, and the
engine reapplies site ACLs during reconciliation. Site databases, content, and
engine state live elsewhere.

This first release targets fresh installations. `REDIS_IMAGE`, `REDIS_LISTEN`,
and `REDIS_MAXMEMORY` are rejected, and a managed `presso-redis` installation
blocks reconciliation before any managed resources are changed. There is no
automatic in-place migration or cleanup. Keep an existing development
installation separate while testing; do not run the new engine against its
state or use `task nuke` to migrate it. Retiring the old cache and moving
existing sites requires a deliberate operator transition. Redis 7.4 data files
must not be mounted into Valkey; [upstream documents the incompatibility](https://valkey.io/topics/migration/).

## Local development

Requirements: Go 1.26, Node.js 22, Docker Engine or Docker Desktop, and
[Task](https://taskfile.dev). [sqlc](https://sqlc.dev) is needed when changing SQL
queries or the schema.

```sh
cp .env.example .env
# Set AUTH_SECRET in .env, e.g. using: openssl rand -base64 32
task build:edge
task build:runtime PHP=8.5 WP=7.1
task user -- create --email you@example.com --role admin
task dev
```

In another terminal, run `cd web && npm ci`, then `task dev:web` from the repository
root. Open <http://localhost:5173>. Local `.env` image templates use the images
you build; build any additional WordPress/PHP pair before selecting it.

Use a Docker environment without an existing presso installation. Ports
80/443, 3306/6379/7080, and 7081 must be available. `task dev` stops an existing
listener on 7080; `task nuke` deletes presso-managed containers and volumes.

`task check` runs Go vet/tests and frontend checks/lint. `task build` builds the
UI and embeds it in `bin/pressod`. CI additionally runs Linux race tests,
installer checks, a clean frontend build, and Docker integration tests.
For pushes and pull requests that change only the root user guides or Markdown
under `docs/`, a small change-detection job runs and skips the expensive checks.
Code/configuration, license/source-access files, and runtime vendoring records
still require full CI. Manual CI runs and release tags always verify everything.
See [release verification](docs/release-verification.md) for release test evidence.

To prepare a release, make the repository public and wait for CI on the
committed changes to pass, then run
`task release:tag V=v0.1.0-rc.1` with an unused version. This creates and pushes
the tag. The workflow first publishes a separate `sources-<version>` prerelease
with matching source bundles and third-party notices. It verifies anonymous
source downloads before pushing each corresponding image. Source preparation
fails if the repository is private; it does not change visibility for you.
The release workflow requires verification before publishing images
and keeps the GitHub release in draft until binary, edge, runtime, and smoke
jobs succeed. Plain `vX.Y.Z` tags, including `v0.x.y`, are normal releases and
update GitHub's `latest` feed. Tags with an `-alpha.N`, `-beta.N`, or `-rc.N`
suffix are prereleases and do not update that feed. `task release:snapshot`
rehearses the binary packaging locally without publishing.

The tag task and publishing workflows enforce the same version format as the
engine: `vX.Y.Z` or `vX.Y.Z-{alpha,beta,rc}.N`. Use dotted suffixes (`-rc.1`,
never `-rc1`), no leading zeros in numeric components, and no build metadata
(`+build.1`). Invalid tags fail validation before any release artifacts publish.

Source collection requires Python 3.12+, Go, Docker (for image bundles),
network access to upstream source archives, and several GB of temporary disk
space. See [licensing and source access](docs/licensing.md) for downloads and retention.

## Project documentation

- [ROADMAP.md](ROADMAP.md): planned work, deferred features, and longer-term ideas.
- [SECURITY.md](SECURITY.md): security scope, updates, and private reporting.
- [CONTRIBUTING.md](CONTRIBUTING.md): the current contribution policy.
- [docs/orchestration.md](docs/orchestration.md), [docs/runtime.md](docs/runtime.md), and
  [docs/harness.md](docs/harness.md): technical design notes, including historical proposals;
  these are not a list of shipped features.

## License

Copyright 2026 Hinkolas and contributors.

Unless otherwise noted, presso's original code and documentation are licensed
under the [Apache License, Version 2.0](LICENSE). Third-party components retain
their own licenses, including the vendored GPLv3 Redis Object Cache drop-in.
Release archives and images carry generated `THIRD_PARTY_LICENSES.txt`.
See [licensing and source access](docs/licensing.md) for third-party notices
and matching source downloads.

External pull requests and patches are not being accepted at this time.
Bug reports and feedback are welcome; see [CONTRIBUTING.md](CONTRIBUTING.md).
