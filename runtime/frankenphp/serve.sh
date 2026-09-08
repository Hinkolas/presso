#!/bin/sh
# presso-serve, FrankenPHP flavor: exec'd as `web` by the entrypoint.
exec frankenphp run --config /etc/caddy/Caddyfile --adapter caddyfile
