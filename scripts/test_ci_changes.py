import subprocess
import tempfile
import unittest
from pathlib import Path

from ci_changes import documentation, needs_verification


class ChangeScopeTests(unittest.TestCase):
    def test_moved_review_records_are_documentation(self):
        for path in ('docs/release-verification.md', 'docs/license-review/review.md',
                     'docs/orchestration.md', 'docs/harness.md', 'docs/runtime.md'):
            self.assertTrue(documentation(path))

    def test_license_build_and_unknown_paths_keep_verification(self):
        for path in ('LICENSE', 'NOTICE', 'docs/licensing.md',
                     'docs/license-review/inventory.json',
                     'runtime/dropins/README.md', 'web/DESIGN.md',
                     '.github/workflows/ci.yml', 'scripts/ci_changes.py', 'new-file'):
            with self.subTest(path=path):
                self.assertFalse(documentation(path))

    def test_complete_git_diffs_and_fail_safe_events(self):
        with tempfile.TemporaryDirectory(prefix='presso-ci-scope-') as d:
            root = Path(d)
            def git(*args):
                return subprocess.check_output(['git', *args], cwd=d, stderr=subprocess.DEVNULL).decode().strip()
            def commit():
                git('add', '.')
                git('-c', 'user.name=CI test', '-c', 'user.email=ci@example.invalid', 'commit', '-qm', 'fixture')
                return git('rev-parse', 'HEAD')
            def check(base, head, expected):
                self.assertEqual(needs_verification('push', {'before': base, 'after': head}, d), expected)
                pr = {'pull_request': {'base': {'sha': base}, 'head': {'sha': head}}}
                self.assertEqual(needs_verification('pull_request', pr, d), expected)
            git('init', '-q')
            (root / 'README.md').write_text('initial')
            (root / 'code.go').write_text('code')
            initial = commit()
            (root / 'README.md').write_text('updated')
            docs = commit()
            check(initial, docs, False)
            (root / 'docs').mkdir()
            (root / 'code.go').rename(root / 'docs/runtime.md')
            renamed = commit()
            check(docs, renamed, True)  # Code deletion cannot hide as a guide.
            check(initial, renamed, True)  # Mixed code/documentation push.
            (root / 'README.md').unlink()
            deleted = commit()
            check(renamed, deleted, False)
            check('0' * 40, deleted, True)
            check('f' * 40, deleted, True)
            check(deleted, deleted, True)
            self.assertTrue(needs_verification('workflow_dispatch', {}, d))


if __name__ == '__main__':
    unittest.main()
