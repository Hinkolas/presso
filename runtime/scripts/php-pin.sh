#!/bin/sh
# Fails the build unless the image's PHP is exactly the pinned release.
# Usage: php-pin.sh <branch> <patch>, e.g. php-pin.sh 8.5 8.5.12
set -eu
case "$2" in
"$1".*) ;;
*)
	echo "php-pin: PHP_PATCH $2 is not a $1 release" >&2
	exit 1
	;;
esac
actual=$(php -r 'echo PHP_VERSION;')
if [ "$actual" != "$2" ]; then
	echo "php-pin: base image ships PHP $actual, expected $2" >&2
	exit 1
fi
echo "php-pin: PHP $actual"
