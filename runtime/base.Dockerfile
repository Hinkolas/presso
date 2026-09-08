# syntax=docker/dockerfile:1
# presso-runtime-base, FrankenPHP flavor (PHP >= 8.2): everything a site
# image needs except WordPress core and the runtime files, so the 72 per-tag
# images (runtime/Dockerfile) are thin layers on top. Classic mode, plain HTTP
# on :8080. See RUNTIME.md "Container structure".
#
#   docker build -f runtime/base.Dockerfile --build-arg PHP_VERSION=8.4 \
#     -t presso-runtime-base:php8.4 runtime
ARG PHP_VERSION=8.4
ARG WPCLI_VERSION=2.12.0

FROM dunglas/frankenphp:1-php${PHP_VERSION}

# Re-declare after FROM so the values are visible in this stage.
ARG PHP_VERSION
ARG WPCLI_VERSION

LABEL org.opencontainers.image.title="presso-runtime-base" \
      presso.runtime.php="${PHP_VERSION}" \
      presso.runtime.flavor="frankenphp"

# --- PHP extensions WordPress needs ------------------------------------------
# opcache ships compiled-but-disabled in the official PHP images; listing it
# here enables it. redis is the client for the object-cache drop-in.
RUN install-php-extensions mysqli pdo_mysql gd imagick intl zip exif bcmath soap opcache redis

# mariadb-client: wp db export/import and the harness; util-linux: setpriv,
# which the entrypoint uses to drop from root to `web` before exec'ing.
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      mariadb-client curl less unzip ca-certificates util-linux \
 && rm -rf /var/lib/apt/lists/*

# php.ini-production as the base; runtime/php/presso.ini layers on top and the
# Caddyfile applies the per-site (env driven) values for web requests.
RUN mv "$PHP_INI_DIR/php.ini-production" "$PHP_INI_DIR/php.ini"
COPY php/ /usr/local/etc/php/conf.d/

# --- WP-CLI, users ------------------------------------------------------------
RUN --mount=type=bind,source=scripts,target=/mnt/scripts \
    /mnt/scripts/wpcli.sh "${WPCLI_VERSION}" \
 && /mnt/scripts/users.sh
# WP-CLI is exec'ed as `ops`, who has no home directory.
ENV WP_CLI_CACHE_DIR=/tmp/wp-cli-cache

# --- Non-root FrankenPHP ----------------------------------------------------
# We listen on 8080, so the binary needs no CAP_NET_BIND_SERVICE. Caddy keeps
# its runtime state under XDG dirs that the base image points at /data and
# /config; those must be writable by the user that runs it.
RUN setcap -r /usr/local/bin/frankenphp \
 && chown -R web:web /data/caddy /config/caddy

# --- Web server ---------------------------------------------------------------
COPY Caddyfile /etc/caddy/Caddyfile
# A Caddyfile that does not parse should fail the build, not the container.
RUN frankenphp validate --config /etc/caddy/Caddyfile --adapter caddyfile
COPY --chmod=0755 frankenphp/serve.sh /usr/local/bin/presso-serve

ENV PRESSO_FLAVOR=frankenphp \
    SERVER_NAME=:8080
EXPOSE 8080
WORKDIR /var/www/html
