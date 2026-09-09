# syntax=docker/dockerfile:1
# presso-runtime: one WordPress site per container. The thin per-tag image:
# a shared base (base.Dockerfile for FrankenPHP, base-legacy.Dockerfile for
# nginx + PHP-FPM) plus one WordPress core version and the runtime files.
# Core is baked in and read-only; wp-content is a volume seeded on first
# boot; everything per-site arrives as environment variables read by the
# static wp-config.php. See docs/runtime.md.
#
#   docker build --build-arg BASE_IMAGE=presso-runtime-base:php8.4 \
#     --build-arg WP_VERSION=6.8.8 -t presso-runtime:php8.4-wp6.8.8 runtime
ARG BASE_IMAGE=presso-runtime-base:php8.4

FROM ${BASE_IMAGE}

# Always a pinned release (the compat matrix owns "latest"), so a rebuild of
# the same tag yields the same core.
ARG WP_VERSION

LABEL org.opencontainers.image.title="presso-runtime" \
      presso.runtime.wp="${WP_VERSION}"

# --- WordPress core, root-owned, read-only for both users -------------------
RUN --mount=type=bind,source=scripts,target=/mnt/scripts \
    test -n "${WP_VERSION}" \
 && /mnt/scripts/wordpress.sh "${WP_VERSION}"

# --- Runtime files ------------------------------------------------------------
COPY --chmod=0644 wp-config.php /var/www/html/wp-config.php
COPY mu-plugins/ /opt/presso/mu-plugins/
COPY dropins/ /opt/presso/dropins/
COPY health.php /opt/presso/health.php
COPY --chmod=0755 entrypoint.sh /usr/local/bin/presso-entrypoint

VOLUME /var/www/html/wp-content

HEALTHCHECK --interval=15s --timeout=5s --start-period=30s --retries=3 \
  CMD ["php", "/opt/presso/health.php"]

# Runs as root, fixes up the volume, then drops to `web` via setpriv.
ENTRYPOINT ["presso-entrypoint"]
