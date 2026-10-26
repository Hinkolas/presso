# presso-mariadb: the shared database server. MariaDB (GPL-2.0) from Alpine's
# package, published with every release so its sources ship with it.
#
# musl's allocator costs MariaDB about a third of its read/write throughput
# under concurrency; with jemalloc preloaded it matches a glibc build (sysbench
# OLTP, 16 threads), so the entrypoint starts the server with jemalloc.
#
# The entrypoint implements the parts of the official image's contract that
# pressod uses (MARIADB_ROOT_PASSWORD, MARIADB_AUTO_UPGRADE, mariadbd options
# as arguments, healthcheck.sh --connect --innodb_initialized), so
# MARIADB_IMAGE stays interchangeable with mariadb:11.8. The entrypoint starts
# as root only to give mysql (uid 999, as in the official image) its data
# directory, then switches to it with su-exec; the server never runs as root.
FROM alpine:3.24
# Alpine's ping group holds gid 999; the package reuses an existing mysql user.
RUN delgroup ping \
 && addgroup -S -g 999 mysql \
 && adduser -S -D -H -u 999 -G mysql -h /var/lib/mysql -s /sbin/nologin mysql \
 && apk add --no-cache mariadb mariadb-client mariadb-server-utils jemalloc su-exec \
 && mkdir -p /run/mysqld /var/lib/mysql \
 && chown mysql:mysql /run/mysqld /var/lib/mysql \
 && chmod 0750 /var/lib/mysql
COPY LICENSE NOTICE /usr/share/licenses/presso/
COPY mariadb/mariadb.cnf /etc/presso/mariadb.cnf
RUN printf '!include /etc/presso/mariadb.cnf\n' > /etc/my.cnf.d/zz-presso.cnf
COPY --chmod=0755 mariadb/entrypoint.sh /usr/local/bin/presso-mariadb-entrypoint
COPY --chmod=0755 mariadb/health.sh /usr/local/bin/presso-mariadb-health
RUN ln -s presso-mariadb-health /usr/local/bin/healthcheck.sh
VOLUME ["/var/lib/mysql"]
EXPOSE 3306
ENTRYPOINT ["presso-mariadb-entrypoint"]
CMD ["mariadbd"]
