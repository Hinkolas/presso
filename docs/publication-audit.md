# Source publication audit

Audit date: 2026-10-27. Baseline: `1855bdf` plus the public-release preparation
changes. This is a repository hygiene and distribution-readiness review, not
a penetration test or a guarantee that no secret exists.

## Scope and findings

- Reviewed 295 baseline tracked files and historical sensitive filenames.
  No real environment file, private key, database, dump, or backup was found
  tracked. SQL under `query/` and `migrations/` is application source.
- Gitleaks 8.30.1 scanned all available local refs, including fetched remote
  history and local checkpoint refs: 85 reachable commits, of which the scanner
  reported 45 with additions. The branch itself had 39 commits. The scan found
  two false positives: the literal `default=256mb` memory setting and
  `PRESSO_DB_PREFIX=wp_` table prefix. Both were reviewed in context. The config
  suppresses only those exact values in their respective source files; default
  detection remains enabled for tests, examples, and all other content.
- The configured history scan and publishable working-file scan passed.
  The local `AUTH_SECRET` value was also compared directly with 829 historical
  file blobs and was absent. Its value was not logged or added to the repo.
- Reviewed the currently available GitHub-hosted material: 19 workflow log
  archives, three Docker build-record artifacts, and two issue/PR records with
  their comments/reviews. No releases existed. Scanning the extracted material,
  with nested archive inspection enabled, produced 46 matches of one public
  PHP release-signing GPG fingerprint. It was verified against the
  [official PHP image Dockerfile](https://github.com/docker-library/php/blob/master/8.5/bookworm/cli/Dockerfile).
  These were false positives; no real credential was identified. Hosted
  material was kept under a temporary local audit directory, not added to this
  repository or uploaded. Recheck any new material before publication.
- The ignored local `.env` remains private local state. Ignore rules now also
  cover runtime databases, backups, credential directories, local task state,
  and common scanner reports. `.dockerignore` excludes corresponding secrets
  and generated files from the root build context. The frontend no longer
  exempts `.env.test` from its ignore rules.
- Existing Apache-2.0 licensing, attribution, GPL vendoring records, security
  policy, contribution policy, and matching source/notice delivery are retained.
  Dependency maintenance and publication instructions are now documented.
- All external GitHub Actions references are pinned to the commits resolved
  from their existing major-version tags. Weekly Dependabot updates cover Go,
  npm, and GitHub Actions. Shared security checks gate PRs and releases and
  also run weekly and on `main`.

## Dependency findings

The initial npm advisory query reported seven affected packages: four high and
three low. Compatible lockfile fixes removed the high findings; the remaining
low findings required SvelteKit 3 and static adapter 4. Those updates, the
required Svelte patch, and navigation/configuration adjustments are included.
The final npm audit reported zero vulnerabilities, including build tools.
The browser remains a static SPA served by the Go engine.

The installed Go 1.26.5 toolchain initially produced 17 reachable standard-library
advisory findings. The minimum build version and engine Docker builder now use
Go 1.26.10. Govulncheck 1.9.0 then reported zero affected symbols or imported
packages. It still reports the module-level
[GO-2026-5932](https://pkg.go.dev/vuln/GO-2026-5932) advisory for the unmaintained
`golang.org/x/crypto/openpgp` package. The project requires `x/crypto` for other
packages and does not import the affected OpenPGP package. No blanket advisory
ignore or failed-check bypass was added.

## Validation and remaining publication work

Local checks passed:

- Go tests (including race tests) and vet, module integrity verification, and
  Go advisory analysis for the local and Linux amd64 targets.
- A clean locked npm install, frontend type checking, lint, and static build;
  npm advisory analysis reports zero vulnerabilities.
- Workflow linting, installer Bash syntax, release-gate tests (including failed
  security checks on docs-only PRs), and CI change-scope tests.
- Configured history/working-file scans and four scanner regression tests:
  ignored local secrets, an untracked secret beside an allowed expression,
  deleted historical secrets, and forcibly tracked ignored files.
- Compliance unit tests on Python 3.12 and actual Linux amd64 binary source
  collection/verification, retaining 49 linked Go module sources and nine
  browser/font packages with their notices.

The Docker ownership helper test requires an explicit Docker run and was
skipped. Image builds/integration tests were not rerun for this preparation.
SvelteKit's existing `$lib` imports use its supported compatibility alias;
the tool emits a deprecation notice for that alias.

GitHub was read to confirm the repo was **private**, `main` was protected, and
`ci-result` was required with administrator enforcement. Workflow defaults were
read-only, with workflow approval of PR reviews disabled. Remote ref discovery
showed only `main` and no tags. Private-reporting status could not be verified
through its API while the repository was private (HTTP 404); verify it when
publishing. No visibility, package,
or security setting was changed. No branch or tag was pushed and no release
was published as part of this preparation.

Before publication, complete [public-release.md](public-release.md), including
review of any new GitHub-hosted material and external attachments, author-email disclosure,
security settings, anonymous GHCR access, and final CI/image gates. This audit
does not scan unreachable or remote-only objects, external attachment contents,
hosting-provider secrets, external backups, or every package in built container images. A fresh VPS
installation and the actual publication workflow still need to be exercised.
