#!/usr/bin/env bash
# Smoke-tests a built platform image before its release is published:
#   build/smoke.sh <edge|mariadb|valkey|browser> <image>
# Each check starts the image the way pressod does and talks to it through
# the same interface (admin modules, SQL, the Valkey protocol, DevTools).
set -euo pipefail
kind=$1
image=$2
name=presso-smoke-$kind-$$
trap 'docker rm -f "$name" >/dev/null 2>&1 || true' EXIT

retry() {
	for _ in $(seq 60); do
		if "$@" >/dev/null 2>&1; then return 0; fi
		sleep 1
	done
	"$@"
}

case "$kind" in
edge)
	docker run --rm "$image" caddy version
	docker run --rm "$image" caddy list-modules | grep -q '^http.handlers.cache$'
	;;
valkey)
	docker run -d --name "$name" "$image" valkey-server --requirepass smoke --save '' --appendonly no >/dev/null
	retry docker exec "$name" valkey-cli -a smoke --no-auth-warning ping
	docker exec "$name" valkey-cli -a smoke --no-auth-warning ACL SETUSER smoke on '>pw' '~smoke:*' '+@all' | grep -q OK
	docker exec "$name" valkey-cli -a smoke --no-auth-warning INFO server | grep -m1 '^valkey_version:'
	;;
mariadb)
	docker run -d --name "$name" -e MARIADB_ROOT_PASSWORD=smoke -e MARIADB_AUTO_UPGRADE=1 "$image" \
		--character-set-server=utf8mb4 --collation-server=utf8mb4_unicode_ci >/dev/null
	retry docker exec "$name" healthcheck.sh --connect --innodb_initialized
	docker exec "$name" mariadb --protocol=tcp -h127.0.0.1 -uroot -psmoke -N -e 'SELECT VERSION(), @@character_set_server'
	# A restart must keep the data directory and credentials.
	docker restart "$name" >/dev/null
	retry docker exec "$name" healthcheck.sh --connect --innodb_initialized
	docker exec "$name" mariadb --protocol=tcp -h127.0.0.1 -uroot -psmoke -N -e 'SELECT 1' | grep -q 1
	;;
browser)
	docker run -d --name "$name" "$image" --no-sandbox --use-gl=angle --use-angle=swiftshader \
		--remote-debugging-address=127.0.0.1 --remote-debugging-port=9223 >/dev/null
	devtools() {
		docker exec "$name" sh -c '(printf "GET /json/version HTTP/1.1\r\nHost: 127.0.0.1:9223\r\n\r\n"; sleep 1) | socat - TCP4:127.0.0.1:9223,connect-timeout=5' | grep '"Browser"'
	}
	retry devtools
	;;
*)
	echo "smoke: unknown image kind $kind" >&2
	exit 2
	;;
esac
echo "smoke: $kind ok ($image)"
