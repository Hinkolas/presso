#!/bin/sh
# The two-user model (RUNTIME.md section 8), identical in every base image.
# web (1000) runs the web server and may only write uploads/, cache/ and tmp.
# ops (1001) owns plugins/, themes/, languages/ and runs WP-CLI. ops is a
# member of web so it can read (and, via group-writable dirs, write) what web
# owns; web is deliberately NOT a member of ops.
set -eu
groupadd -g 1000 web
useradd -u 1000 -g web -M -s /usr/sbin/nologin web
groupadd -g 1001 ops
useradd -u 1001 -g ops -G web -M -s /bin/bash ops
mkdir -p /etc/presso/entrypoint.d
