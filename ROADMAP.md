# Roadmap

presso is in initial development (0.x). Releases before `v1.0.0` are
experimental and are not recommended for production workloads. The
[README](README.md) describes what works today; this document describes work
that is **not yet available** unless explicitly stated otherwise.

This is a direction, not a delivery schedule. Priorities and scope may change,
and no item is a promise for a particular release.

## Planned for later releases

### Managed WordPress and plugin updates

- WordPress core and plugin update discovery and management, including security
  updates. Theme and language updates are also planned.
- Operator controls for scheduling and applying updates, with clear status and
  failure reporting.
- Validation before and after changes; recovery workflows built on the backup
  and restore capabilities below.

Today, the engine can update its own release and roll out the matching runtime
images. Core lives in the runtime image and WordPress's automatic updater is
disabled. That is not yet a managed security-update service for site software.

### Backups and recovery

- Per-site database and `wp-content` backups, with scheduled runs and retention.
- Restore to the same site or a separate test site.
- Off-host copies and recovery of the engine's state and secrets.
- Pre-change snapshots and, later, automatic rollback where recovery is safe.

These features are deliberately deferred from the first test release. Current
failed applications require explicit retry; there is no automatic rollback.

### Import, migration, and domains

- Import an existing WordPress site from an archive, SSH source, or migration
  plugin, with compatibility checks and a testable preview before cutover.
- Guided primary-domain changes with serialized-data-aware URL replacement,
  redirects, and cache invalidation.
- Search-and-replace tooling with a dry run.

### Site management

- Plugin/theme discovery, installation, and upload through the panel.
- Staging clones with URL rewriting, noindex, and controlled outbound mail.
- Maintenance pages served by the edge with operator preview access.
- SMTP relay configuration and a development mail catcher.

## Further improvements to explore

- Deeper WordPress diagnostics: slow queries, autoloaded options, database
  growth, PHP errors, integrity checks, storage usage, and actionable alerts.
- Cache-plugin compatibility checks, additional cache controls, rate limiting,
  and outbound HTTP policies.
- Additional runtime sizing controls, image optimization, and CDN integration.
- A local development CLI for human and AI-assisted WordPress work, using the
  same runtime images, editable plugin/theme paths, and snapshot-based resets.
- Multi-host placement, migration between hosts, and object-storage integration.
- Experimental persistent PHP worker modes, only if WordPress/plugin
  compatibility can be demonstrated.

The legacy runtime (nginx + PHP-FPM, PHP 7.4–8.1) is disabled because its
official base images carry no fix for CVE-2026-109618 and CVE-2026-109619; see
[Legacy runtimes](README.md#legacy-runtimes). Bringing it back means building
it on packages with backported fixes and verifying them in the build. Keeping
old versions available would remain a compatibility option, not a commitment to
maintain upstream versions indefinitely.

## Toward v1.0.0

Before recommending production use, the project needs repeatable fresh-host
and upgrade validation, tested backup/restore procedures, a defined security
maintenance policy, and documented compatibility expectations. Publishing
`v1.0.0` will not change the license's warranty disclaimer.

The older [runtime](docs/runtime.md), [orchestration](docs/orchestration.md), and
[harness](docs/harness.md) notes contain implementation ideas and tradeoffs. This
roadmap is the current feature-planning reference; those notes do not establish
release commitments.
