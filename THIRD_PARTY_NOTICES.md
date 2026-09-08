# Third-party notices

The root [Apache 2.0 license](LICENSE) applies to original presso code and
documentation unless a file states otherwise. It does not relicense upstream
software or dependencies bundled in the binaries or container images.

## Vendored Redis Object Cache drop-in

`runtime/dropins/object-cache.php` is Redis Object Cache 2.8.0 by Till Krüss,
licensed under GPLv3. Its upstream source and license headers are preserved.
See [the vendoring record](runtime/dropins/README.md), the
[GPLv3 license text](runtime/dropins/COPYING), and
[upstream source](https://github.com/rhubarbgroup/redis-cache/tree/2.8.0).

## Runtime images and dependencies

Runtime images include WordPress and upstream components such as PHP,
FrankenPHP/Caddy or nginx, PHP extensions, and WP-CLI. The downloaded WordPress
license is retained outside the web root at `/opt/presso/licenses/wordpress.txt`.
The Redis drop-in's license accompanies it under `/opt/presso/dropins/COPYING`.
The Apache license and presso notice accompany the system plugin under
`/opt/presso/mu-plugins/` and the engine/edge images under
`/usr/share/licenses/presso/`. Copies under `build/` and `runtime/mu-plugins/`
support their separate Docker build contexts; release checks keep them in sync
with the root `LICENSE` and `NOTICE`.

Go and frontend dependencies, fonts, operating-system packages, and shared
service images retain their respective licenses and notices. Dependency versions
are recorded in `go.mod`/`go.sum`, `web/package-lock.json`, Dockerfiles, and the
runtime matrix. This file identifies the directly vendored code; it is not a
complete inventory of every transitive dependency or base-image package.
