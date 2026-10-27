import argparse
import io
import json
from pathlib import Path
import sys
import tarfile
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).parent))
import collect
import release


def fixture(directory):
    directory.mkdir(parents=True, exist_ok=True)
    files = {
        'THIRD_PARTY_LICENSES.txt': b'An upstream license and copyright notice\n',
        'docs/licensing.md': b'Matching retained source directions\n',
        'sources/presso/source.tar.gz': b'fixture source',
        'sources/dependency/source.tar.gz': b'preferred source, patches and recipes',
    }
    for n, b in files.items():
        p = directory / n
        p.parent.mkdir(parents=True, exist_ok=True)
        p.write_bytes(b)
    manifest = {'schema': 1, 'subject': {'kind': 'image', 'image_id': 'sha256:original', 'architecture': 'arm64'},
                'components': [{'kind': 'go-module', 'name': 'dependency', 'source': 'sources/dependency/source.tar.gz'}],
                'files': {n: {'sha256': collect.digest(directory / n), 'size': len(b)} for n, b in files.items()}}
    (directory / 'manifest.json').write_text(json.dumps(manifest))


class ManifestTests(unittest.TestCase):
    def test_missing_modified_and_unrecorded_material_blocks_verification(self):
        for mode in ('missing', 'modified', 'unrecorded'):
            with self.subTest(mode=mode), tempfile.TemporaryDirectory() as d:
                directory = Path(d)
                fixture(directory)
                collect.verify(directory)
                p = directory / 'sources/dependency/source.tar.gz'
                if mode == 'missing':
                    p.unlink()
                elif mode == 'modified':
                    p.write_bytes(b'different source')
                else:
                    (directory / 'unrecorded').write_text('not verified')
                with self.assertRaises(ValueError):
                    collect.verify(directory)

    def test_manifest_path_traversal_is_rejected(self):
        with self.assertRaises(ValueError):
            collect.checked_path('../secret')
        with self.assertRaises(ValueError):
            collect.checked_path('/etc/passwd')

    def test_notice_file_names(self):
        for name in ('m@v1/LICENSE', 'm@v1/UNLICENSE', 'COPYING.LIB', 'licenses/MIT.txt', 'NOTICE.md'):
            self.assertTrue(collect.NOTICE.search(name), name)
        for name in ('unlicensed.go', 'README.md', 'notices.go'):
            self.assertFalse(collect.NOTICE.search(name), name)

    def test_split_bundle_round_trip_and_checksums(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            fixture(root / 'input')
            name, files = release.pack(root / 'input', root / 'assets', 'test', part_size=97)
            record = json.loads(next(p for p in files if p.name.endswith('.manifest.json')).read_text())
            self.assertGreater(len(record['parts']), 1)
            joined = root / 'joined.tar.gz'
            with joined.open('wb') as out:
                for part in record['parts']:
                    path = root / 'assets' / part['name']
                    self.assertEqual(collect.digest(path), part['sha256'])
                    out.write(path.read_bytes())
            with tarfile.open(joined) as t:
                t.extractall(root / 'extract', filter='data')
            collect.verify(root / 'extract' / name)

    def test_debian_continuations_and_signed_descriptors(self):
        text = '-----BEGIN PGP SIGNED MESSAGE-----\nHash: SHA512\n\nSource: libtest\nVersion: 1:2.0-1\nChecksums-Sha256:\n abc 12 libtest.tar.xz\n\n-----BEGIN PGP SIGNATURE-----\n'
        row = next(r for r in collect.deb822(text) if 'Source' in r)
        self.assertEqual(row['Version'], '1:2.0-1')
        self.assertEqual(row['Checksums-Sha256'].strip(), 'abc 12 libtest.tar.xz')


class PublicationTests(unittest.TestCase):
    def run_image(self, root, source_failure=False, corrupt=False):
        calls = []
        def fake_run(*args, **kw):
            if args[:2] == ('python3', str(collect.ROOT / 'scripts/compliance/collect.py')):
                out = Path(args[args.index('--out') + 1])
                fixture(out)
                if corrupt:
                    (out / 'sources/dependency/source.tar.gz').unlink()
                calls.append('collect')
                return b''
            if args[:3] == ('docker', 'image', 'inspect'):
                layers = ['base'] if args[3] == 'sha256:original' else ['base', 'notices']
                return json.dumps([{'Id': 'sha256:documented', 'RootFS': {'Layers': layers}}]).encode()
            calls.append(' '.join(args[:2]))
            return b''
        def fake_publish(*args):
            calls.append('public-source-verification')
            if source_failure:
                raise ValueError('anonymous download failed')
        args = argparse.Namespace(image='input:1', target='output:1', release='v0.1.0', output=root / 'assets', push=True)
        with patch.object(release, 'run', side_effect=fake_run), patch.object(release, 'publish_sources', side_effect=fake_publish):
            if source_failure or corrupt:
                with self.assertRaises((ValueError, FileNotFoundError)):
                    release.image(args)
            else:
                release.image(args)
        return calls

    def test_sources_verified_before_image_push(self):
        with tempfile.TemporaryDirectory() as d:
            calls = self.run_image(Path(d))
            self.assertLess(calls.index('public-source-verification'), calls.index('docker push'))

    def test_failed_anonymous_download_blocks_push(self):
        with tempfile.TemporaryDirectory() as d:
            calls = self.run_image(Path(d), source_failure=True)
            self.assertNotIn('docker push', calls)

    def test_missing_source_blocks_both_publications(self):
        with tempfile.TemporaryDirectory() as d:
            calls = self.run_image(Path(d), corrupt=True)
            self.assertNotIn('public-source-verification', calls)
            self.assertNotIn('docker push', calls)

    def test_draft_sources_cannot_satisfy_publication(self):
        with patch.object(release, 'run', return_value=b'{"isDraft":true,"isPrerelease":true,"assets":[]}'):
            with self.assertRaisesRegex(ValueError, 'not publicly published'):
                release.publish_sources('v0.1.0', [])

    def test_anonymous_download_bytes_must_match(self):
        with tempfile.TemporaryDirectory() as d:
            asset = Path(d) / 'test-source.part-0000'
            asset.write_bytes(b'matching retained source')
            state = json.dumps({'isDraft': False, 'isPrerelease': True, 'assets': [{'name': asset.name}]}).encode()
            for payload in (asset.read_bytes(), b'wrong downloaded source'):
                with patch.object(release, 'run', return_value=state), patch.object(release.urllib.request, 'urlopen', return_value=io.BytesIO(payload)) as download:
                    if payload == asset.read_bytes():
                        release.publish_sources('v0.1.0', [asset])
                    else:
                        with self.assertRaisesRegex(ValueError, 'missing or differs'):
                            release.publish_sources('v0.1.0', [asset])
                    url = download.call_args.args[0]
                    self.assertIsInstance(url, str)  # no authenticated Request
                    self.assertIn('/releases/download/sources-v0.1.0/', url)

    def test_private_repository_fails_without_changing_visibility(self):
        with patch.object(release, 'run', return_value=b'{"visibility":"PRIVATE"}') as command:
            with self.assertRaisesRegex(ValueError, 'must be public'):
                release.prepare('v0.1.0')
            self.assertEqual(command.call_count, 1)
            self.assertEqual(command.call_args.args[:3], ('gh', 'repo', 'view'))


if __name__ == '__main__':
    unittest.main()
