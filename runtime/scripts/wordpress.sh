#!/bin/sh
# WordPress core at the web root, root-owned, read-only for both users.
# Usage: wordpress.sh <wp-version>
#
# The release tarball is fetched and unpacked with system tar rather than
# `wp core download`: WP-CLI extracts with PharData, which silently truncates
# paths longer than 100 characters (Twenty Twenty-Two's pattern files in
# WordPress 6.0-6.3 lose their .php suffix and the theme fatals on boot).
# The checksum wordpress.org publishes next to the tarball is verified.
#
# The stock wp-content is moved aside as the seed for an empty volume; the
# real path becomes the mount point. The seed is trimmed first: the bundled
# plugins (Akismet, Hello Dolly) go, and of the three bundled themes only the
# one core activates on install (WP_DEFAULT_THEME, which differs per release)
# stays, so a new site starts with nothing to clean up in wp-admin.
set -eu
version="$1"
url="https://wordpress.org/wordpress-${version}.tar.gz"
mkdir -p /var/www/html /opt/presso
curl -fsSL -o /tmp/wordpress.tar.gz "$url"
echo "$(curl -fsSL "${url}.sha1")  /tmp/wordpress.tar.gz" | sha1sum -c - >/dev/null
tar -xzf /tmp/wordpress.tar.gz --strip-components=1 -C /var/www/html
rm -f /tmp/wordpress.tar.gz
wp core version --allow-root --path=/var/www/html > /opt/presso/wp-version
[ "$(cat /opt/presso/wp-version)" = "$version" ] || { echo "core reports $(cat /opt/presso/wp-version), expected $version" >&2; exit 1; }
mv /var/www/html/wp-content /opt/presso/wp-content-seed
mkdir /var/www/html/wp-content
default_theme=$(sed -n "s/.*define( 'WP_DEFAULT_THEME', '\([a-z0-9-]*\)' ).*/\1/p" /var/www/html/wp-includes/default-constants.php)
[ -d "/opt/presso/wp-content-seed/themes/$default_theme" ] || { echo "default theme '$default_theme' not found in the seed" >&2; exit 1; }
for t in /opt/presso/wp-content-seed/themes/*/; do
	[ "$(basename "$t")" = "$default_theme" ] || rm -rf "$t"
done
rm -rf /opt/presso/wp-content-seed/plugins/akismet /opt/presso/wp-content-seed/plugins/hello.php
echo "seed keeps theme $default_theme; bundled plugins removed"
# Retain the upstream license in the distributed image, outside the web root.
mkdir -p /opt/presso/licenses
mv /var/www/html/license.txt /opt/presso/licenses/wordpress.txt
rm -f /var/www/html/wp-config-sample.php /var/www/html/readme.html
chown -R root:root /var/www/html /opt/presso
find /var/www/html /opt/presso -type d -exec chmod 0755 {} +
find /var/www/html /opt/presso -type f -exec chmod 0644 {} +
