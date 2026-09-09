"""Ownership regression tests on a native Linux filesystem, even on macOS.

Run with PRESSO_TEST_DOCKER=1. The container has no host mounts or Docker socket;
its /tmp reproduces Linux ownership rather than Docker Desktop file sharing.
"""
import os
import subprocess
import unittest

from collect import ALPINE_BOOTSTRAP, ALPINE_FETCH


HARNESS = r'''set -eu
apk add --no-cache python3 >/dev/null
mkdir -p /tmp/runner/work/packages/fixture
ln -s /tmp/runner/work /work
cd /work/packages/fixture
printf 'fixture source\n' > payload.txt
sum=$(sha512sum payload.txt | cut -d' ' -f1)
if [ "$1" = checksum-failure ]; then sum=$(printf '%0128d' 0); fi
cat > APKBUILD <<EOF
pkgname=fixture
pkgver=1.0
pkgrel=0
source="payload.txt"
sha512sums="$sum  payload.txt"
EOF
cat >> APKBUILD <<'EOF'
fetch() {
  mkdir -p "$SRCDEST/nested"
  printf 'downloaded bytes\n' > "$SRCDEST/nested/download"
  default_fetch
}
EOF
printf '1.0-r0\n' > expected-version
chown -R 1001:1001 /tmp/runner
set +e
sh -ec "$2" presso-source-helper 1001 1001 "$3"
result=$?
set -e
if [ "$1" = checksum-failure ]; then
  test "$result" -ne 0
else
  test "$result" -eq 0
fi
cd /tmp
su-exec 1001:1001 python3 -c '
from pathlib import Path
import shutil
for p in Path("/tmp/runner/work").rglob("*"):
    assert p.lstat().st_uid == 1001, f"wrong owner: {p}"
    assert p.lstat().st_gid == 1001, f"wrong group: {p}"
shutil.rmtree("/tmp/runner")
assert not Path("/tmp/runner").exists()
print("Runner ownership and cleanup verified")
'
'''


@unittest.skipUnless(os.environ.get('PRESSO_TEST_DOCKER') == '1', 'requires explicit Docker test run')
class AlpineOwnershipTests(unittest.TestCase):
    def test_source_files_remain_removable_after_success_and_checksum_failure(self):
        for outcome in ('success', 'checksum-failure'):
            with self.subTest(outcome=outcome):
                result = subprocess.run(
                    ['docker', 'run', '--rm', '--entrypoint', 'sh', 'alpine:3.23',
                     '-ec', HARNESS, 'presso-ownership-test', outcome, ALPINE_BOOTSTRAP, ALPINE_FETCH],
                    capture_output=True, text=True, timeout=180,
                )
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertIn('Runner ownership and cleanup verified', result.stdout)
                if outcome == 'checksum-failure':
                    self.assertIn('FAILED', result.stdout + result.stderr)


if __name__ == '__main__':
    unittest.main()
