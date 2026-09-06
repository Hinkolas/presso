#!/usr/bin/env bash
# Container entrypoint, runs as root. Prepares the wp-content volume (seed on
# first boot, drop-in refresh, ownership per the two-user model in RUNTIME.md
# section 8), then drops privileges and exec's FrankenPHP as `web`.
#
# Ownership work is guarded so that restarts stay fast on large volumes: a
# recursive chown of uploads/ only happens when the top-level directory is
# not owned by web yet (first boot or a volume imported from elsewhere).
set -euo pipefail

WP_CONTENT=/var/www/html/wp-content
SEED=/opt/presso/wp-content-seed
DROPINS=/opt/presso/dropins

log() {
	# One JSON line per event on stderr, same stream as PHP errors.
	printf '{"presso":"%s"%s}\n' "$1" "${2:-}" >&2
}

# --- Seed an empty volume ---------------------------------------------------
if [ ! -d "$WP_CONTENT/themes" ]; then
	cp -a "$SEED/." "$WP_CONTENT/"
	log seeded_wp_content
fi

# mu-plugins stays empty (WPMU_PLUGIN_DIR points into the image) but tools
# expect the stock layout.
for d in uploads cache upgrade languages plugins themes mu-plugins; do
	mkdir -p "$WP_CONTENT/$d"
done

# --- Object cache drop-in ---------------------------------------------------
# Refreshed on every start so a new image tag always wins; removed when the
# site has no Redis so WordPress falls back to its in-memory cache.
if [ -n "${PRESSO_REDIS_HOST:-}" ]; then
	cp "$DROPINS/object-cache.php" "$WP_CONTENT/object-cache.php"
	chown root:web "$WP_CONTENT/object-cache.php"
	chmod 0644 "$WP_CONTENT/object-cache.php"
else
	rm -f "$WP_CONTENT/object-cache.php"
fi

# --- Ownership --------------------------------------------------------------
# The volume root: ops and root may add drop-ins; web may not create anything
# here (no debug.log, no advanced-cache.php).
chown ops:web "$WP_CONTENT"
chmod 0755 "$WP_CONTENT"

# Directories carry the setgid bit (2xxx) so whatever ops or web creates
# inherits group web straight away instead of waiting for the next boot.

# Web-writable data: uploads, cache, upgrade. ops writes via the web group.
for d in uploads cache upgrade; do
	dir="$WP_CONTENT/$d"
	if [ "$(stat -c '%U:%G' "$dir")" != "web:web" ]; then
		chown -R web:web "$dir"
		find "$dir" -type d -exec chmod 2775 {} +
		find "$dir" -type f -exec chmod 0664 {} +
		log chowned "$(printf ',"dir":"%s"' "$d")"
	fi
done

# Code: plugins, themes, languages (and the unused mu-plugins) belong to ops.
# web reads only, unless the site's file-mod policy is OPEN, in which case
# the web group may write too (the escape hatch for plugins that write into
# their own directory).
if [ "${PRESSO_FILE_MODS:-locked}" = "open" ]; then
	DMODE=2775; FMODE=0664
else
	DMODE=2755; FMODE=0644
fi
for d in plugins themes languages mu-plugins; do
	dir="$WP_CONTENT/$d"
	# stat-only scans: chown/chmod run only on entries that differ.
	find "$dir" \( ! -user ops -o ! -group web \) -exec chown ops:web {} +
	find "$dir" -type d ! -perm "$DMODE" -exec chmod "$DMODE" {} +
	find "$dir" -type f ! -perm "$FMODE" -exec chmod "$FMODE" {} +
done

# The only scratch space for web besides uploads/cache (php/presso.ini).
mkdir -p /tmp && chmod 1777 /tmp

# --- Start ------------------------------------------------------------------
log boot "$(printf ',"php":"%s","wp":"%s","site":"%s"' \
	"$(php -r 'echo PHP_VERSION;')" \
	"$(cat /opt/presso/wp-version 2>/dev/null || echo unknown)" \
	"${PRESSO_SITE_ID:-}")"

# PRESSO_CLI=1 is set by the engine when it exec's WP-CLI; nothing to do here.
exec setpriv --reuid=web --regid=web --init-groups \
	frankenphp run --config /etc/caddy/Caddyfile --adapter caddyfile
