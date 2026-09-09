#!/usr/bin/env bash
# presso installer: one command on a fresh Linux host.
#
#   curl -fsSL https://raw.githubusercontent.com/Hinkolas/presso/main/install.sh | sudo bash -s -- --version v0.1.0-rc.1
#
# What it does: installs Docker when missing (get.docker.com), downloads the
# pressod release binary for this CPU, verifies its checksum, writes
# /etc/presso/presso.env (secrets generated once, never rewritten) and a
# systemd unit, starts the service and creates the first admin account.
# Re-running upgrades the binary and restarts the service; configuration and
# data are left alone. Later upgrades normally happen from the panel
# (System → Updates); this script is the way back when that is not possible.
#
# Options (after `bash -s --`):
#   --version vX.Y.Z      explicit release (required to select a prerelease)
#   --panel-domain HOST   serve the panel at https://HOST through the edge
#   --acme-email EMAIL    contact e-mail for Let's Encrypt
#   --email EMAIL         first admin's e-mail (asked interactively otherwise)
#   --yes                 no questions; go with defaults, abort on real problems
#   --uninstall [--purge] remove the service and binary (--purge: also every
#                         presso container, volume, the data and the config)
set -euo pipefail

REPO="${PRESSO_REPO:-Hinkolas/presso}"
API="${PRESSO_API_URL:-https://api.github.com}"
BIN=/usr/local/bin/pressod
ENV_FILE=/etc/presso/presso.env
UNIT=/etc/systemd/system/presso.service
DATA_DIR=/var/lib/presso
PORT=7080

VERSION="" PANEL_DOMAIN="" ACME_EMAIL="" ADMIN_EMAIL="" YES=0 UNINSTALL=0 PURGE=0

# --- output helpers ----------------------------------------------------------
if [ -t 2 ]; then
  bold=$'\e[1m' dim=$'\e[2m' red=$'\e[31m' green=$'\e[32m' reset=$'\e[0m'
else
  bold="" dim="" red="" green="" reset=""
fi
say()  { printf '%s\n' "$*" >&2; }
step() { printf '\n%s==> %s%s\n' "$bold" "$*" "$reset" >&2; }
ok()   { printf '%s  ✓ %s%s\n' "$green" "$*" "$reset" >&2; }
die()  { printf '%s  ✗ %s%s\n' "$red" "$*" "$reset" >&2; exit 1; }

# When piped through bash, stdin is the script itself: prompts read the tty.
ask() { # ask VAR "Prompt" [default]
  local var="$1" prompt="$2" default="${3:-}" answer=""
  if [ "$YES" = 1 ] || [ ! -e /dev/tty ]; then
    printf -v "$var" '%s' "$default"
    return
  fi
  if [ -n "$default" ]; then prompt="$prompt [$default]"; fi
  printf '%s: ' "$prompt" >/dev/tty
  IFS= read -r answer </dev/tty || true
  printf -v "$var" '%s' "${answer:-$default}"
}
confirm() { # confirm "Question?" → 0 yes / 1 no (yes when --yes)
  local a=""
  [ "$YES" = 1 ] && return 0
  [ -e /dev/tty ] || return 1
  printf '%s [y/N] ' "$1" >/dev/tty
  IFS= read -r a </dev/tty || true
  [[ "$a" =~ ^[Yy]([Ee][Ss])?$ ]]
}

# --- arguments ---------------------------------------------------------------
while [ $# -gt 0 ]; do
  case "$1" in
    --version)      VERSION="$2"; shift 2 ;;
    --panel-domain) PANEL_DOMAIN="$2"; shift 2 ;;
    --acme-email)   ACME_EMAIL="$2"; shift 2 ;;
    --email)        ADMIN_EMAIL="$2"; shift 2 ;;
    --yes|-y)       YES=1; shift ;;
    --uninstall)    UNINSTALL=1; shift ;;
    --purge)        PURGE=1; shift ;;
    -h|--help)      sed -n '2,25p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) die "unknown option: $1 (see --help)" ;;
  esac
done

