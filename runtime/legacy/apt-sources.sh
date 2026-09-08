#!/bin/sh
# Build-time only. Debian releases past LTS (bullseye since 2026-08-31) move
# to archive.debian.org, and the live mirrors drop them piecemeal: the index
# may still answer while the pool already 404s. So the rule is not "did
# apt-get update fail" but "does the archive carry this codename": if it
# does, the release is EOL and the archive is the only complete source. Its
# Release files are past their Valid-Until date, hence the override. The
# security suite lands on the archive later than main; it is added once it
# is there. Releases the archive does not know (bookworm, ...) are untouched.
set -eu

codename=$(. /etc/os-release && echo "$VERSION_CODENAME")
archive=http://archive.debian.org
has() { curl -fsSIL -o /dev/null --max-time 20 "$1"; }

if ! has "$archive/debian/dists/${codename}/InRelease"; then
	apt-get update
	exit 0
fi

echo "apt-sources: ${codename} is archived; using ${archive}" >&2
rm -f /etc/apt/sources.list.d/*.sources /etc/apt/sources.list.d/*.list
{
	echo "deb $archive/debian ${codename} main"
	echo "deb $archive/debian ${codename}-updates main"
	if has "$archive/debian-security/dists/${codename}-security/InRelease"; then
		echo "deb $archive/debian-security ${codename}-security main"
	else
		echo "apt-sources: ${codename}-security is not on the archive yet; skipped" >&2
	fi
} >/etc/apt/sources.list
echo 'Acquire::Check-Valid-Until "false";' >/etc/apt/apt.conf.d/99presso-archive
apt-get update
