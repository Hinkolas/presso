# The shared edge: Caddy with the Souin cache handler (page cache with
# surrogate-key purging) and its Badger storage backend. pressod pushes the
# full Caddyfile through the admin API; the image ships no site config.
# Souin storage backends must be compiled in explicitly (>= v1.7).
FROM caddy:2-builder AS builder
RUN xcaddy build \
    --with github.com/darkweak/souin/plugins/caddy \
    --with github.com/darkweak/storages/badger/caddy

FROM caddy:2
COPY --from=builder /usr/bin/caddy /usr/bin/caddy
# The engine's bootstrap: admin API reachable on the published port until
# the real config arrives; --resume restores the last pushed config after a
# restart (persisted in the /config volume).
ENV CADDY_ADMIN=0.0.0.0:2019
VOLUME ["/data", "/config", "/cache"]
EXPOSE 80 443 443/udp 2019 2020
CMD ["caddy", "run", "--resume", "--config", "/etc/caddy/Caddyfile", "--adapter", "caddyfile"]