# --- preflight ---------------------------------------------------------------
[ "$(id -u)" = 0 ] || die "run as root: curl -fsSL … | sudo bash"
[ "$(uname -s)" = Linux ] || die "presso runs on Linux hosts (this is $(uname -s))"
command -v systemctl >/dev/null || die "systemd is required (systemctl not found)"
case "$(uname -m)" in
  x86_64|amd64)  ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) die "unsupported CPU architecture: $(uname -m) (amd64 and arm64 are built)" ;;
esac
for tool in curl tar sha256sum; do
  command -v "$tool" >/dev/null || die "$tool is required; install it and re-run"
done
[ -d /etc/ssl/certs ] || [ -f /etc/ssl/cert.pem ] || die "ca-certificates is required (apt-get install -y ca-certificates)"

# --- uninstall ---------------------------------------------------------------
if [ "$UNINSTALL" = 1 ]; then
  step "Removing presso"
  if systemctl list-unit-files presso.service >/dev/null 2>&1 && [ -f "$UNIT" ]; then
    systemctl disable --now presso.service >/dev/null 2>&1 || true
    rm -f "$UNIT"; systemctl daemon-reload
    ok "service stopped and removed"
  fi
  rm -f "$BIN" "$BIN.previous" "$BIN.new"; ok "binary removed"
  if [ "$PURGE" = 1 ]; then
    if command -v docker >/dev/null && confirm "Remove EVERY presso container, volume and network (all sites and their data)?"; then
      ids=$(docker ps -aq --filter label=presso.managed=true || true)
      [ -n "$ids" ] && docker rm -f $ids >/dev/null 2>&1 || true
      vols=$(docker volume ls -q --filter label=presso.managed=true || true)
      [ -n "$vols" ] && docker volume rm -f $vols >/dev/null 2>&1 || true
      nets=$(docker network ls -q --filter label=presso.managed=true || true)
      [ -n "$nets" ] && docker network rm $nets >/dev/null 2>&1 || true
      ok "containers, volumes and networks removed"
    fi
    if confirm "Delete $DATA_DIR (engine database, screenshots) and $ENV_FILE (secrets)?"; then
      rm -rf "$DATA_DIR" /etc/presso; ok "data and configuration deleted"
    fi
  else
    say "  kept: $ENV_FILE, $DATA_DIR and the Docker containers/volumes (add --purge to remove them)"
  fi
  exit 0
fi

# --- docker ------------------------------------------------------------------
step "Checking Docker"
if ! command -v docker >/dev/null; then
  say "  Docker Engine is not installed."
  if confirm "Install it now with Docker's official script (get.docker.com)?"; then
    curl -fsSL https://get.docker.com | sh
    systemctl enable --now docker >/dev/null 2>&1 || true
  else
    die "install Docker Engine (https://docs.docker.com/engine/install/) and re-run"
  fi
fi
docker info >/dev/null 2>&1 || die "Docker is installed but the daemon is not reachable (systemctl status docker)"
ok "Docker $(docker version --format '{{.Server.Version}}' 2>/dev/null || echo present)"

DOCKER_UNIT=""
if systemctl cat docker.service >/dev/null 2>&1; then
  DOCKER_UNIT=docker.service
else
  say "  ${dim}no docker.service unit found (snap install?); presso will not be ordered after Docker${reset}"
fi

# --- ports -------------------------------------------------------------------
FRESH=1; [ -f "$ENV_FILE" ] && FRESH=0
if [ "$FRESH" = 1 ] && command -v ss >/dev/null; then
  busy=""
  for p in 80 443 $PORT; do
    if ss -ltnH "sport = :$p" 2>/dev/null | grep -q .; then busy="$busy $p"; fi
  done
  if [ -n "$busy" ]; then
    say "  ports already in use:$busy (presso needs 80 and 443 for the edge and $PORT for the engine)"
    confirm "Continue anyway?" || die "free the ports (another web server?) and re-run"
  fi
fi

# --- release -----------------------------------------------------------------
step "Resolving the release"
if [ -z "$VERSION" ]; then
  latest=$(curl -fsSL -H 'Accept: application/vnd.github+json' "$API/repos/$REPO/releases/latest" 2>/dev/null || true)
  VERSION=$(printf '%s' "$latest" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n1)
  [ -n "$VERSION" ] || die "no published release found for $REPO (a release is published only once all of its images are built; try again in a few minutes or pass --version)"
