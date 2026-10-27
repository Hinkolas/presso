# Release regression verification

This change targets fresh installations. Existing installations, site data,
Docker volumes, and operator configuration are not reset.

## Implemented coverage

| Issue | Regression coverage |
| --- | --- |
| 1. Management isolation | Docker exec HTTP and WebSocket transport, container-address isolation, host-gateway checks, safe edge restart, generation matching, screenshots, cancellation |
| 2. Panel and callback isolation | Loopback panel policy, authenticated edge TLS, invalid certificates, forwarded-header spoofing, per-site token checks, real WordPress HTTPS callback, persistent trust and leaf renewal |
| 3. Provisioning exposure | Pause WordPress installation, refresh routes, restart the edge, verify 503/no-store until validation succeeds |
| 4. Upgrade and rollout recovery | Inject image, container, health, database, bootstrap, purge and edge failures; retry; wait for child outcomes; queue rejection; missing containers; matching images with incomplete application; one site worker; interrupted jobs |
| 5. Stop and cache | Populate a real page cache, stop, verify unavailability, then delete and verify the unknown-host response; unavailable routes cover aliases |
| 6. Stopped settings | Save without starting or recreating; Start applies PHP configuration and inspected container memory limits |
| 7. Runtime catalog | Host restrictions cannot invent published pairs; a default without a published pair fails startup |
| 8. Publication gates | CI and releases call the same verification workflow; negative dependency checks reject failed, cancelled and skipped prerequisites |

## Results on 2026-09-08

Passed locally:

- Go tests and vet.
- Complete Go race suite on Linux, in a disposable Go container.
- Installer Bash syntax and ShellCheck.
- Frontend check, lint and build; GoReleaser also performed a clean `npm ci`.
- Docker Desktop integration tests with isolated MariaDB, Redis (before the Valkey change), WordPress,
  edge, browser, networks, databases and volumes.
- Management isolation and authenticated forwarding with a Linux test engine
  inside a separate Docker daemon. No existing host management-port bindings
  were present in that daemon.
- Release dependency checks, including failed/cancelled/skipped prerequisites.
- Local GoReleaser snapshot rehearsal producing Linux amd64 and arm64 archives
  and checksums, with publication disabled.

The baseline reusable Ubuntu verification workflow passed on `642fb22`,
including native Linux integration and race tests. The source/notice pipeline
added afterward still needs CI on its committed changes before tagging.
The earlier additional nested Linux lifecycle
rehearsal could not create resource-limited WordPress containers because Docker
Desktop's nested cgroup hierarchy was in threaded mode. This was an environment
limitation; the complete lifecycle passed on Docker Desktop, but it is not a
substitute for the native Linux integration gate. The disposable nested daemon
was removed. An actual GitHub tag/publication run has not been performed.

The Docker Desktop isolation test detected management ports belonging to the
older development installation. It left them unchanged and verified that the
new test edge published no management ports. On a fresh host, the same test
also requires those host-gateway connections to fail.

## Reproduce

The subsequent Valkey change also passed the production cache spec/ACL/restart
test and the WordPress lifecycle test with real object-cache persistence and
selective flushing. See [the Valkey record](license-review/valkey.md).

The integration tests require a Docker daemon and locally built
`presso-{edge,mariadb,valkey,browser,runtime}:release-verification` tags.
The image verification workflow (`.github/workflows/image-verification.yml`,
run by release tags and by hand) builds them from this checkout with
`docker-bake.hcl`, selecting the runtime from `pressod runtime-matrix`, and
also collects their source bundles and smoke-tests the platform images. Pull
request CI does not build images. Locally:
`WP_VERSION=7.1 docker buildx bake --load`.

```sh
go test ./internal/... ./cmd/...
go vet ./internal/... ./cmd/...
go test -race ./internal/... ./cmd/...
go test -tags integration -count=1 -timeout 10m ./internal/integration ./internal/platform
bash -n install.sh
shellcheck --severity=warning install.sh
ruby scripts/check-release-gates.rb
(cd web && npm ci && npm run check && npm run lint && npm run build)
```

Use `task release:snapshot` for a local binary packaging rehearsal. Snapshot
artifacts are test artifacts, not a published release. The tag workflow keeps
publication dependent on verification, source/notice collection and public
availability, binary packaging, edge builds/smoke checks, and runtime
builds/smoke checks. See [the source-delivery rehearsal](license-review/source-delivery.md)
for coverage, commands, and the distinction between local checks and actual
anonymous downloads during a publishing run.
