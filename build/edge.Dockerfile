# The shared edge: Caddy with the Souin cache handler (page cache with
# surrogate-key purging) and its Badger storage backend. pressod pushes the
# full Caddyfile through the admin API; the image ships no site config.
# Souin storage backends must be compiled in explicitly (>= v1.7).
#
# The builder runs on the build platform and cross-compiles for the target
# (xcaddy honours GOOS/GOARCH), so the multi-arch release build does not run
# the Go toolchain under emulation.
FROM --platform=$BUILDPLATFORM caddy:2-builder AS builder
ARG TARGETOS
ARG TARGETARCH
RUN XCADDY_SKIP_CLEANUP=1 GOOS=$TARGETOS GOARCH=$TARGETARCH xcaddy build \
    --with github.com/darkweak/souin/plugins/caddy \
    --with github.com/darkweak/storages/badger/caddy \
 && mkdir -p /edge-build \
 && cp /tmp/buildenv_*/main.go /tmp/buildenv_*/go.mod /tmp/buildenv_*/go.sum /edge-build/

FROM caddy:2
RUN apk add --no-cache socat
COPY Caddyfile /etc/caddy/Caddyfile
COPY LICENSE NOTICE /usr/share/licenses/presso/
COPY --from=builder /usr/bin/caddy /usr/bin/caddy
COPY --from=builder /edge-build/ /usr/share/presso/build/edge/
# Container-loopback administration; every boot serves an unavailable response
# until the engine reconciles the current site state.
ENV CADDY_ADMIN=127.0.0.1:2019
VOLUME ["/data", "/config", "/cache"]
EXPOSE 80 443 443/udp
CMD ["caddy", "run", "--config", "/etc/caddy/Caddyfile", "--adapter", "caddyfile"]
