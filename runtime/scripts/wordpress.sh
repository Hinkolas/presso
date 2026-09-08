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
# The stock wp-content (default themes, akismet, hello.php, index.php) is
# moved aside as the seed for an empty volume; the real path becomes the
# mount point.
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
rm -f /var/www/html/wp-config-sample.php /var/www/html/readme.html /var/www/html/license.txt
chown -R root:root /var/www/html /opt/presso
find /var/www/html /opt/presso -type d -exec chmod 0755 {} +
find /var/www/html /opt/presso -type f -exec chmod 0644 {} +
