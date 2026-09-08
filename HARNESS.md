# HARNESS — Local development harness for AI-driven WordPress work

Status: unimplemented design proposal. Current planning lives in
[ROADMAP.md](ROADMAP.md); the CLI and workflows described here have not shipped.
Designed to fall out of the runtime image with a dev profile rather than be
built separately.

## Goal

Let an AI coding agent (Claude Code or similar) work on a WordPress site with full
control: change files, edit the database, drive the site in a browser, run WP-CLI,
inspect logs. The human mostly reviews. The harness must be fast enough on macOS
that the agent's feedback loop (edit, reload, check) is not dominated by container
file system overhead.

## Core constraint: the VM file system boundary

On macOS (and Windows) Docker runs inside a Linux VM. Bind mounts cross a
guest-to-host file share and cost tens to hundreds of microseconds per file
operation. WordPress does thousands of file operations per request (includes,
OPcache stats, file_exists checks). A bind-mounted WordPress tree is therefore
several times slower than native. Named volumes and image layers live inside the
VM and read at native speed but are not directly visible on the host.

Rule: files live on the same side of the boundary as the process that reads them
most.

## Storage layout

| Content                          | Location        | Why                                   |
|----------------------------------|-----------------|---------------------------------------|
| WordPress core                   | Image (RO)      | Never edited, native speed, atomic     |
| Third-party plugins and themes   | Named volume    | Large, rarely edited, native speed     |
| Uploads                          | Named volume    | Large, never edited by hand            |
| Database data                    | Named volume    | Speed and InnoDB locking/fsync safety  |
| Code currently being worked on   | Bind mount or synced volume | Small, must be editable    |

A promote/demote mechanism moves a plugin or theme between "read-only in volume"
and "editable on host" based on what is being worked on. Promote copies it out to
a bind-mounted directory and symlinks/mounts it into wp-content; demote copies it
back and removes the mount. This keeps the bind-mounted surface to a few dozen
files at a time.

OPcache in dev: revalidate frequently (or always) for the editable paths only;
rarely for everything else.

## Relationship to RUNTIME and ORCHESTRATION

The harness does not have its own WordPress stack. It runs exactly one site
container from the RUNTIME image, plus the shared services it needs, and omits
the edge proxy entirely:

```
  laptop
  +-----------------------------------------------------------+
  | Docker VM (macOS) / native (Linux)                        |
  |                                                           |
  |  site container (RUNTIME image, profile=dev)              |
  |    FrankenPHP publishes its port directly to the host     |
  |    e.g. http://localhost:8080 or https://mysite.localhost |
  |    (Caddy can issue a local cert itself; no edge needed)  |
  |    core RO in image, wp-content volume, editable code     |
  |    bind-mounted or synced in                              |
  |                                                           |
  |  mariadb (single db)    valkey       mailcatcher          |
  +-----------------------------------------------------------+
  | host: Claude Code, editor, browser, `bw` CLI wrapper      |
  +-----------------------------------------------------------+
```

Because the site container is self-contained (see "Container structure" in
RUNTIME.md), removing the edge changes nothing inside it. The only differences
from production are the dev profile flags and that FrankenPHP's port is
published to the host. There is no page cache in the harness, which is also
what you want when editing.

The harness is a thin client of the same Go engine code (ORCHESTRATION.md,
"Substrate and stack"), either linking the packages directly as a CLI or
talking to a locally running engine. It is the same provisioning code with
different flags: "create site" with `edge=false`, `profile=dev`,
`publish_port=true`. This keeps dev and production identical apart from those
flags, and lets the harness import a production site by pointing at the
engine's backup format. Multiple local sites are just multiple site containers
on different ports.

## Two ways to run the agent

1. Agent on the host (default Claude Code usage). Needs:
   - Bind-mounted editable code as above.
   - A CLI wrapper (`bw` or similar) that proxies into the container:
     `bw wp ...` (WP-CLI), `bw db ...` (mysql client / dump / import),
     `bw logs`, `bw exec`, `bw cat <core path>` for reading core/plugin source
     that lives in the volume without mounting it.
   - Browser access via Playwright MCP or Chrome MCP against the site's local URL.
2. Agent inside the container (devcontainer style). Claude Code, WP-CLI, mysql
   client, Playwright/Chromium all run inside the VM. No file boundary at all.
   Host editor attaches remotely if the human wants to look. Cleanest for
   performance; the human's tooling is the cost. Worth supporting both.

## Required capabilities for the agent

- File access to wp-content (and read access to core for reference).
- WP-CLI with full permissions (users, options, plugins, cache flush, cron run,
  search-replace, db query), exec'ed as the `ops` user so plugin/theme installs
  work while the web process stays unable to write code (RUNTIME.md section 8).
  In dev the editable code directory is owned by the host user via the bind
  mount; the two-user split applies to volume content only.
- Direct SQL access (read and write) to the site database.
- Browser automation against the site with a logged-in admin session available
  (auto-login helper or a known dev credential).
- Access to PHP error log, web server access log, slow query log, Query Monitor
  or equivalent per-request profile output in a machine-readable form.
- Mail catcher so transactional mail can be inspected.
- Xdebug available but off by default (it slows everything down when on).
- Snapshot/restore of database and uploads so the agent can experiment and reset.
- Import of a production site via the engine's migration path (migration
  plugin push, SSH pull, or archive), landing at `<slug>.localhost` through the
  same domain operation production uses (ORCHESTRATION.md, "Domains").
- Legacy PHP sites (7.4-8.1) run locally on the FPM build of the runtime
  image, exactly as in production.

## Dev profile of the runtime image

Same image as production, different profile:
- WP_DEBUG, WP_DEBUG_LOG, SCRIPT_DEBUG on
- OPcache validate_timestamps on for editable paths
- Xdebug installed, disabled by default
- Mail routed to catcher
- No full-page cache (or trivially bypassable)
- Object cache on (Valkey), to match production behavior
- Real cron via CLI, same as production

## Open questions

- Promote/demote UX: explicit command vs automatic detection of edits.
- Whether to run a Linux VM with the agent inside it as the primary mode and
  treat host-side editing as secondary.
- Browser session for the agent: use the system plugin's one-click login
  (RUNTIME.md section 14); the harness mints the token locally. Application
  passwords + REST for non-browser tasks.
