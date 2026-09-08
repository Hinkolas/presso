#!/usr/bin/env bash
# Entrypoint hook (FPM flavor), sourced as root before privileges drop:
# renders nginx.conf, php-fpm.conf and php/site.ini from the same PHP_* env
# the Caddyfile reads, with the same defaults, into /run/presso (root-owned,
# so web cannot rewrite loaded config) and tests the nginx config so a bad
# value fails the boot loudly. Also run standalone at build time.
set -euo pipefail

presso_render() {
	local out=/run/presso tmpl=/etc/presso
	local mem="${PHP_MEMORY_LIMIT:-256M}" upload="${PHP_UPLOAD_MAX_FILESIZE:-64M}" post="${PHP_POST_MAX_SIZE:-64M}"
	local exec_time="${PHP_MAX_EXECUTION_TIME:-120}" input_vars="${PHP_MAX_INPUT_VARS:-3000}"
	local opcache_mem="${PHP_OPCACHE_MEMORY:-128}" revalidate="${PHP_OPCACHE_REVALIDATE_FREQ:-60}" tz="${PHP_TIMEZONE:-UTC}"

	# PHP shorthand (64M, 1G, plain bytes) to bytes, so the two limits compare.
	to_bytes() {
		local v="${1%%[!0-9]*}" unit="${1#"${1%%[!0-9]*}"}"
		case "${unit,,}" in
			k) echo $((v << 10)) ;;
			m) echo $((v << 20)) ;;
			g) echo $((v << 30)) ;;
			*) echo "$v" ;;
		esac
	}
	local body
	body=$(to_bytes "$upload")
	if [ "$(to_bytes "$post")" -gt "$body" ]; then body=$(to_bytes "$post"); fi

	# Worker count: PHP_WORKERS if the engine sets it, else the container
	# memory limit (cgroup v2, then v1) at 64 MiB per worker, clamped 2..32;
	# 8 when there is no limit to read.
	local workers="${PHP_WORKERS:-}" limit=""
	if [ -z "$workers" ]; then
		if [ -r /sys/fs/cgroup/memory.max ]; then
			limit=$(cat /sys/fs/cgroup/memory.max)
		elif [ -r /sys/fs/cgroup/memory/memory.limit_in_bytes ]; then
			limit=$(cat /sys/fs/cgroup/memory/memory.limit_in_bytes)
		fi
		if [[ "$limit" =~ ^[0-9]+$ ]] && [ "${#limit}" -lt 16 ]; then
			workers=$((limit / (64 << 20)))
			[ "$workers" -lt 2 ] && workers=2
			[ "$workers" -gt 32 ] && workers=32
		else
			workers=8
		fi
	fi
	local min_spare=$(( (workers + 3) / 4 )) max_spare=$(( (workers + 1) / 2 ))

	mkdir -p "$out/php"
	local vars='${NGINX_MAX_BODY} ${NGINX_FASTCGI_TIMEOUT} ${PHP_WORKERS} ${PHP_WORKERS_START} ${PHP_WORKERS_MIN_SPARE} ${PHP_WORKERS_MAX_SPARE} ${PHP_MEMORY_LIMIT} ${PHP_UPLOAD_MAX_FILESIZE} ${PHP_POST_MAX_SIZE} ${PHP_MAX_EXECUTION_TIME} ${PHP_MAX_INPUT_VARS} ${PHP_OPCACHE_MEMORY} ${PHP_OPCACHE_REVALIDATE_FREQ} ${PHP_TIMEZONE}'
	local pair
	for pair in nginx.conf:nginx.conf php-fpm.conf:php-fpm.conf site.ini:php/site.ini; do
		env NGINX_MAX_BODY="$body" NGINX_FASTCGI_TIMEOUT="$((exec_time + 30))" \
			PHP_WORKERS="$workers" PHP_WORKERS_START="$min_spare" \
			PHP_WORKERS_MIN_SPARE="$min_spare" PHP_WORKERS_MAX_SPARE="$max_spare" \
			PHP_MEMORY_LIMIT="$mem" PHP_UPLOAD_MAX_FILESIZE="$upload" PHP_POST_MAX_SIZE="$post" \
			PHP_MAX_EXECUTION_TIME="$exec_time" PHP_MAX_INPUT_VARS="$input_vars" \
			PHP_OPCACHE_MEMORY="$opcache_mem" PHP_OPCACHE_REVALIDATE_FREQ="$revalidate" PHP_TIMEZONE="$tz" \
			envsubst "$vars" <"$tmpl/${pair%%:*}.tmpl" >"$out/${pair#*:}"
	done
	chown -R root:root "$out"
	find "$out" -type d -exec chmod 0755 {} +
	find "$out" -type f -exec chmod 0644 {} +

	# Socket, pid and temp paths; the tmpfs at /tmp is fresh on every start.
	mkdir -p /tmp/presso
	chown web:web /tmp/presso
	chmod 0755 /tmp/presso

	# Tested as web: nginx run by root would chown the temp paths to its
	# compiled-in user. It opens /dev/stdout and /dev/stderr for its logs,
	# which a non-root process can only do when fd 1/2 are something web may
	# write, hence the file (a build step's stdio is not; the container's is).
	local log=/tmp/presso/nginx-test.log
	install -o web -g web -m 0644 /dev/null "$log"
	if ! setpriv --reuid=web --regid=web --init-groups nginx -t -q -c "$out/nginx.conf" >>"$log" 2>&1; then
		cat "$log" >&2
		return 1
	fi
	rm -f "$log"
	unset -f to_bytes
}

presso_render
unset -f presso_render
