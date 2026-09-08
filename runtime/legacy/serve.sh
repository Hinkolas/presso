#!/usr/bin/env bash
# presso-serve, FPM flavor: PID 1 of the container, running as `web` (the
# entrypoint drops privileges before exec'ing this). Supervises php-fpm and
# nginx: both are started in the foreground, a TERM/INT/QUIT from the engine
# turns into a graceful QUIT for both and exit 0, and if either master dies on its
# own the other is stopped and the container exits non-zero so the restart
# policy kicks in and the engine sees `exited` with logs.
set -uo pipefail

log() {
	# One JSON line per event on stderr, same stream as PHP errors.
	printf '{"presso":"%s"%s}\n' "$1" "${2:-}" >&2
}

fpm= nginx= stopping=0
on_stop() {
	stopping=1
	kill -QUIT ${fpm:+"$fpm"} ${nginx:+"$nginx"} 2>/dev/null
}
trap on_stop TERM INT QUIT

# Per-site ini only for php-fpm: WP-CLI (exec'ed by the engine, not a child
# of this shell) keeps php.ini + presso.ini.
PHP_INI_SCAN_DIR=/usr/local/etc/php/conf.d:/run/presso/php \
	php-fpm --nodaemonize --fpm-config /run/presso/php-fpm.conf &
fpm=$!

# nginx would only log upstream errors until the socket exists.
for _ in $(seq 1 100); do
	[ -S /tmp/presso/php-fpm.sock ] && break
	kill -0 "$fpm" 2>/dev/null || break
	sleep 0.1
done

if [ "$stopping" -eq 0 ]; then
	nginx -c /run/presso/nginx.conf -g 'daemon off;' &
	nginx=$!
fi

# Returns when the first child exits (or a trap fires).
wait -n
if [ "$stopping" -eq 1 ]; then
	wait
	exit 0
fi

# A child died without being asked to.
dead=php-fpm; status=0
if kill -0 "$fpm" 2>/dev/null; then
	dead=nginx
	wait "$nginx"; status=$?
else
	wait "$fpm"; status=$?
fi
log child_exited "$(printf ',"child":"%s","status":%d' "$dead" "$status")"
kill -QUIT ${fpm:+"$fpm"} ${nginx:+"$nginx"} 2>/dev/null
wait
[ "$status" -ne 0 ] && exit "$status"
exit 1
