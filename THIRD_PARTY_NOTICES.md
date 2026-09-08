# Third-party notices

The root [Apache 2.0 license](LICENSE) applies to original presso code and
documentation unless a file states otherwise. It does not relicense upstream
software or dependencies bundled in the binaries or container images.

Released binary archives and presso-published images additionally include
`THIRD_PARTY_LICENSES.txt`, collected from the actual compiled dependencies and
image contents. In images it is under `/usr/share/licenses/presso/`, beside
`SOURCE_BUNDLE`, the inventory, and [source-access instructions](SOURCE_ACCESS.md).
The version's separate `sources-<version>` prerelease retains matching source
archives, patches, build recipes, and the original notice files. Publication
requires successful collection and verified anonymous source downloads.

## MySQL driver source notice

The `pressod` binary includes `github.com/go-sql-driver/mysql` v1.10.1 under the
Mozilla Public License 2.0. Its unmodified source, including the license, is
available at <https://github.com/go-sql-driver/mysql/tree/v1.10.1>. You can
download it at <https://github.com/go-sql-driver/mysql/archive/refs/tags/v1.10.1.tar.gz>.
The MPL-covered files remain available under MPL-2.0; the Apache license for
presso does not restrict your rights to those files. Keep this notice aligned
with the driver version in `go.mod` when updating dependencies.

## Vendored Redis Object Cache drop-in

`runtime/dropins/object-cache.php` is Redis Object Cache 2.8.0 by Till Krüss,
licensed under GPLv3. Its upstream source and license headers are preserved.
See [the vendoring record](runtime/dropins/README.md), the
[GPLv3 license text](runtime/dropins/COPYING), and
[upstream source](https://github.com/rhubarbgroup/redis-cache/tree/2.8.0).

## Runtime images and dependencies

The web build generates `third-party-licenses.txt` from the installed browser
runtime and font packages. It is served alongside the panel, embedded with the
UI, and included at `web/static/third-party-licenses.txt` in binary archives.
See `web/scripts/licenses.mjs` for the explicit package list. This covers those
packages, not all transitive code in the engine and container images.

Runtime images include WordPress and upstream components such as PHP,
FrankenPHP/Caddy or nginx, PHP extensions, and WP-CLI. The downloaded WordPress
license is retained outside the web root at `/opt/presso/licenses/wordpress.txt`.
The Redis drop-in's license accompanies it under `/opt/presso/dropins/COPYING`.
The WP-CLI root license and source reference are retained under
`/opt/presso/licenses/wp-cli.txt` and `wp-cli-source.txt`. This does not replace
the separate obligations for packages bundled inside the PHAR; the release
collector inspects its installed Composer references and includes their sources
and notices. Oxymel's pinned revision declares MIT in `composer.json` without
a standalone license file; its declaration, author attribution, and an
explicitly identified MIT supplement are retained together.
The Apache license and presso notice accompany the system plugin under
`/opt/presso/mu-plugins/` and the engine/edge images under
`/usr/share/licenses/presso/`. Copies under `build/` and `runtime/mu-plugins/`
support their separate Docker build contexts; release checks keep them in sync
with the root `LICENSE` and `NOTICE`.

The runtime-base images also include the original presso license and notice
under `/usr/share/licenses/presso/`, since they are published independently.

## Valkey server and Redis protocol clients

The default cache server is `valkey/valkey:8.1.10-alpine`, pulled directly from
upstream by the engine. Valkey's server license is BSD-3-Clause; its
[versioned source and notices](https://github.com/valkey-io/valkey/tree/8.1.10)
and [full COPYING file](https://raw.githubusercontent.com/valkey-io/valkey/8.1.10/COPYING)
include both Valkey contributors' and inherited Redis copyright notices.
The container's other packages retain their own terms. Presso does not
republish this image as part of its own runtime or edge images.

The server change removes Redis 7.4's RSALv2/SSPLv1 from the default deployment.
It does not change the GPLv3 license of Redis Object Cache, the BSD license of
`github.com/redis/go-redis/v9`, or the PHP license of phpredis. Those clients
speak the protocol Valkey supports and do not require the Redis server.
`WP_REDIS_*` / `PRESSO_REDIS_*` remain protocol configuration names.
See [the Valkey verification record](docs/license-review/valkey.md).

Go and frontend dependencies, fonts, operating-system packages, and shared
service images retain their respective licenses and notices. Dependency versions
are recorded in `go.mod`/`go.sum`, `web/package-lock.json`, Dockerfiles, and the
runtime matrix. This file identifies the directly vendored code; it is not a
complete inventory of every transitive dependency or base-image package.
The original findings and subsequent implementation are recorded in
[LICENSE_REVIEW.md](LICENSE_REVIEW.md). These notices are not a statement that
all release artifacts have completed license compliance review.
