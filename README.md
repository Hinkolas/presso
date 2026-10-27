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
  release. Runtimes use FrankenPHP on PHP 8.2 and newer; the nginx + PHP-FPM
  legacy runtime is [currently disabled](#legacy-runtimes).
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
| `RUNTIME_LEGACY_ENABLED` | `false`; offer published legacy runtime pairs (none are published) |
| `RUNTIME_PHP_VERSIONS` | Optional allowlist within the published runtime pairs |
| `RUNTIME_DEFAULT_WP` | Default WordPress branch; newest in the catalog if unset |
| `SHOTS_ENABLED` | `true`; enable the shared screenshot browser |

### Legacy runtimes

**The legacy runtime (nginx + PHP-FPM, PHP 7.4–8.1) is disabled.** Releases
publish no legacy images, and `RUNTIME_LEGACY_ENABLED` defaults to `false`.
Only WordPress 6.1 and newer can be offered, because WordPress 5.6–6.0 cannot
run on PHP 8.2 or newer.

Why: in October 2026, PHP fixed two critical, unauthenticated remote code
execution flaws, CVE-2026-109618 and CVE-2026-109619 ("PHP2Shell"), in 8.5.12,
8.4.27, 8.3.36 and 8.2.35. PHP 8.1 and older are end of life and received no
release. The legacy runtime is built on the official Docker `php` images, whose
final 8.1.34, 8.0.30 and 7.4.33 builds remain affected. Backported fixes exist
in distribution packages (for example deb.sury.org), but they keep the same
version numbers and are not in those images. Disabling
`session.upload_progress` only partially mitigates the flaws.

The runtime code stays in the repository. Re-enabling it requires a base image
with the backports and a check that the build actually contains them, then
adding a legacy branch back to the build policy in `internal/compat/matrix.json`.

Setting `RUNTIME_LEGACY_ENABLED=true` has no effect while the release publishes
no legacy pairs. It does not delete existing sites, remove downloaded images,
or change running containers. Host PHP restrictions can narrow the published
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

Every container image comes from presso's registry and carries the release tag:
the edge, `presso-mariadb` (MariaDB 11.8 on Alpine, with jemalloc),
`presso-valkey` (Valkey 9.0 on Alpine), `presso-browser` (Chromium's headless
shell on Debian stable) and the runtimes. They are built from distribution
packages in `build/` and `runtime/`, and each release publishes their sources
first. `MARIADB_IMAGE`, `VALKEY_IMAGE`, `EDGE_IMAGE` and `SHOTS_IMAGE` can point
elsewhere; [.env.example](.env.example) documents what each replacement must
provide.

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

Fresh installations run the BSD-3-Clause licensed Valkey server (9.0, from
Alpine's package) in the `presso-valkey` container, from the
`ghcr.io/hinkolas/presso-valkey` image published with each release.
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

Requirements: Go 1.26, Node.js 22, Docker Engine or Docker Desktop,
[Task](https://taskfile.dev), and [jq](https://jqlang.org). [sqlc](https://sqlc.dev) is needed when changing SQL
queries or the schema.

```sh
cp .env.example .env
# Set AUTH_SECRET in .env, e.g. using: openssl rand -base64 32
task build:edge
task build:services
task build:runtime PHP=8.5 WP=7.1
task user -- create --email you@example.com --role admin
task dev
```

In another terminal, run `cd web && npm ci`, then `task dev:web` from the repository
root. Open <http://localhost:5173>. Local `.env` image templates use the images
you build; build any additional WordPress/PHP pair before selecting it.
`task smoke:services` checks the locally built platform images.

Use a Docker environment without an existing presso installation. Ports
80/443, 3306/6379/7080, and 7081 must be available. `task dev` stops an existing
listener on 7080; `task nuke` deletes presso-managed containers and volumes.

`task check` runs Go vet/tests and frontend checks/lint. `task build` builds the
UI and embeds it in `bin/pressod`.

`main` is protected. Work happens on semantic branches (`feat/`, `fix/`,
`chore/`, ...) and lands through a pull request, which needs the `ci-result`
check to pass. CI runs only for pull requests into `main` (and on manual
dispatch): Linux race tests, installer checks, a clean frontend build, the
compliance tooling and a real source bundle for the engine binary. Pull
requests that change only the root user guides or Markdown under `docs/` skip
those checks.

Image builds, image source collection, smoke tests and the Docker integration
tests run in the image verification workflow, which every release tag runs
before anything is published. Run it by hand before tagging with
`gh workflow run image-verification.yml --ref main`, or locally:
`WP_VERSION=7.1 docker buildx bake --load` builds the same images, then
`go test -tags integration ./internal/integration ./internal/platform`.
See [release verification](docs/release-verification.md) for release test
evidence.

To prepare a release, make the repository public, merge the changes through
pull requests whose CI passed, optionally rehearse with the image verification
workflow, then run
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
