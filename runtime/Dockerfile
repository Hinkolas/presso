# presso-runtime: one WordPress site per container on FrankenPHP (classic
# mode, plain HTTP on :8080). Core is baked into the image and read-only;
# wp-content is a volume seeded on first boot; everything per-site arrives as
# environment variables read by the static wp-config.php. See RUNTIME.md.
#
# Build one image per supported PHP version:
#   docker build -t presso-runtime:php8.3 --build-arg PHP_VERSION=8.3 runtime
ARG PHP_VERSION=8.3
ARG WP_VERSION=latest
ARG WPCLI_VERSION=2.12.0

FROM dunglas/frankenphp:1-php${PHP_VERSION}

# Re-declare after FROM so the values are visible in this stage.
ARG PHP_VERSION
ARG WP_VERSION
ARG WPCLI_VERSION

LABEL org.opencontainers.image.title="presso-runtime" \
      presso.runtime.php="${PHP_VERSION}" \
      presso.runtime.wp="${WP_VERSION}"

# --- Layer 2: PHP extensions WordPress needs ------------------------------
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

# --- Layer 3: WP-CLI --------------------------------------------------------
RUN curl -fsSL -o /usr/local/bin/wp \
      "https://github.com/wp-cli/wp-cli/releases/download/v${WPCLI_VERSION}/wp-cli-${WPCLI_VERSION}.phar" \
 && chmod 0755 /usr/local/bin/wp \
 && wp --info --allow-root
# WP-CLI is exec'ed as `ops`, who has no home directory.
ENV WP_CLI_CACHE_DIR=/tmp/wp-cli-cache

# --- Users ------------------------------------------------------------------
# web (1000) runs FrankenPHP and may only write uploads/, cache/ and tmp.
# ops (1001) owns plugins/, themes/, languages/ and runs WP-CLI. ops is a
# member of web so it can read (and, via group-writable dirs, write) what web
# owns; web is deliberately NOT a member of ops.
RUN groupadd -g 1000 web \
 && useradd -u 1000 -g web -M -s /usr/sbin/nologin web \
 && groupadd -g 1001 ops \
 && useradd -u 1001 -g ops -G web -M -s /bin/bash ops

# --- Layer 4: WordPress core, root-owned, read-only for both users ----------
# The stock wp-content (default themes, akismet, hello.php, index.php) is
# moved aside as the seed for an empty volume; the real path becomes the
# mount point.
# php.ini-production's 128M is not enough to unzip core; the runtime ini
# (copied later) raises it for CLI, so pass it explicitly here.
RUN mkdir -p /var/www/html /opt/presso \
 && if [ "${WP_VERSION}" = "latest" ]; then \
      php -d memory_limit=512M /usr/local/bin/wp core download --allow-root --path=/var/www/html; \
    else \
      php -d memory_limit=512M /usr/local/bin/wp core download --allow-root --path=/var/www/html --version="${WP_VERSION}"; \
    fi \
 && wp core version --allow-root --path=/var/www/html > /opt/presso/wp-version \
 && mv /var/www/html/wp-content /opt/presso/wp-content-seed \
 && mkdir /var/www/html/wp-content \
 && rm -f /var/www/html/wp-config-sample.php /var/www/html/readme.html /var/www/html/license.txt \
 && chown -R root:root /var/www/html /opt/presso \
 && find /var/www/html /opt/presso -type d -exec chmod 0755 {} + \
 && find /var/www/html /opt/presso -type f -exec chmod 0644 {} +

# --- Non-root FrankenPHP ----------------------------------------------------
# We listen on 8080, so the binary needs no CAP_NET_BIND_SERVICE. Caddy keeps
# its runtime state under XDG dirs that the base image points at /data and
# /config; those must be writable by the user that runs it.
RUN setcap -r /usr/local/bin/frankenphp \
 && chown -R web:web /data/caddy /config/caddy

# --- Layer 5: runtime files -------------------------------------------------
COPY Caddyfile /etc/caddy/Caddyfile
COPY php/ /usr/local/etc/php/conf.d/
# A Caddyfile that does not parse should fail the build, not the container.
RUN frankenphp validate --config /etc/caddy/Caddyfile --adapter caddyfile
COPY --chmod=0644 wp-config.php /var/www/html/wp-config.php
COPY mu-plugins/ /opt/presso/mu-plugins/
COPY dropins/ /opt/presso/dropins/
COPY health.php /opt/presso/health.php
COPY --chmod=0755 entrypoint.sh /usr/local/bin/presso-entrypoint

ENV SERVER_NAME=:8080
EXPOSE 8080
WORKDIR /var/www/html
VOLUME /var/www/html/wp-content

HEALTHCHECK --interval=15s --timeout=5s --start-period=30s --retries=3 \
  CMD ["php", "/opt/presso/health.php"]

# Runs as root, fixes up the volume, then drops to `web` via setpriv.
ENTRYPOINT ["presso-entrypoint"]
