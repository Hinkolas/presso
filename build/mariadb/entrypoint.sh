#!/bin/sh
# presso-mariadb entrypoint: prepares the data directory, then runs mariadbd.
# Implements the parts of the official image's contract pressod relies on:
#   MARIADB_ROOT_PASSWORD  sets root@localhost and root@'%' on first start
#   MARIADB_AUTO_UPGRADE   runs mariadb-upgrade when the data directory was
#                          last upgraded by a different server version
# Arguments starting with "-" are mariadbd options. Started as root, it only
# hands the data directory to mysql (a fresh tmpfs or bind mount is owned by
# root) and re-executes itself as mysql; everything else runs as mysql.
#
# The health probe logs in as 'mysql'@'localhost' over the socket
# (unix_socket, no privileges). A data directory this image did not create
# (for example one from the official image) gets that account once, on a
# socket-only server without grant checks, like the upgrade.
set -eu

datadir=/var/lib/mysql
socket=/run/mysqld/mysqld.sock
jemalloc=/usr/lib/libjemalloc.so.2
# Marks a data directory that has the probe account.
marker=$datadir/.presso-mariadb
# Exists while the first start sets up the data directory.
incomplete=$datadir/.presso-initializing

case "${1:-}" in
'' | -*) set -- mariadbd "$@" ;;
esac
if [ "$1" != mariadbd ] && [ "$1" != mysqld ]; then
	exec "$@"
fi

log() { printf '%s [presso] %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*" >&2; }

if [ "$(id -u)" = 0 ]; then
	for dir in "$datadir" /run/mysqld; do
		if [ "$(stat -c %u "$dir")" != 999 ]; then
			chown -R mysql:mysql "$dir"
		fi
	done
	exec su-exec mysql "$0" "$@"
fi

server_version() { mariadbd --version | awk '{print $3}'; }

# A server reachable only through the socket, for setup and upgrades.
temp_start() {
	LD_PRELOAD=$jemalloc mariadbd --skip-networking --socket="$socket" \
		--loose-innodb-buffer-pool-dump-at-shutdown=0 "$@" &
	temp_pid=$!
	i=0
	until mariadb-admin --socket="$socket" --silent ping >/dev/null 2>&1; do
		if ! kill -0 "$temp_pid" 2>/dev/null; then
			log "temporary server exited during startup"
			exit 1
		fi
		i=$((i + 1))
		if [ "$i" -ge 120 ]; then
			log "temporary server did not start within 60s"
			exit 1
		fi
		sleep 0.5
	done
}

# SIGTERM is a clean shutdown and needs no account, which matters because
# mariadb-upgrade and FLUSH PRIVILEGES re-enable grant checks.
temp_stop() {
	kill -TERM "$temp_pid"
	wait "$temp_pid"
}

# Escapes a value for a single-quoted SQL string literal.
sql_quote() { printf '%s' "$1" | sed -e 's/\\/\\\\/g' -e "s/'/\\\\'/g"; }

if [ -e "$incomplete" ]; then
	log "an earlier initialization of $datadir did not finish; remove the data volume to start over"
	exit 1
fi

if [ ! -d "$datadir/mysql" ]; then
	if [ -z "${MARIADB_ROOT_PASSWORD:-}" ]; then
		log "MARIADB_ROOT_PASSWORD is required to initialize an empty data directory"
		exit 1
	fi
	log "initializing $datadir"
	: >"$incomplete"
	# --user=mysql adds 'mysql'@'localhost' (unix_socket), used for setup and
	# reduced to the probe account afterwards.
	mariadb-install-db --user=mysql --datadir="$datadir" --skip-test-db \
		--auth-root-authentication-method=socket >/dev/null
	temp_start
	pw=$(sql_quote "$MARIADB_ROOT_PASSWORD")
	# Read from stdin so the password never appears in a process listing.
	mariadb --socket="$socket" <<-SQL
		DELETE FROM mysql.global_priv WHERE User = '';
		ALTER USER 'root'@'localhost' IDENTIFIED VIA unix_socket OR mysql_native_password USING PASSWORD('$pw');
		CREATE USER 'root'@'%' IDENTIFIED BY '$pw';
		GRANT ALL PRIVILEGES ON *.* TO 'root'@'%' WITH GRANT OPTION;
		REVOKE ALL PRIVILEGES, GRANT OPTION FROM 'mysql'@'localhost';
		DELETE FROM mysql.proxies_priv WHERE User = 'mysql';
		FLUSH PRIVILEGES;
	SQL
	temp_stop
	server_version >"$datadir/mariadb_upgrade_info"
	: >"$marker"
	rm "$incomplete"
	log "initialized"
else
	current=$(server_version)
	previous=$(cat "$datadir/mariadb_upgrade_info" "$datadir/mysql_upgrade_info" 2>/dev/null | head -n 1 || true)
	upgrade=false
	if [ "${MARIADB_AUTO_UPGRADE:-0}" != 0 ] && [ "$previous" != "$current" ]; then
		upgrade=true
	fi
	# Each step gets its own server: both end with grant checks enabled.
	if [ "$upgrade" = true ]; then
		log "upgrading data directory from ${previous:-an unknown version} to $current"
		temp_start --skip-grant-tables
		mariadb-upgrade --socket="$socket" --upgrade-system-tables --silent
		temp_stop
		server_version >"$datadir/mariadb_upgrade_info"
		log "upgraded"
	fi
	if [ ! -e "$marker" ]; then
		log "adding the health probe account"
		temp_start --skip-grant-tables
		# FLUSH PRIVILEGES loads the grant tables so account statements work.
		mariadb --socket="$socket" <<-SQL
			FLUSH PRIVILEGES;
			CREATE USER IF NOT EXISTS 'mysql'@'localhost' IDENTIFIED VIA unix_socket;
		SQL
		temp_stop
		: >"$marker"
	fi
fi

unset MARIADB_ROOT_PASSWORD
LD_PRELOAD=$jemalloc
export LD_PRELOAD
exec "$@"
