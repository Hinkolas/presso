#!/usr/bin/env bash
# Smoke test for a built runtime image, either flavor. Usage: smoke.sh <image>
# Starts the container without a database (health.php skips the DB probe when
# PRESSO_DB_HOST is unset), waits for Docker's health check, then checks the
# routing rules both web servers must agree on, that nothing runs as root,
# and that a stop is graceful and quick. Used by CI and `task` alike.
set -euo pipefail

image=$1
name="presso-smoke-$$"
fail() { echo "smoke: FAIL: $*" >&2; docker logs "$name" >&2 || true; exit 1; }
cleanup() { docker rm -f "$name" >/dev/null 2>&1 || true; }
trap cleanup EXIT

docker run -d --name "$name" -p 127.0.0.1:0:8080 --tmpfs /tmp \
	-e PRESSO_SITE_URL=http://localhost:8080 "$image" >/dev/null
port=$(docker port "$name" 8080/tcp | head -n1 | sed 's/.*://')
base="http://127.0.0.1:$port"
flavor=$(docker inspect -f '{{index .Config.Labels "presso.runtime.flavor"}}' "$name")
echo "smoke: $image flavor=$flavor port=$port"

for _ in $(seq 1 60); do
	status=$(docker inspect -f '{{.State.Health.Status}}' "$name")
	[ "$status" = healthy ] && break
	[ "$status" = unhealthy ] && fail "container unhealthy"
	sleep 1
done
[ "$status" = healthy ] || fail "not healthy after 60s ($status)"

expect() { # method path want-code [want-body]
	local got opt=()
	[ "$1" = HEAD ] && opt=(-I)
	got=$(curl -s -m 10 ${opt[@]+"${opt[@]}"} -o /tmp/smoke-body -w '%{http_code}' "$base$2")
	[ "$got" = "$3" ] || fail "$1 $2: want $3, got $got"
	if [ $# -gt 3 ]; then
		[ "$(cat /tmp/smoke-body)" = "$4" ] || fail "$1 $2: body $(head -c 64 /tmp/smoke-body)"
	fi
	echo "smoke: ok $1 $2 -> $3"
}

expect GET /-/ping 200 ok
if [ "$flavor" = fpm ]; then expect GET /-/fpm-ping 200 pong; fi
expect HEAD /wp-config.php 404
expect HEAD /wp-content/debug.log 404
expect HEAD /.git/config 404
# WordPress itself: no database yet, so the install screen or an error page
# rendered by PHP; anything but an nginx/Caddy-level failure.
code=$(curl -s -m 30 -o /dev/null -w '%{http_code}' "$base/")
case "$code" in 200|302|500|503) echo "smoke: ok GET / -> $code (php ran)";; *) fail "GET /: $code";; esac

docker exec "$name" sh -c 'mkdir -p /var/www/html/wp-content/uploads && for f in x.php x.PHP x.phtml; do echo "<?php echo 1;" > /var/www/html/wp-content/uploads/$f; done'
expect GET /wp-content/uploads/x.php 403
expect GET /wp-content/uploads/x.PHP 403
expect GET /wp-content/uploads/x.phtml 403
expect GET /wp-content/uploads/x.php/a.png 403

# Everything after the entrypoint runs as web (uid 1000); the probe itself
# runs as web too so it does not show up as root.
uids=$(docker exec -u 1000 "$name" sh -c 'for p in /proc/[0-9]*; do awk "/^Uid:/{print \$2}" "$p/status" 2>/dev/null; done | sort -u | tr "\n" " "')
[ "$uids" = "1000 " ] || fail "process uids: $uids"
echo "smoke: ok all processes uid 1000"

# The env contract must reach PHP through the web server (php-fpm needs
# clear_env=no for that). /proc/<pid>/environ is unreadable for setpriv'd
# processes without CAP_SYS_PTRACE, so ask PHP itself via a temporary file.
docker exec "$name" sh -c 'echo "<?php echo getenv(\"PRESSO_SITE_URL\");" > /var/www/html/presso-smoke-env.php'
expect GET /presso-smoke-env.php 200 http://localhost:8080
docker exec "$name" rm -f /var/www/html/presso-smoke-env.php

docker exec "$name" sh -c 'for f in /opt/presso/health.php /var/www/html/wp-config.php $(find /opt/presso/mu-plugins -name "*.php"); do php -l "$f" >/dev/null || exit 1; done'
echo "smoke: ok php -l"

start=$(date +%s)
docker stop "$name" >/dev/null
code=$(docker wait "$name")
took=$(( $(date +%s) - start ))
[ "$code" = 0 ] || fail "stop exit code $code"
[ "$took" -le 10 ] || fail "stop took ${took}s"
echo "smoke: ok stop exit 0 in ${took}s"