fi
# Kept identical to internal/version.ReleasePattern by the release checks.
release_pattern='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-(alpha|beta|rc)\.(0|[1-9][0-9]*))?$'
[[ "$VERSION" =~ $release_pattern ]] || die "not a release tag: $VERSION (use vX.Y.Z or vX.Y.Z-{alpha,beta,rc}.N)"
if [[ "$VERSION" == v0.* ]]; then
  say "Initial development (0.x): not recommended for production workloads."
elif [[ "$VERSION" == *-* ]]; then
  say "Prerelease: not recommended for production workloads."
fi
if [[ "$VERSION" == *-* ]]; then
  say "This is a prerelease of ${VERSION%%-*}."
fi
if [[ "$VERSION" == v0.* || "$VERSION" == *-* ]]; then
  say "Breaking changes and data loss are possible. No automated backups or rollback."
  say "Managed WordPress/plugin security updates are planned, not available yet."
fi
ARCHIVE="pressod_${VERSION}_linux_${ARCH}.tar.gz"
SUMS="pressod_${VERSION}_checksums.txt"
BASE="https://github.com/$REPO/releases/download/$VERSION"
if [ -x "$BIN" ] && [ "$("$BIN" version 2>/dev/null || true)" = "$VERSION" ]; then
  ok "pressod $VERSION is already installed"
  UPGRADED=0
else
  tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT
  say "  downloading $ARCHIVE"
  curl -fsSL --retry 3 -o "$tmp/$ARCHIVE" "$BASE/$ARCHIVE" || die "download failed: $BASE/$ARCHIVE"
  curl -fsSL --retry 3 -o "$tmp/$SUMS" "$BASE/$SUMS" || die "download failed: $BASE/$SUMS"
  (cd "$tmp" && grep " [*]\{0,1\}$ARCHIVE\$" "$SUMS" | sha256sum -c --quiet -) || die "checksum verification failed for $ARCHIVE"
  tar -xzf "$tmp/$ARCHIVE" -C "$tmp" pressod
  got=$("$tmp/pressod" version 2>/dev/null || true)
  [ "$got" = "$VERSION" ] || die "downloaded binary reports '$got', expected $VERSION"
  install -m 0755 "$tmp/pressod" "$BIN.new"
  [ -x "$BIN" ] && cp -f "$BIN" "$BIN.previous"
  mv -f "$BIN.new" "$BIN"
  ok "installed pressod $VERSION to $BIN"
  UPGRADED=1
fi

# --- configuration -----------------------------------------------------------
step "Configuration"
mkdir -p /etc/presso "$DATA_DIR"; chmod 0750 "$DATA_DIR"
if [ "$FRESH" = 1 ]; then
  if [ -z "$PANEL_DOMAIN" ]; then
    say "  A panel domain lets the edge serve the admin UI over HTTPS (DNS must point here)."
    ask PANEL_DOMAIN "Panel domain (empty: reach the panel through an SSH tunnel)" ""
  fi
  PANEL_DOMAIN=$(printf '%s' "$PANEL_DOMAIN" | tr '[:upper:]' '[:lower:]')
  if [ -n "$PANEL_DOMAIN" ] && [ -z "$ACME_EMAIL" ]; then
    ask ACME_EMAIL "E-mail for Let's Encrypt certificate notices (optional)" ""
  fi
  secret=$(head -c 32 /dev/urandom | base64 | tr -d '\n')
  umask 077
  cat >"$ENV_FILE" <<EOT
# presso engine configuration (systemd EnvironmentFile). Every setting is
# documented in .env.example of the repository. AUTH_SECRET encrypts the
# database/Valkey and per-site secrets at rest: never rotate it casually.
DATA_DIR="$DATA_DIR"
HTTP_ADDR="127.0.0.1:$PORT"
INTERNAL_HTTPS_ADDR=":7081"
ENGINE_INTERNAL_URL="https://host.docker.internal:7081"
RUNTIME_LEGACY_ENABLED="true"
AUTH_SECRET="$secret"
LOG_FORMAT="json"
PANEL_DOMAIN="$PANEL_DOMAIN"
ACME_EMAIL="$ACME_EMAIL"
EOT
  umask 022
  ok "wrote $ENV_FILE"
