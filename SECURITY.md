# Security policy

## Project status and supported versions

presso is in initial development (0.x). Versions before `v1.0.0` are
not recommended for production workloads. There is no production support
commitment, guaranteed response time, or guaranteed security-fix schedule.

Security reports are welcome for any version. Investigation and fixes focus on
the latest published release and the current development branch. Older
releases and prereleases do not have a maintained backport policy. A fix may
require an upgrade with breaking changes; compatibility is not guaranteed
before v1.0.0.

## Reporting a vulnerability

Please use GitHub's **Report a vulnerability** form to report privately:

<https://github.com/Hinkolas/presso/security/advisories/new>

Do not disclose vulnerability details in a public issue, pull request, or log
attachment. If private reporting is unavailable, open an issue containing only
a request for a private security contact, without technical details or affected
host information. Wait for a private channel before sharing the report.

Include, where possible:

- The presso release or commit, host OS/architecture, and Docker version.
- Affected components and relevant configuration, with secrets removed.
- Reproduction steps or a minimal proof of concept against your own test setup.
- Expected and observed behavior, likely impact, and any suggested mitigation.

Never include real passwords, session/login tokens, `AUTH_SECRET`, private keys,
database dumps, or customer data. Test only systems you own or have permission
to assess, and use disposable sites with synthetic data.

The maintainer will review reports as capacity allows and coordinate fixes and
disclosure privately where possible. There is no bug bounty or paid support
program. The temporary pause on code contributions does not apply to security
reports.

## Security boundaries

presso is intended for an administrator managing trusted test workloads on a
single host. Containers and per-site database/Valkey credentials reduce shared
access, but they are not a guarantee of isolation between hostile tenants.
Containers share the host kernel and infrastructure services.

The engine has privileged Docker access. Host access, Docker access, and panel
administrator access must be treated as sensitive. The local panel binds only
to loopback. Public panel access goes through the HTTPS edge; internal panel
requests require its client certificate. Site purge callbacks use separate
per-site bearer tokens over HTTPS. Edge/cache administration and Chrome DevTools
listen only on container loopback and are reached through Docker exec streams.

Protect `/etc/presso/presso.env` and `DATA_DIR/control`. `AUTH_SECRET` encrypts
stored credentials; it does not encrypt all site data or the entire SQLite
database. The local CA and private keys establish management trust. Losing or
replacing them can break access and recovery. No automated backup or restore
facility exists yet.

## WordPress, plugins, and runtime updates

Managed WordPress core and plugin updates, including security updates, are
planned for a later release. Themes and language updates are also on the
[roadmap](ROADMAP.md). WordPress's automatic updater is disabled, and core is
provided by a pinned runtime image. An engine release/rollout is not a guarantee
that every upstream component or installed extension is patched.

For now, administrators must track upstream advisories and manage the software
they install. Container images are rebuilt with presso releases; there is no
guaranteed upstream patch cadence. Host OS and Docker updates remain the host
administrator's responsibility.

Each PHP branch is pinned to an exact patch release in
`internal/compat/matrix.json`, and the image build fails if the base image ships
a different release. The legacy runtime (PHP 7.4–8.1) is disabled because those
end-of-life branches have no upstream fix for CVE-2026-109618 and
CVE-2026-109619; see [Legacy runtimes](README.md#legacy-runtimes). Disabling it
does not upgrade or remove existing legacy sites.

For a vulnerability confined to an upstream component, also follow that
project's reporting process. Report it here if presso's integration introduces
or materially changes the exposure.
