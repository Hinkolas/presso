# presso-valkey: the shared object cache. Valkey (BSD-3-Clause) from Alpine's
# package, published with every release so hosts pull it from presso's
# registry and its sources ship with the release. pressod passes the complete
# command line (password, memory limit, persistence off); the image only
# provides valkey-server and valkey-cli, the contract VALKEY_IMAGE documents.
#
# Alpine builds Valkey against musl's allocator. Unlike MariaDB, that measured
# within noise of upstream's jemalloc build (valkey-benchmark, 512-byte values),
# so no allocator is preloaded.
FROM alpine:3.24
RUN apk add --no-cache valkey valkey-cli
COPY LICENSE NOTICE /usr/share/licenses/presso/
# Persistence is off, so the working directory only has to exist.
WORKDIR /var/lib/valkey
USER valkey
EXPOSE 6379
CMD ["valkey-server", "--save", "", "--appendonly", "no"]