else
  ok "keeping the existing $ENV_FILE"
  PANEL_DOMAIN=$(sed -n 's/^PANEL_DOMAIN="\{0,1\}\([^"]*\)"\{0,1\}$/\1/p' "$ENV_FILE" | head -n1)
fi

# --- service -----------------------------------------------------------------
step "Service"
cat >"$UNIT" <<EOT
[Unit]
Description=presso - WordPress hosting engine
Documentation=https://github.com/$REPO
Wants=network-online.target
After=network-online.target${DOCKER_UNIT:+ $DOCKER_UNIT}
${DOCKER_UNIT:+Requires=$DOCKER_UNIT}

[Service]
Type=simple
EnvironmentFile=$ENV_FILE
ExecStart=$BIN serve
# The engine re-execs itself after a self-update; if that ever fails it exits
# non-zero and systemd brings the installed binary up.
Restart=always
RestartSec=2
KillSignal=SIGTERM
TimeoutStopSec=30
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
EOT
systemctl daemon-reload
if systemctl is-active --quiet presso.service; then
  if [ "$UPGRADED" = 1 ]; then systemctl restart presso.service; ok "service restarted"; else ok "service running"; fi
else
  systemctl enable --now presso.service >/dev/null 2>&1 || systemctl start presso.service
  ok "service enabled and started"
fi
for _ in $(seq 1 60); do
  if curl -fsS "http://127.0.0.1:$PORT/healthz" >/dev/null 2>&1; then break; fi
  sleep 1
done
curl -fsS "http://127.0.0.1:$PORT/healthz" >/dev/null 2>&1 || die "the engine did not become healthy; see: journalctl -u presso -n 50"
ok "engine healthy on 127.0.0.1:$PORT"

# --- first admin -------------------------------------------------------------
PASSWORD=""
set -a
# shellcheck source=/dev/null
. "$ENV_FILE"
set +a
if ! "$BIN" user list 2>/dev/null | grep -q '@'; then
  step "First admin account"
  if [ -z "$ADMIN_EMAIL" ]; then ask ADMIN_EMAIL "Admin e-mail" ""; fi
  [ -n "$ADMIN_EMAIL" ] || die "an admin e-mail is required (pass --email)"
  PASSWORD=$(head -c 18 /dev/urandom | base64 | tr -d '\n=/+' | cut -c1-20)
  printf '%s' "$PASSWORD" | "$BIN" user create --email "$ADMIN_EMAIL" --role admin --password-stdin >/dev/null
  ok "created admin $ADMIN_EMAIL"
fi

# --- summary -----------------------------------------------------------------
host_ip=$(hostname -I 2>/dev/null | awk '{print $1}')
say ""
say "${bold}presso $VERSION is installed.${reset}"
if [ -n "$PANEL_DOMAIN" ]; then
  say "  Panel:    https://$PANEL_DOMAIN  ${dim}(point the DNS A/AAAA record at ${host_ip:-this host}; the certificate is issued on first visit)${reset}"
  say "  Tunnel:   ssh -L $PORT:127.0.0.1:$PORT root@${host_ip:-<host>}  →  http://127.0.0.1:$PORT"
else
  say "  Panel:    ssh -L $PORT:127.0.0.1:$PORT root@${host_ip:-<host>}  →  http://127.0.0.1:$PORT"
  say "            ${dim}(set PANEL_DOMAIN in $ENV_FILE and restart to serve it over HTTPS)${reset}"
fi
if [ -n "$PASSWORD" ]; then
  say "  Login:    $ADMIN_EMAIL"
  say "  Password: $PASSWORD   ${dim}(shown once; change it under Account security)${reset}"
fi
say "  Logs:     journalctl -u presso -f"
say "  Upgrade:  System → Updates in the panel, or re-run this script"
say "  Rollback: $BIN.previous is the last binary; or: install.sh --version vX.Y.Z"
