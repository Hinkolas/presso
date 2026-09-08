# dropins/

WordPress drop-ins must live in `WP_CONTENT_DIR`; unlike must-use plugins
they cannot be relocated with a constant. The entrypoint therefore copies
the files here into the wp-content volume on every container start
(refresh, so a new image tag always wins), or removes them when the feature
is off.

## object-cache.php

- Source: Redis Object Cache by Till Krüss, `includes/object-cache.php`
  https://github.com/rhubarbgroup/redis-cache/blob/2.8.0/includes/object-cache.php
- Version: 2.8.0 (vendored verbatim; sha256
  `713e9a12018e865f7daf71d84e6257b7756251560f04aac6cc6b5b15bf9d0b33`,
  identical to `includes/object-cache.php` in
  https://downloads.wordpress.org/plugin/redis-cache.2.8.0.zip)
- License: GPLv3; the full text is in [COPYING](COPYING). This vendored file
  retains its upstream license and is not relicensed under presso's Apache 2.0 license.
- Configured entirely through the `WP_REDIS_*` constants in `wp-config.php`,
  which the runtime derives from `PRESSO_REDIS_*`. With no
  `PRESSO_REDIS_HOST` the entrypoint removes the file and `WP_REDIS_DISABLED`
  keeps WordPress on its in-memory cache.

To upgrade, replace the file with the new release and update the version
and hash above.
