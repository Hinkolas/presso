# syntax=docker/dockerfile:1
# presso-runtime-base, FPM flavor (PHP 7.4 / 8.0 / 8.1, which FrankenPHP does
# not support): nginx + php-fpm under a small bash supervisor, same users,
# same paths, same env contract as the FrankenPHP base. See docs/runtime.md
# "Container structure".
#
#   docker build -f runtime/base-legacy.Dockerfile \
#     --build-arg PHP_VERSION=8.1 --build-arg PHP_PATCH=8.1.34 \
#     --build-arg DEBIAN=bookworm -t presso-runtime-base:php8.1 runtime
# DEBIAN is bullseye for 7.4 and 8.0 (no bookworm tag), bookworm for 8.1.
# PHP_PATCH is the exact release from internal/compat/matrix.json. These
# branches are end of life upstream: their final releases receive no fixes.
ARG PHP_VERSION=8.1
ARG PHP_PATCH=8.1.34
ARG DEBIAN=bookworm
ARG WPCLI_VERSION=2.12.0

FROM php:${PHP_PATCH}-fpm-${DEBIAN}

# Re-declare after FROM so the values are visible in this stage.
ARG PHP_VERSION
ARG PHP_PATCH
ARG WPCLI_VERSION

LABEL org.opencontainers.image.title="presso-runtime-base" \
      presso.runtime.php="${PHP_VERSION}" \
      presso.runtime.php.patch="${PHP_PATCH}" \
      presso.runtime.flavor="fpm"

RUN --mount=type=bind,source=scripts,target=/mnt/scripts \
    /mnt/scripts/php-pin.sh "${PHP_VERSION}" "${PHP_PATCH}"

# --- Packages -----------------------------------------------------------------
# bullseye left LTS in 2026-08: once the live mirrors stop serving it, apt
# falls back to archive.debian.org (legacy/apt-sources.sh). Must run first.
COPY --from=mlocati/php-extension-installer:2 /usr/bin/install-php-extensions /usr/local/bin/
RUN --mount=type=bind,source=legacy/apt-sources.sh,target=/mnt/apt-sources.sh \
    /mnt/apt-sources.sh

# Same extension set as the FrankenPHP base.
RUN install-php-extensions mysqli pdo_mysql gd imagick intl zip exif bcmath soap opcache redis

# nginx fronts php-fpm; gettext-base: envsubst for legacy/render.sh;
# mariadb-client: wp db export/import and the harness; util-linux: setpriv.
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      nginx mariadb-client curl less unzip ca-certificates util-linux gettext-base \
 && rm -rf /var/lib/apt/lists/* \
 # nginx opens its compiled-in error log before reading the config; point it
 # (and the access log) at the container streams so a non-root start is quiet.
 && ln -sf /dev/stdout /var/log/nginx/access.log \
 && ln -sf /dev/stderr /var/log/nginx/error.log \
 # The stock pool would try to listen on :9000 as www-data; render.sh writes
 # the only configuration php-fpm loads.
 && rm -f /usr/local/etc/php-fpm.d/*.conf

# php.ini-production as the base; runtime/php/presso.ini layers on top and
# /run/presso/php/site.ini (rendered from env at boot) applies the per-site
# values to php-fpm only.
RUN mv "$PHP_INI_DIR/php.ini-production" "$PHP_INI_DIR/php.ini"
COPY php/ /usr/local/etc/php/conf.d/

# --- WP-CLI, users ------------------------------------------------------------
RUN --mount=type=bind,source=scripts,target=/mnt/scripts \
    /mnt/scripts/wpcli.sh "${WPCLI_VERSION}" \
 && /mnt/scripts/users.sh
# WP-CLI is exec'ed as `ops`, who has no home directory.
ENV WP_CLI_CACHE_DIR=/tmp/wp-cli-cache

# --- Web server ---------------------------------------------------------------
COPY legacy/nginx.conf.tmpl legacy/php-fpm.conf.tmpl legacy/site.ini.tmpl /etc/presso/
COPY --chmod=0755 legacy/render.sh /etc/presso/entrypoint.d/10-render.sh
COPY --chmod=0755 legacy/serve.sh /usr/local/bin/presso-serve

# Parity with `frankenphp validate`: templates that render into something
# nginx or php-fpm reject should fail the build, not the container. Rendered
# with the env defaults, tested as `web` like at runtime (render.sh runs
# `nginx -t` itself), then removed. php-fpm's error_log is /proc/self/fd/2,
# which a non-root process cannot reopen inside a build step, hence the file.
RUN mkdir -p /tmp/presso && chown web:web /tmp/presso \
 && bash /etc/presso/entrypoint.d/10-render.sh \
 && setpriv --reuid=web --regid=web --init-groups bash -c \
      'PHP_INI_SCAN_DIR=/usr/local/etc/php/conf.d:/run/presso/php \
       php-fpm -tt -y /run/presso/php-fpm.conf >/tmp/presso/fpm-test.log 2>&1; \
       rc=$?; cat /tmp/presso/fpm-test.log; exit $rc' \
 && rm -rf /run/presso /tmp/presso

ENV PRESSO_FLAVOR=fpm
# php:*-fpm declares SIGQUIT; presso-serve handles TERM like the FrankenPHP base.
STOPSIGNAL SIGTERM
EXPOSE 8080
WORKDIR /var/www/html

# This base is published independently of the site image.
COPY mu-plugins/LICENSE mu-plugins/NOTICE /usr/share/licenses/presso/
