import subprocess
import tempfile
import unittest
from pathlib import Path

from ci_changes import affects_sources, documentation, needs_sources, needs_verification


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

    def test_image_and_collection_inputs_need_sources(self):
        for path in ('build/mariadb.Dockerfile', 'build/browser/presso-browser', 'runtime/base.Dockerfile',
                     'runtime/php/presso.ini', 'scripts/compliance/collect.py', 'internal/compat/matrix.json',
                     'docker-bake.hcl', '.github/workflows/verify.yml', 'scripts/ci_changes.py',
                     'LICENSE', 'NOTICE', 'docs/licensing.md'):
            with self.subTest(path=path):
                self.assertTrue(affects_sources(path))
        for path in ('internal/platform/platform.go', 'internal/compat/compat.go', 'web/src/app.html',
                     'go.mod', 'README.md', 'docs/runtime.md', 'scripts/check-release-gates.rb'):
            with self.subTest(path=path):
                self.assertFalse(affects_sources(path))

    def test_complete_git_diffs_and_fail_safe_events(self):
        with tempfile.TemporaryDirectory(prefix='presso-ci-scope-') as d:
            root = Path(d)
            def git(*args):
                return subprocess.check_output(['git', *args], cwd=d, stderr=subprocess.DEVNULL).decode().strip()
            def commit():
                git('add', '.')
                git('-c', 'user.name=CI test', '-c', 'user.email=ci@example.invalid', 'commit', '-qm', 'fixture')
                return git('rev-parse', 'HEAD')
            def check(base, head, expected, sources=None):
                push = {'before': base, 'after': head}
                pr = {'pull_request': {'base': {'sha': base}, 'head': {'sha': head}}}
                for name, event in (('push', push), ('pull_request', pr)):
                    self.assertEqual(needs_verification(name, event, d), expected)
                    if sources is not None:
                        self.assertEqual(needs_sources(name, event, d), sources)
            git('init', '-q')
            (root / 'README.md').write_text('initial')
            (root / 'code.go').write_text('code')
            initial = commit()
            (root / 'README.md').write_text('updated')
            docs = commit()
            check(initial, docs, False)
            (root / 'code.go').write_text('changed code')
            code = commit()
            check(docs, code, True, sources=False)
            (root / 'build').mkdir()
            (root / 'build/valkey.Dockerfile').write_text('FROM alpine')
            image = commit()
            check(code, image, True, sources=True)
            check(docs, image, True, sources=True)
            (root / 'docs').mkdir()
            (root / 'code.go').rename(root / 'docs/runtime.md')
            renamed = commit()
            check(docs, renamed, True)  # Code deletion cannot hide as a guide.
            check(initial, renamed, True)  # Mixed code/documentation push.
            (root / 'README.md').unlink()
            deleted = commit()
            check(renamed, deleted, False)
            check('0' * 40, deleted, True, sources=True)
            check('f' * 40, deleted, True, sources=True)
            check(deleted, deleted, True, sources=True)
            for name in ('workflow_dispatch', 'schedule'):
                self.assertTrue(needs_verification(name, {}, d))
                self.assertTrue(needs_sources(name, {}, d))


if __name__ == '__main__':
    unittest.main()
