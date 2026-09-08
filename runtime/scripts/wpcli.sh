#!/bin/sh
# WP-CLI as a single phar. Usage: wpcli.sh <version>
set -eu
curl -fsSL -o /usr/local/bin/wp \
	"https://github.com/wp-cli/wp-cli/releases/download/v$1/wp-cli-$1.phar"
# The release PHAR does not retain the root WP-CLI license. Preserve the
# matching upstream notice separately; bundled dependencies need their own
# inventory as documented in LICENSE_REVIEW.md.
mkdir -p /opt/presso/licenses
curl -fsSL -o /opt/presso/licenses/wp-cli.txt \
	"https://raw.githubusercontent.com/wp-cli/wp-cli/v$1/LICENSE"
printf 'WP-CLI %s source: https://github.com/wp-cli/wp-cli/tree/v%s\n' "$1" "$1" \
	> /opt/presso/licenses/wp-cli-source.txt
chmod 0755 /usr/local/bin/wp
wp --info --allow-root
