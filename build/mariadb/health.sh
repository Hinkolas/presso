#!/bin/sh
# Health probe, also installed as healthcheck.sh with the official image's
# option names for the checks pressod uses:
#   --connect             the server listens on TCP port 3306 and answers
#   --innodb_initialized  InnoDB has finished starting
# Queries go over the socket as 'mysql'@'localhost' (unix_socket, no
# privileges); the TCP check reads the kernel's socket table, so a probe
# never shows up as a failed login.
set -eu
# The socket account is the server's own user; docker exec defaults to root.
if [ "$(id -u)" = 0 ]; then
	exec su-exec mysql "$0" "$@"
fi
socket=/run/mysqld/mysqld.sock
query() { mariadb --socket="$socket" --batch --skip-column-names -e "$1"; }
for check in "$@"; do
	case "$check" in
	--connect)
		# 0CEA is port 3306; state 0A is LISTEN.
		grep -qs ':0CEA 0\{8,32\}:0000 0A ' /proc/net/tcp /proc/net/tcp6
		[ "$(query 'SELECT 1')" = 1 ]
		;;
	--innodb_initialized)
		[ "$(query "SELECT 1 FROM information_schema.ENGINES WHERE ENGINE = 'InnoDB' AND SUPPORT IN ('YES', 'DEFAULT')")" = 1 ]
		;;
	*)
		echo "presso-mariadb-health: unsupported check $check" >&2
		exit 2
		;;
	esac
done
