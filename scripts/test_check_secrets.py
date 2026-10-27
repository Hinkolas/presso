"""Exercise credential detection in a disposable repository with synthetic tokens."""
from pathlib import Path
import secrets
import shutil
import subprocess
import tempfile
import unittest


@unittest.skipUnless(shutil.which('gitleaks'), 'requires Gitleaks on PATH')
class SecretCheckTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='presso-secret-check-test-')
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        source = Path(__file__).resolve().parent.parent
        (self.root / 'scripts').mkdir()
        shutil.copyfile(source / 'scripts/check_secrets.py', self.root / 'scripts/check_secrets.py')
        shutil.copyfile(source / '.gitleaks.toml', self.root / '.gitleaks.toml')
        (self.root / '.gitignore').write_text('.env\n')
        self.git('init', '-q')
        self.commit()
        # A syntactically valid but randomly generated, unissued test token.
        self.token = 'ghp_' + secrets.token_hex(18)

    def git(self, *args):
        return subprocess.check_output(['git', *args], cwd=self.root, stderr=subprocess.DEVNULL)

    def commit(self):
        self.git('add', '.')
        self.git('-c', 'user.name=Security test', '-c', 'user.email=test@example.invalid',
                 'commit', '-qm', 'fixture')

    def scan(self, expected):
        result = subprocess.run(
            ['python3', 'scripts/check_secrets.py'], cwd=self.root,
            capture_output=True, text=True,
        )
        self.assertEqual(result.returncode, expected, result.stdout + result.stderr)
        self.assertNotIn(self.token, result.stdout + result.stderr)

    def test_ignored_local_secret_is_not_publishable(self):
        (self.root / '.env').write_text('TOKEN=' + self.token + '\n')
        self.scan(0)

    def test_untracked_secret_and_neighbor_of_allowed_expression_are_detected(self):
        target = self.root / 'internal/site/container.go'
        target.parent.mkdir(parents=True)
        target.write_text('"PRESSO_DB_PREFIX=wp_"\napi_token = "' + self.token + '"\n')
        self.scan(1)

    def test_deleted_historical_secret_is_detected(self):
        target = self.root / 'credentials.txt'
        target.write_text('TOKEN=' + self.token + '\n')
        self.commit()
        target.unlink()
        self.commit()
        self.scan(1)

    def test_tracked_ignored_file_is_still_scanned(self):
        (self.root / '.env').write_text('TOKEN=' + self.token + '\n')
        self.git('add', '-f', '.env')
        self.scan(1)


if __name__ == '__main__':
    unittest.main()
