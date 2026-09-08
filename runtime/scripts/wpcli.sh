#!/bin/sh
# WP-CLI as a single phar. Usage: wpcli.sh <version>
set -eu
curl -fsSL -o /usr/local/bin/wp \
	"https://github.com/wp-cli/wp-cli/releases/download/v$1/wp-cli-$1.phar"
chmod 0755 /usr/local/bin/wp
wp --info --allow-root
