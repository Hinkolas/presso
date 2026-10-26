# Valkey backend verification

> **Superseded image (2026-10-26).** The default server is now presso's own
> `presso-valkey` image: Valkey 9.0.4 from Alpine 3.24's package, published
> with each release after its source bundle (`build/valkey.Dockerfile`). The
> BSD-3-Clause and client findings below still apply; the image, version and
> digests describe the September selection. The integration tests and the
> release smoke test exercise the new image (`build/smoke.sh valkey`).

Date: 2026-09-08. This supplements the earlier inventory in `inventory.json`;
that historical file still records the Redis image inspected before this change.
This is a functional/backend licensing record, not clearance of every package
in presso's release images.

## Selected server

- Image: `valkey/valkey:8.1.10-alpine`, pulled directly from upstream.
- Observed server output: `Valkey server v=8.1.10`.
- Observed multi-platform image index:
  `sha256:d2e18f3410b6f616de1417f570fa55261af2898b9c5b2cfb6781ce2373ea43d1`.
- Published Linux amd64 manifest:
  `sha256:e55eb7bfbce02957d1a91fb1e26d69aa36eb18f77bf9f86ff6eff81f864b13b7`.
- Published Linux arm64 manifest:
  `sha256:3e0751bb90e44d5d3a2b321bb36d219d7bb86a275ad908feb06567e0f6db77c0`.

The configuration pins the version tag, not the digest. These digests identify
what was inspected; record the actual resolved digest again for final release
verification. Upstream lists 8.1.10 as the supported 8.x release on the
[download page](https://valkey.io/download/).

Valkey's [8.1.10 COPYING file](https://raw.githubusercontent.com/valkey-io/valkey/8.1.10/COPYING)
contains BSD-3-Clause terms and both Valkey contributors' and inherited Redis
copyright notices. The source is at
<https://github.com/valkey-io/valkey/tree/8.1.10>. The container's other packages
retain their respective licenses. The engine pulls this upstream image;
presso's release workflows do not republish it.

The Go `go-redis` client, phpredis PHP extension and Redis Object Cache drop-in
retain their upstream names and licenses. No client dependency or vendored
source was relicensed. The drop-in remains GPLv3 and its SHA-256 remains
`713e9a12018e865f7daf71d84e6257b7756251560f04aac6cc6b5b15bf9d0b33`.

## Deployment policy

The managed container is `presso-valkey`, with Docker alias `valkey`, component
kind `valkey`, and an encrypted `valkey.default_password` in engine state.
Operators configure `VALKEY_IMAGE`, `VALKEY_LISTEN`, and `VALKEY_MAXMEMORY`.
The engine uses a loopback host port; sites use the private container address.
Site ACLs continue to use the existing per-site credentials and key prefixes.
Existing `redis_*` SQLite columns and `PRESSO_REDIS_*` / `WP_REDIS_*` protocol
settings remain intact.

There is no persistent cache volume. RDB snapshots and AOF are disabled, and
the memory policy remains `volatile-lru` with the configured cap. Site ACLs are
restored from encrypted engine state during periodic reconciliation. A cache
restart is allowed to lose cached objects.

This targets fresh installations. Obsolete `REDIS_*` host settings fail
configuration validation. An existing `presso-redis` container blocks managed
reconciliation before resources are changed. No data, container, volume or
operator configuration is automatically reset or migrated. In particular,
[Redis 7.4 data files are incompatible with Valkey](https://valkey.io/topics/migration/).

## Regression coverage

`internal/platform/valkey_integration_test.go` uses the production container
spec and ACL/client/reconciliation methods, with a temporary SQLite database
and a unique container on an ephemeral loopback port. It verifies:

- Actual server identity, the configured memory cap and eviction policy,
  persistence disabled, and the real Docker health check.
- Default-user authentication, rejection of unauthenticated/wrong-password
  access, per-site reads/writes and cross-site read/write/delete denial.
- Denial of global flushes, ACL/configuration changes, key enumeration, and
  cross-site reads attempted from Lua.
- Cache loss on restart, restoring stored site ACLs with the same encrypted
  state, and revoking a deleted site's access.

`internal/integration/lifecycle_test.go` now uses Valkey and delegates cache
user management to the real platform service. It additionally verifies the
actual WordPress/Redis Object Cache/phpredis integration: connection health,
objects persisting across separate WordPress processes, and selective Lua
flushes preserving another tenant's data. Checking backend status and a second
process prevents the drop-in's graceful in-memory fallback from hiding errors.
The existing provisioning, unavailable routing, stop/start/settings, deletion,
and HTTPS callback checks run against the same Valkey-backed site.

Both integration tests passed on Docker Desktop (Linux arm64 containers).
Additional isolated network probes passed with the existing PHP 7.4.33,
8.1.34 and 8.5.9 runtime bases, each using phpredis 6.3.0: ACL authentication,
cache reads/writes and cross-site access denial. These probes do not replace
final runtime-image checks on both architectures.
Native Linux verification remains a required CI gate. The reusable `verify`
workflow runs both packages before release publication:

```sh
go test -tags integration -count=1 -timeout 10m ./internal/integration ./internal/platform
```

The backend policy finding is resolved. The separate notice and corresponding-
source work in [license review](review.md) is still required
before publication.
