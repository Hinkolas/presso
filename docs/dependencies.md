# Dependency inventory and maintenance

Original presso code and documentation use [Apache-2.0](../LICENSE), with the
project attribution in [NOTICE](../NOTICE). Dependencies retain their own
licenses. The [licensing and source-access guide](licensing.md) describes the
notices and matching sources distributed with releases.

## Where versions and licenses are recorded

| Scope | Version inventory | License and source records |
| --- | --- | --- |
| Go engine, direct and transitive modules | [`go.mod`](../go.mod), [`go.sum`](../go.sum), and the compiled binary's Go build information | Release collector reads actual linked modules, verifies their source checksums, and retains license/notice files in the binary's source bundle |
| Browser code and fonts | [`web/package-lock.json`](../web/package-lock.json); installed versions in generated `web/static/third-party-manifest.json` | [`web/scripts/licenses.mjs`](../web/scripts/licenses.mjs) copies installed license texts to `web/static/third-party-licenses.txt`; both files are regenerated before each web build |
| Frontend build tools and optional platform packages | All entries in `web/package-lock.json`, including their license declarations | Upstream npm packages; build-only dependencies are not automatically claimed to ship in the browser |
| WordPress and PHP branches/patches | [`internal/compat/matrix.json`](../internal/compat/matrix.json) and [`runtime/`](../runtime/) build scripts | Runtime source bundle records the exact image contents, source archives, build recipes, and notices |
| Web servers, PHP extensions, WP-CLI, and OS packages | [`runtime/`](../runtime/), [`build/`](../build/), and the built image's package inventory | Source collector inventories actual installed components; Debian source packages or Alpine recipes/distfiles and independently installed sources are retained |
| Vendored Redis Object Cache | [`runtime/dropins/README.md`](../runtime/dropins/README.md) records version, upstream location, and SHA-256 | GPLv3; upstream headers and [`COPYING`](../runtime/dropins/COPYING) accompany the source |
| CI tools | Workflow action commit hashes and scanner versions/checksums in [`.github/workflows/`](../.github/workflows/) | Build infrastructure; these are separate from the runtime inventory |

The browser notice list currently covers SvelteKit, Svelte, devalue, esm-env,
clsx, uPlot, Instrument Sans, Instrument Serif, and IBM Plex Mono. The Vite build
checks bundled module IDs against this list and fails when a dependency lacks
a notice. Fonts are distributed with their upstream font license texts.

The Go MySQL driver is MPL-2.0; its versioned source notice is in
[licensing.md](licensing.md#mysql-driver-source-notice). The independently
vendored Redis Object Cache is GPLv3. WordPress, MariaDB, PHP, Chromium, Caddy,
Valkey, and OS packages introduce further licenses; the root Apache license
does not replace them. See the existing [license review](license-review/review.md)
for the distribution boundaries and its limitations.

The historical [review inventory](license-review/inventory.json) records a
specific earlier audit. Use each release artifact's generated manifest for its
actual dependency versions and source identity.

## Updating dependencies

1. Update Go modules or npm dependencies and commit the corresponding checksum
   or lockfile changes. Use `npm ci` to reproduce a locked frontend install.
2. Run the regular checks, `task check:deps`, and `task check:secrets`.
   `govulncheck` v1.9.0 and Gitleaks v8.30.1 must be on `PATH`.
3. For new browser imports, update `web/scripts/licenses.mjs` and rebuild.
   Keep the MySQL source notice aligned if that module changes.
4. For runtime changes, update the compatibility matrix, source collectors,
   vendoring records, and smoke tests as applicable. Rehearse the image
   verification workflow before tagging.
5. Verify a new source bundle from the final build. Retain matching notices
   and anonymously downloadable sources for every distributed artifact;
   preserve published source assets as described in [licensing.md](licensing.md).

Dependabot proposes weekly Go, npm, and GitHub Actions updates. It does not
automatically merge changes, update the PHP/WordPress compatibility policy,
or replace the image inventory/source checks. Go vulnerability analysis and
`npm audit` run for every PR/release, on `main`, and weekly; they do not scan
every package in the container images. A passing advisory check reflects the
database at the time of the scan.

## Rehearse binary source and license collection

With Python 3.12+, Go, Node.js, and network access:

```sh
(cd web && npm ci && npm run build)
mkdir -p internal/webui/dist
cp -R web/build/. internal/webui/dist/
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o /tmp/pressod ./cmd/pressod
python3 scripts/compliance/collect.py binary /tmp/pressod --out /tmp/presso-engine-compliance
python3 scripts/compliance/collect.py verify /tmp/presso-engine-compliance
```

Use an empty output directory for each run. Generated frontend notices,
embedded UI output, and compliance output are ignored rather than committed;
the release builds regenerate and package them. This rehearsal publishes
nothing. Image-level collection and integration checks are documented in
[release verification](release-verification.md).
