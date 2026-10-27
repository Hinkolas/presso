# Public repository and first release candidate

This checklist separates the source-publication review from validating an
installable release. Changing GitHub visibility, pushing changes/tags, and
publishing artifacts are explicit maintainer actions.

## Before changing visibility

- Review the [publication audit](publication-audit.md), including its scope and
  remaining validation. Review Git author emails and old commit messages too;
  these become public with the history.
- Fetch all branches and tags that will be exposed and run
  `python3 scripts/check_secrets.py` with Gitleaks 8.30.1 on `PATH`. It scans all
  available local refs plus tracked and non-ignored working files. It includes
  tracked files that now match `.gitignore`; ignoring an already tracked file
  does not remove it. No test directories or example files are blanket-excluded.
- If a real credential is found, revoke/rotate it first. Coordinate cleanup of
  all affected refs, releases, artifacts, and copies before publication. A
  later deletion or `.gitignore` change is insufficient. Re-scan afterward.
- Review GitHub's existing Actions logs/artifacts/caches, releases and assets,
  issues/PR attachments, and package contents for private data. A local Git
  scan does not establish that those surfaces are clean. Remove obsolete
  sensitive materials before changing visibility.
- Confirm [LICENSE](../LICENSE), [NOTICE](../NOTICE),
  [SECURITY.md](../SECURITY.md), [CONTRIBUTING.md](../CONTRIBUTING.md), and
  [dependency/source documentation](dependencies.md) describe the project you
  intend to publish. Existing licensing is preserved; third-party software
  retains its own terms.
- Merge the preparation changes through a PR with a passing `ci-result` check.
  The required check includes secrets/dependency checks even for docs-only PRs.
  Repository visibility does not need to change before that PR is tested.

Local checks:

```sh
python3 scripts/check_secrets.py
go mod verify
govulncheck ./cmd/... ./internal/... ./migrations/...
npm --prefix web audit --audit-level=low
ruby scripts/check-release-gates.rb
```

Use Go 1.26.10 or newer, Node 22.17 or newer, Gitleaks 8.30.1, and
govulncheck 1.9.0. The security workflow pins the Gitleaks archive by SHA-256
and redacts its output. Avoid uploading scanner reports, even to public CI
artifacts. Review new exceptions as carefully as code changes.

## GitHub settings at publication

These settings live on GitHub and are not enabled by committing files:

- Change repository visibility when the review is complete.
- Enable **private vulnerability reporting**, then verify the reporting link
  in `SECURITY.md` while signed in with a different eligible account. A policy
  file alone does not enable the form. See [GitHub's instructions](https://docs.github.com/en/code-security/how-tos/report-and-fix-vulnerabilities/configure-vulnerability-reporting/configure-for-a-repository).
- Enable the dependency graph, Dependabot alerts/security updates, secret
  scanning, and push protection where available. Dependabot's version-update
  configuration is included in the repo; alerts and security updates are
  separate settings.
- Preserve protection for `main`, require `ci-result`, block force pushes and
  deletions, and use PRs for changes. Keep workflow default permissions at
  **read repository contents**. Require approval for workflows from outside
  contributors. Do not grant untrusted PR workflows repository secrets or
  use `pull_request_target` to execute PR code.
- Check GitHub Actions permissions and publishing credentials. The existing
  release uses the scoped `GITHUB_TOKEN` for this repo and GHCR; it does not
  need a personal token embedded in files. Review any additional configured
  secrets and deployment environments separately.
- Set every GHCR package used by the release to **public** and verify pulls
  anonymously. Repository visibility and package visibility are separate;
  source-bundle availability alone does not prove an image is public. This
  includes `presso-edge`, `presso-mariadb`, `presso-valkey`, `presso-browser`,
  `presso-runtime-base`, and `presso-runtime`.

## Cut and verify `v0.1.0-rc.1`

1. Run the image-verification workflow against the final merged commit and
   confirm its builds, source collection, smoke tests, and Linux integration
   tests pass. Run the normal CI checks too. A local scan is not a substitute
   for these checks.
2. With explicit publication approval, run
   `task release:tag V=v0.1.0-rc.1` on that clean commit. This command creates
   **and pushes** the tag, triggering publication. It is not a dry run.
3. Confirm the release workflow succeeds. Security and regular/image
   verification gate source publication; the installable release stays draft
   until binaries and images pass their publishing/smoke checks. Check that
   `v0.1.0-rc.1` is a prerelease and does not become GitHub's `latest` release.
4. Verify the release's installer, both Linux binary archives and checksums,
   notice files, and the matching `sources-v0.1.0-rc.1` assets are anonymously
   downloadable. Check the intended image tags on both amd64 and arm64.
   Preserve the source assets alongside the binaries/images.
5. Test installation on a new VPS without GitHub/GHCR credentials using the
   exact-version instructions in [README.md](../README.md#getting-started).

## Fresh VPS acceptance test

Use a disposable Linux VPS with systemd on amd64 or arm64, and synthetic site
data. Record the distribution, architecture, Docker version, release, and test
results. Keep SSH access available; allow public HTTP/HTTPS only as needed
for the panel and sites. Management ports should not be public.

- Install the exact RC; confirm checksum verification, `presso.service`,
  `/healthz`, and successful image pulls without registry login.
- Without a panel domain, sign in through an SSH tunnel. With a panel domain,
  verify HTTPS issuance and sign-in. Confirm unauthenticated requests cannot
  perform administrator actions.
- Create a disposable WordPress site, complete setup, and verify HTTPS,
  WordPress admin login, WP-CLI, page cache, object cache, and screenshots.
- Stop and restart the site; confirm a stopped site does not serve cached
  pages. Reboot the VPS and confirm service/site recovery and retained data.
- Check Settings, activity logs, and the beta update channel. An RC belongs to
  the beta channel; stable should not offer it. Sanitize any test report.
- Delete the disposable site and verify its routes disappear. Use the
  documented uninstall flow if tearing down the test host; review destructive
  purge prompts before removing data.

Passing this test establishes a testable RC on that host. It does not establish
hostile-tenant isolation, managed upstream security updates, or backup/recovery
support; those limits are documented in `SECURITY.md` and `README.md`.
