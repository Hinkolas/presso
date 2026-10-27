#!/usr/bin/env python3
"""Collect the actual shipped dependencies; missing inputs fail the build.

No installer, running container, host volume, or operator state is inspected.
Downloads are retained, hashed, and accompanied by their upstream identity.
"""
import argparse
import concurrent.futures
import hashlib
import io
import json
import os
from pathlib import Path, PurePosixPath
import re
import shutil
import subprocess
import tarfile
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request
import zipfile

ROOT = Path(__file__).resolve().parents[2]
NOTICE = re.compile(r"(^|/)((un)?licen[cs]es?|copying|copyright|notice|authors|patents)([._/-]|$)", re.I)

# Install tools in the disposable container as root, then drop to the caller's
# numeric UID/GID before touching its bind mount. Native Linux preserves these
# owners; Docker Desktop's sharing layer can hide root-owned-file mistakes.
ALPINE_BOOTSTRAP = '''set -eu
apk add --no-cache abuild curl su-exec >/dev/null
export HOME=/tmp
exec su-exec "$1:$2" sh -ec "$3"
'''

ALPINE_FETCH = '''set -eu
mkdir -p /work/distfiles
export SRCDEST=/work/distfiles
export DISTFILES_MIRROR="https://distfiles.alpinelinux.org/distfiles/v$(cut -d. -f1,2 /etc/alpine-release)"
for recipe in /work/packages/*; do
  cd "$recipe"
  actual=$(sh -c '. ./APKBUILD; printf "%s-r%s" "$pkgver" "$pkgrel"')
  grep -Fxq "$actual" expected-version
  abuild -F fetch verify
  # abuild creates absolute container-local links here. The original recipes
  # and actual distfiles are archived separately; do not archive these links.
  rm -rf src
done
'''


def run(*args, **kwargs):
    p = subprocess.run(args, stdout=subprocess.PIPE, stderr=subprocess.PIPE, **kwargs)
    if p.returncode:
        raise RuntimeError(f"{args[0]} failed: {p.stderr.decode(errors='replace')[-3000:]}")
    return p.stdout


def digest(path, algorithm='sha256'):
    h = hashlib.new(algorithm)
    with open(path, 'rb') as f:
        for chunk in iter(lambda: f.read(1024 * 1024), b''):
            h.update(chunk)
    return h.hexdigest()


def safe_name(value):
    return re.sub(r'[^A-Za-z0-9._+-]', '_', value)


def checked_path(value):
    p = PurePosixPath(value)
    if p.is_absolute() or '..' in p.parts:
        raise ValueError(f"unsafe archive path: {value}")
    return str(p)


def fetch(url, cache):
    """Cache only complete successful downloads; no credentials in records."""
    if not url.startswith('https://'):
        raise ValueError(f"source URL must use HTTPS: {url}")
    dest = cache / hashlib.sha256(url.encode()).hexdigest()
    if dest.exists():
        return dest
    cache.mkdir(parents=True, exist_ok=True)
    for attempt in range(4):
        try:
            req = urllib.request.Request(url, headers={'User-Agent': 'presso-source-collector/1'})
            with urllib.request.urlopen(req, timeout=90) as response, tempfile.NamedTemporaryFile(dir=cache, delete=False) as out:
                tmp = Path(out.name)
                shutil.copyfileobj(response, out)
            if not tmp.stat().st_size:
                raise ValueError(f"empty download: {url}")
            tmp.replace(dest)
            return dest
        except (urllib.error.URLError, TimeoutError) as e:
            if isinstance(e, urllib.error.HTTPError) and e.code in (400, 401, 403, 404):
                raise
            if attempt == 3:
                raise
            time.sleep(2 ** attempt)
    raise AssertionError('unreachable')


def archive_notices(path):
    """Retain all named notices, including nested bundled third-party code."""
    result = {}
    if zipfile.is_zipfile(path):
        with zipfile.ZipFile(path) as z:
            for m in z.infolist():
                if not m.is_dir() and NOTICE.search(m.filename):
                    result[checked_path(m.filename)] = z.read(m)
    else:
        try:
            with tarfile.open(path, 'r:*') as t:
                for m in t:
                    if m.isfile() and NOTICE.search(m.name):
                        result[checked_path(m.name)] = t.extractfile(m).read()
        except tarfile.ReadError:
            pass  # .dsc, .diff.gz and build recipes are also retained as inputs.
    return result


class Bundle:
    def __init__(self, out, cache):
        self.out, self.cache = out, cache
        out.mkdir(parents=True, exist_ok=True)
        self.components = []
        self.records = []
        self.notices = []

    def add_file(self, src, name, url=None):
        name = checked_path(name)
        dest = self.out / name
        dest.parent.mkdir(parents=True, exist_ok=True)
        if dest.exists() and digest(dest) != digest(src):
            raise ValueError(f"conflicting material: {name}")
        if src.resolve() != dest.resolve():
            shutil.copyfile(src, dest)
        self.records.append({'path': name, 'sha256': digest(dest), 'size': dest.stat().st_size, 'url': url})
        return name

    def source(self, component, url, filename):
        src = fetch(url, self.cache)
        name = self.add_file(src, 'sources/' + safe_name(component) + '/' + filename, url)
        notices = archive_notices(src)
        for n, data in notices.items():
            self.notice(component, n, data)
        return name, notices

    def notice(self, component, name, data):
        if not data.strip():
            return
        # Binary PDFs occasionally use a NOTICE filename; preserve them too.
        dest = self.out / 'notices' / safe_name(component) / checked_path(name)
        dest.parent.mkdir(parents=True, exist_ok=True)
        dest.write_bytes(data)
        self.notices.append((component, name, data))

    def repository(self):
        # Include current tracked files plus non-ignored additions for local
        # rehearsals. Ignored .env, state, binaries and node_modules stay out.
        names = run('git', 'ls-files', '-z', '--cached', '--others', '--exclude-standard', cwd=ROOT).decode().split('\0')
        with tempfile.NamedTemporaryFile(suffix='.tar.gz') as f:
            with tarfile.open(f.name, 'w:gz') as t:
                for n in sorted(set(filter(None, names))):
                    checked_path(n)
                    path = ROOT / n
                    if path.is_file() and not path.is_symlink():
                        t.add(path, arcname='presso/' + n, recursive=False)
            self.add_file(Path(f.name), 'sources/presso/source.tar.gz')

    def go_binary(self, binary, label, generated_main=False):
        info = json.loads(run('go', 'version', '-m', '-json', str(binary)))
        self.components.append({'kind': 'go-binary', 'name': label, 'sha256': digest(binary), 'buildinfo': info})
        version = info['GoVersion']
        if not re.fullmatch(r'go\d+\.\d+(?:\.\d+)?', version):
            raise ValueError(f"unreleased Go toolchain: {version}")
        self.source(version, f'https://go.dev/dl/{version}.src.tar.gz', 'source.tar.gz')
        for mod in [info['Main']] + info.get('Deps', []):
            path = mod['Path']
            if path == 'github.com/Hinkolas/presso' or path == 'caddy' and generated_main:
                continue  # Exact working-tree source is included separately.
            actual = mod.get('Replace', mod)
            if actual.get('Version', '(devel)') == '(devel)':
                settings = {x['Key']: x.get('Value', '') for x in info.get('Settings', [])}
                # FrankenPHP records ../ as a local replacement. Its immutable
                # VCS revision identifies both the caddy submodule and parent.
                if path != 'github.com/dunglas/frankenphp' or settings.get('vcs.modified') != 'false':
                    raise ValueError(f"unresolved local Go replacement: {mod}")
                rev = settings.get('vcs.revision', '')
                if not re.fullmatch('[a-f0-9]{40}', rev):
                    raise ValueError('FrankenPHP revision missing')
                name, notices = self.source(path + '@' + rev, f'https://codeload.github.com/dunglas/frankenphp/tar.gz/{rev}', 'source.tar.gz')
            else:
                # go mod download verifies the public checksum database and the
                # binary's recorded h1 checksum, rather than trusting a new tag.
                with tempfile.TemporaryDirectory(prefix='presso-go-download-') as wd:
                    env = dict(os.environ, GOWORK='off', GO111MODULE='on')
                    data = json.loads(run('go', 'mod', 'download', '-json', actual['Path'] + '@' + actual['Version'], cwd=wd, env=env))
                if data.get('Error') or actual.get('Sum') and data['Sum'] != actual['Sum']:
                    raise ValueError(f"Go source checksum mismatch: {path}")
                name = self.add_file(Path(data['Zip']), 'sources/go/' + safe_name(actual['Path'] + '@' + actual['Version']) + '.zip', 'https://proxy.golang.org/' + actual['Path'])
                self.add_file(Path(data['GoMod']), name + '.mod')
                notices = archive_notices(Path(data['Zip']))
                for n, text in notices.items():
                    self.notice(path + '@' + actual['Version'], n, text)
            if not notices:
                raise ValueError(f"Go dependency has no license material: {path}")
            self.components.append({'kind': 'go-module', 'name': path, 'version': mod.get('Version'), 'source': name})

    def frontend(self):
        manifest = json.loads((ROOT / 'web/static/third-party-manifest.json').read_text())
        lock = json.loads((ROOT / 'web/package-lock.json').read_text())['packages']
        for item in manifest:
            name = item['name']
            p = lock['node_modules/' + name]
            if p['version'] != item['version']:
                raise ValueError(f"stale frontend notice manifest: {name}")
            src = fetch(p['resolved'], self.cache)
            algorithm, expected = p['integrity'].split('-', 1)
            import base64
            if base64.b64encode(bytes.fromhex(digest(src, algorithm))).decode() != expected:
                raise ValueError(f"npm integrity mismatch: {name}")
            source = self.add_file(src, 'sources/npm/' + safe_name(name + '@' + p['version']) + '.tgz', p['resolved'])
            notices = archive_notices(src)
            if not notices:
                raise ValueError(f"npm package has no notices: {name}")
            for n, data in notices.items():
                self.notice(name, n, data)
            self.components.append({'kind': 'npm', 'name': name, 'version': p['version'], 'source': source})

    def finish(self, subject):
        self.repository()
        for n in ('LICENSE', 'NOTICE'):
            self.notice('presso', n, (ROOT / n).read_bytes())
            self.add_file(ROOT / n, n)
        # A single readable file travels inside the binary archive/image; the
        # original unmodified files and source archives accompany the bundle.
        text = ['Third-party licenses and copyright notices\n', 'Source access: see docs/licensing.md shipped with this distribution.\n']
        unique = {}
        for component, name, data in self.notices:
            unique[(component, name)] = data
        for (component, name), data in sorted(unique.items()):
            text.append(f'\n{"=" * 72}\n{component}: {name}\n{"=" * 72}\n')
            text.append(data.decode('utf-8', errors='replace') + '\n')
        (self.out / 'THIRD_PARTY_LICENSES.txt').write_text(''.join(text))
        self.add_file(ROOT / 'docs/licensing.md', 'docs/licensing.md')
        manifest = {'schema': 1, 'subject': subject, 'components': self.components,
                    'files': {str(p.relative_to(self.out)): {'sha256': digest(p), 'size': p.stat().st_size}
                              for p in self.out.rglob('*') if p.is_file()}, 'downloads': self.records}
        (self.out / 'manifest.json').write_text(json.dumps(manifest, indent=2) + '\n')
        verify(self.out)


def verify(directory):
    m = json.loads((directory / 'manifest.json').read_text())
    if m.get('schema') != 1 or not m['components'] or not m['files']:
        raise ValueError('empty compliance manifest')
    for name, record in m['files'].items():
        p = directory / checked_path(name)
        if not p.is_file() or p.stat().st_size != record['size'] or digest(p) != record['sha256']:
            raise ValueError(f"missing or changed compliance material: {name}")
    actual = {str(p.relative_to(directory)) for p in directory.rglob('*') if p.is_file()} - {'manifest.json'}
    if actual != set(m['files']):
        raise ValueError('unrecorded compliance material')
    for c in m['components']:
        if c.get('source') and c['source'] not in m['files']:
            raise ValueError(f"missing component source: {c['name']}")
    for required in ('THIRD_PARTY_LICENSES.txt', 'docs/licensing.md', 'sources/presso/source.tar.gz'):
        if required not in m['files']:
            raise ValueError(f"missing required material: {required}")
    return m


class Image:
    def __init__(self, ref, temp):
        self.ref, self.temp = ref, temp
        self.inspect = json.loads(run('docker', 'image', 'inspect', ref))[0]
        self.id = self.inspect['Id']
        cid = run('docker', 'create', '--entrypoint', '/bin/true', self.id).decode().strip()
        try:
            archive = temp / 'rootfs.tar'
            run('docker', 'export', '-o', str(archive), cid)
        finally:
            run('docker', 'rm', '-v', cid)
        self.tar = tarfile.open(archive)
        self.members = {m.name.lstrip('./'): m for m in self.tar.getmembers()}

    def read(self, name):
        name = name.lstrip('/')
        for _ in range(30):
            m = self.members.get(name)
            if m is None:
                # Docker's merged-/usr root has symlinked parent directories.
                parts = PurePosixPath(name).parts
                for i in range(1, len(parts)):
                    prefix = '/'.join(parts[:i])
                    parent = self.members.get(prefix)
                    if parent and parent.issym():
                        name = os.path.normpath('/' + str(PurePosixPath(prefix).parent / parent.linkname) + '/' + '/'.join(parts[i:])).lstrip('/')
                        break
                else:
                    raise FileNotFoundError(name)
                continue
            if m.isfile():
                return self.tar.extractfile(m).read()
            if m.islnk():
                name = m.linkname.lstrip('/')
            elif m.issym():
                name = os.path.normpath('/' + (m.linkname.lstrip('/') if m.linkname.startswith('/') else str(PurePosixPath(name).parent / m.linkname))).lstrip('/')
            else:
                raise ValueError(f"not a regular file: {name}")
        raise ValueError(f"symlink loop: {name}")

    def execute(self, *cmd):
        return run('docker', 'run', '--rm', '--network', 'none', '--read-only', '--tmpfs', '/tmp', '--entrypoint', cmd[0], self.id, *cmd[1:])


def deb822(text):
    records = []
    for block in text.strip().split('\n\n'):
        row, key = {}, None
        for line in block.splitlines():
            if line.startswith((' ', '\t')) and key:
                row[key] += '\n' + line.strip()
            elif ':' in line:
                key, value = line.split(':', 1)
                row[key] = value.lstrip()
        if row:
            records.append(row)
    return records


def debian(bundle, image):
    status = deb822(image.read('var/lib/dpkg/status').decode())
    installed = [p for p in status if p.get('Status') == 'install ok installed']
    if not installed:
        raise ValueError('Debian image has no installed package records')
    sources = {}
    for p in installed:
        n, v = p['Package'], p['Version']
        match = re.fullmatch(r'(\S+)(?: \(([^)]+)\))?', p.get('Source', n))
        src, version = match[1], match[2] or v
        sources[(src, version)] = True
        copyright = image.read(f'usr/share/doc/{n}/copyright')
        if not copyright.strip():
            raise ValueError(f"empty Debian copyright: {n}")
        bundle.notice(n + '@' + v, 'copyright', copyright)
        bundle.components.append({'kind': 'deb', 'name': n, 'version': v, 'source_package': src, 'source_version': version})
    for name, member in image.members.items():
        if name.startswith('usr/share/common-licenses/') and member.isfile():
            bundle.notice('debian-common-licenses', PurePosixPath(name).name, image.read(name))
    # The snapshot API indexes exact source versions, including versions that
    # have disappeared from live apt repositories. Retain all source packages,
    # not a guessed subset based on a package's short license description.
    def obtain(pair):
        pkg, version = pair
        url = f'https://snapshot.debian.org/mr/package/{urllib.parse.quote(pkg, safe="")}/{urllib.parse.quote(version, safe="")}/srcfiles'
        records = json.loads(fetch(url, bundle.cache).read_text())['result']
        if not records:
            raise ValueError(f"no corresponding Debian source: {pkg} {version}")
        downloaded = {}
        for r in records:
            sha = r['hash']
            details = json.loads(fetch(f'https://snapshot.debian.org/mr/file/{sha}/info', bundle.cache).read_text())['result']
            names = {x['name'] for x in details}
            src_url = f'https://snapshot.debian.org/file/{sha}'
            data = fetch(src_url, bundle.cache)
            if digest(data, 'sha1') != sha:
                raise ValueError(f"Debian source checksum mismatch: {pkg}")
            downloaded[sha] = (names, data, src_url)
        # The same upstream bytes may have several Debian filenames. The
        # exact .dsc, not a guessed filename from the snapshot index, decides.
        dsc_name = pkg + '_' + version.split(':')[-1] + '.dsc'
        descriptors = [v for v in downloaded.values() if dsc_name in v[0]]
        if len(descriptors) != 1:
            raise ValueError(f"missing/ambiguous Debian descriptor: {pkg} {version}")
        _, dsc_data, dsc_url = descriptors[0]
        fields = next(r for r in deb822(dsc_data.read_text()) if 'Source' in r)
        if fields['Source'] != pkg or fields['Version'] != version:
            raise ValueError('Debian source identity mismatch')
        result = [(pkg, version, dsc_name, dsc_data, dsc_url)]
        for line in filter(str.strip, fields['Checksums-Sha1'].splitlines()):
            sha, size, name = line.split()
            names, data, url = downloaded[sha]
            if name not in names or data.stat().st_size != int(size):
                raise ValueError('Debian source descriptor/file mismatch')
            result.append((pkg, version, checked_path(name), data, url))
        return result
    with concurrent.futures.ThreadPoolExecutor(max_workers=6) as pool:
        for files in pool.map(obtain, sorted(sources)):
            pkg, version = files[0][:2]
            dsc = []
            for _, _, name, data, url in files:
                dest = bundle.add_file(data, f'sources/debian/{safe_name(pkg + "@" + version)}/{name}', url)
                if name.endswith('.dsc'):
                    dsc.append(dest)
            if len(dsc) != 1:
                raise ValueError(f"missing Debian .dsc/build recipe: {pkg} {version}")
            # The .dsc describes the preferred source, patches and their hashes.
            fields = next(r for r in deb822((bundle.out / dsc[0]).read_text()) if 'Source' in r)
            checks = fields.get('Checksums-Sha256', '').splitlines()
            if not checks:
                raise ValueError(f"Debian source has no SHA256 list: {pkg}")
            for line in filter(str.strip, checks):
                sha, size, name = line.split()
                p = (bundle.out / dsc[0]).parent / checked_path(name)
                if digest(p) != sha or p.stat().st_size != int(size):
                    raise ValueError(f"Debian .dsc verification failed: {name}")
            bundle.components.append({'kind': 'deb-source', 'name': pkg, 'version': version, 'source': dsc[0]})
            print(f'collected Debian source {pkg} {version}', flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest='command', required=True)
    for command in ('binary', 'image'):
        p = sub.add_parser(command)
        p.add_argument('subject')
        p.add_argument('--out', type=Path, required=True)
        p.add_argument('--cache', type=Path, default=Path('/tmp/presso-source-downloads'))
    sub.add_parser('verify').add_argument('directory', type=Path)
    args = parser.parse_args()
    if args.command == 'verify':
        verify(args.directory)
        return
    if args.out.exists() and any(args.out.iterdir()):
        raise ValueError('output directory must be empty; never reuse a stale manifest')
    bundle = Bundle(args.out.resolve(), args.cache.resolve())
    if args.command == 'binary':
        bundle.go_binary(Path(args.subject), 'pressod')
        bundle.frontend()
        bundle.finish({'kind': 'binary', 'sha256': digest(args.subject)})
    else:
        with tempfile.TemporaryDirectory(prefix='presso-image-sources-') as tmp:
            image = Image(args.subject, Path(tmp))
            collect_image(bundle, image)
            bundle.finish({'kind': 'image', 'image_id': image.id, 'architecture': image.inspect['Architecture'], 'os': image.inspect['Os']})
    print(f'Verified compliance bundle: {args.out}', flush=True)


# Providers cover presso's documented release artifacts. Unfamiliar base
# distributions and unresolved source replacements fail collection; extend
# this inventory when introducing additional independently installed software.

def alpine(bundle, image):
    # APK records retain the exact aports commit and origin of each binary.
    records = []
    for block in image.read('lib/apk/db/installed').decode().split('\n\n'):
        row = {}
        for line in block.splitlines():
            if len(line) > 2 and line[1] == ':' and line[0] in 'PVLocA':
                row[line[0]] = line[2:]
        if 'P' in row:
            records.append(row)
    if not records:
        raise ValueError('Alpine package inventory missing')
    with tempfile.TemporaryDirectory(prefix='presso-aports-') as tmp:
        work = Path(tmp)
        packages = work / 'packages'
        packages.mkdir()
        for origin, commit in sorted({(r['o'], r['c']) for r in records}):
            if not re.fullmatch('[a-f0-9]{40}', commit):
                raise ValueError(f"missing aports revision: {origin}")
            url = f'https://codeload.github.com/alpinelinux/aports/tar.gz/{commit}'
            archive = fetch(url, bundle.cache)
            dest = packages / safe_name(origin)
            dest.mkdir()
            with tarfile.open(archive) as t:
                found = False
                for m in t:
                    parts = PurePosixPath(m.name).parts
                    if len(parts) >= 4 and parts[1] in ('main', 'community', 'testing') and parts[2] == origin:
                        local = '/'.join(parts[3:])
                        checked_path(local)
                        if m.isfile():
                            p = dest / local
                            p.parent.mkdir(parents=True, exist_ok=True)
                            p.write_bytes(t.extractfile(m).read())
                            found = True
                if not found:
                    raise ValueError(f"aports recipe unavailable: {origin}@{commit}")
            if not (dest / 'APKBUILD').is_file():
                raise ValueError(f"missing APKBUILD: {origin}")
            expected = {r['V'] for r in records if r['o'] == origin}
            (dest / 'expected-version').write_text('\n'.join(sorted(expected)) + '\n')
            bundle.components.append({'kind': 'apk-source', 'name': origin, 'revision': commit, 'recipe_url': url})
        # Only the isolated helper executes the distribution's package recipes.
        # It has no Docker socket, credentials, repository or host-state mount.
        version = image.read('etc/alpine-release').decode().strip()
        minor = '.'.join(version.split('.')[:2])
        run('docker', 'run', '--rm', '--mount', f'type=bind,src={work},dst=/work',
            '--entrypoint', 'sh', f'alpine:{minor}', '-ec', ALPINE_BOOTSTRAP,
            'presso-source-helper', str(os.getuid()), str(os.getgid()), ALPINE_FETCH)
        with tempfile.NamedTemporaryFile(suffix='.tar.gz') as f:
            with tarfile.open(f.name, 'w:gz', dereference=True) as t:
                t.add(packages, arcname='aports')
                t.add(work / 'distfiles', arcname='distfiles')
            source = bundle.add_file(Path(f.name), 'sources/alpine/aports-and-distfiles.tar.gz')
        count = 0
        for p in (work / 'distfiles').iterdir():
            if p.is_file():
                for name, text in archive_notices(p).items():
                    bundle.notice('alpine/' + p.name, name, text)
                    count += 1
        if not count:
            raise ValueError('no Alpine upstream license texts found')
        for r in records:
            bundle.components.append({'kind': 'apk', 'name': r['P'], 'version': r['V'], 'license': r['L'], 'origin': r['o'], 'revision': r['c'], 'source': source})


def php_runtime(bundle, image):
    config = json.loads(image.execute('php', '-r', '''$ext=[];foreach(get_loaded_extensions() as $e){$r=new ReflectionExtension($e);$ext[$e]=$r->getVersion();}echo json_encode(['version'=>PHP_VERSION,'extensions'=>$ext]);'''))
    php_version = config['version']
    src = image.temp / 'php.tar.xz'
    src.write_bytes(image.read('usr/src/php.tar.xz'))
    bundled = set()
    with tarfile.open(src) as t:
        names = t.getnames()
        if not any(n.startswith('php-' + php_version + '/') for n in names):
            raise ValueError('PHP source archive does not match the installed runtime')
        for name in names:
            parts = PurePosixPath(name).parts
            if len(parts) >= 3 and parts[1] == 'ext':
                bundled.add(parts[2].lower())
    source = bundle.add_file(src, 'sources/php/php-' + php_version + '.tar.xz')
    for name, text in archive_notices(src).items():
        bundle.notice('PHP ' + php_version, name, text)
    bundle.components.append({'kind': 'php', 'name': 'php', 'version': php_version, 'source': source, 'extensions': config['extensions']})
    options = image.execute('php-config', '--configure-options')
    target = bundle.out / 'sources/php/configure-options.txt'
    target.write_bytes(options)
    for ext, version in config['extensions'].items():
        if ext in ('imagick', 'redis'):
            source, notices = bundle.source('pecl-' + ext + '@' + version, f'https://pecl.php.net/get/{ext}-{version}.tgz', 'source.tgz')
            if not notices:
                raise ValueError(f"PECL license missing: {ext}")
            bundle.components.append({'kind': 'pecl', 'name': ext, 'version': version, 'source': source})
        elif ext.lower() not in bundled and ext not in ('Core', 'Zend OPcache', 'PDO', 'SPL', 'Reflection', 'standard', 'date', 'random', 'lexbor', 'uri'):
            raise ValueError(f"unmapped PHP extension: {ext}")
    # Keep actual upstream build helper source as shipped, not a moving tag.
    for name, m in image.members.items():
        if m.isfile() and ('install-php-extensions' in name or PurePosixPath(name).name.startswith('docker-php-')):
            p = bundle.out / 'sources/php/helpers' / PurePosixPath(name).name
            p.parent.mkdir(parents=True, exist_ok=True)
            p.write_bytes(image.read(name))
    if 'usr/local/bin/wp' in image.members:
        wpcli(bundle, image)


def wpcli(bundle, image):
    # InstalledVersions contains removed dev packages too; inspect the PHAR
    # paths to determine which packages are actually present in this artifact.
    probe = '''copy('/usr/local/bin/wp','/tmp/wp.phar');require 'phar:///tmp/wp.phar/vendor/composer/InstalledVersions.php';$out=[];foreach(Composer\\InstalledVersions::getAllRawData() as $set){foreach($set['versions'] as $n=>$p){if(isset($p['install_path']) && is_dir($p['install_path'])){$out[$n]=['version'=>$p['pretty_version'],'reference'=>$p['reference']];}}}echo json_encode($out);'''
    actual = json.loads(image.execute('php', '-r', probe))
    ver = actual['wp-cli/wp-cli']['version'].lstrip('v')
    lock_url = f'https://raw.githubusercontent.com/wp-cli/wp-cli-bundle/v{ver}/composer.lock'
    lock = json.loads(fetch(lock_url, bundle.cache).read_text())
    packages = {p['name']: p for p in lock['packages'] + lock.get('packages-dev', [])}
    bundle.source('wp-cli-bundle@' + ver, f'https://codeload.github.com/wp-cli/wp-cli-bundle/tar.gz/v{ver}', 'build-source.tar.gz')
    # Preserve the original PHAR, which is PHP source, and its generated loader.
    p = image.temp / 'wp.phar'
    p.write_bytes(image.read('usr/local/bin/wp'))
    bundle.add_file(p, 'sources/wp-cli/wp.phar')
    bundle.add_file(fetch(lock_url, bundle.cache), 'sources/wp-cli/composer.lock', lock_url)
    for name, a in actual.items():
        if name == 'wp-cli/wp-cli-bundle':
            continue
        if name not in packages:
            raise ValueError(f"WP-CLI package absent from upstream lock: {name}")
        pkg = packages[name]
        source = pkg['source']
        if source['reference'] != a['reference']:
            raise ValueError(f"WP-CLI source reference mismatch: {name}")
        match = re.fullmatch(r'https://github.com/([\w.-]+/[\w.-]+?)(?:\.git)?', source['url'])
        if not match or not re.fullmatch('[a-f0-9]{40}', source['reference']):
            raise ValueError(f"unsupported Composer source: {name}")
        path, notices = bundle.source(name + '@' + source['reference'], f'https://codeload.github.com/{match[1]}/tar.gz/{source["reference"]}', 'source.tar.gz')
        # This exact revision declares MIT in composer.json but has no license
        # file. Preserve that evidence and an explicitly attributed supplement;
        # never turn arbitrary missing licenses into a guessed SPDX notice.
        if not notices and name == 'nb/oxymel' and source['reference'] == 'cbe626ef55d5c4cc9b5e6e3904b395861ea76e3c':
            with tarfile.open(bundle.out / path) as t:
                member = next(m for m in t if m.name.count('/') == 1 and m.name.endswith('/composer.json'))
                evidence = t.extractfile(member).read()
            metadata = json.loads(evidence)
            if metadata['license'] != 'MIT' or metadata['authors'][0]['name'] != 'Nikolay Bachiyski':
                raise ValueError('Oxymel license evidence changed')
            bundle.notice(name, 'composer.json', evidence)
            supplement = (ROOT / 'scripts/compliance/notices/nb-oxymel.txt').read_bytes()
            bundle.notice(name, 'NOTICE.txt', supplement)
            notices = {'NOTICE.txt': supplement}
        if not notices:
            raise ValueError(f"Composer dependency notice missing: {name}")
        bundle.components.append({'kind': 'composer', 'name': name, 'version': a['version'], 'reference': a['reference'], 'source': path})


def collect_image(bundle, image):
    os_info = image.read('etc/os-release').decode()
    if re.search(r'^ID=debian$', os_info, re.M):
        debian(bundle, image)
    elif re.search(r'^ID=alpine$', os_info, re.M):
        alpine(bundle, image)
    else:
        raise ValueError('unsupported image distribution; source collector required')
    go_paths = ('usr/bin/caddy', 'usr/local/bin/frankenphp', 'usr/local/bin/pressod')
    for name in go_paths:
        if name in image.members:
            binary = image.temp / PurePosixPath(name).name
            binary.write_bytes(image.read(name))
            bundle.go_binary(binary, name, generated_main='usr/share/presso/build/edge/main.go' in image.members)
    if 'usr/local/bin/php' in image.members:
        php_runtime(bundle, image)
    # All custom source/configuration and notices already carried by the image
    # are retained, including WordPress, system plugin and vendored drop-ins.
    with tempfile.NamedTemporaryFile(suffix='.tar.gz') as f:
        with tarfile.open(f.name, 'w:gz') as out:
            for name, m in image.members.items():
                if m.isfile() and (name.startswith(('opt/presso/', 'var/www/html/', 'etc/presso/', 'usr/share/presso/build/')) or name.startswith('usr/local/bin/presso-')):
                    data = image.read(name)
                    info = tarfile.TarInfo(name)
                    info.size = len(data)
                    info.mode = m.mode
                    out.addfile(info, io.BytesIO(data))
                    if NOTICE.search(name):
                        bundle.notice('image-sources', name, data)
        bundle.add_file(Path(f.name), 'sources/image-shipped-source.tar.gz')
    bundle.components.append({'kind': 'container', 'name': image.ref, 'image_id': image.id})
    (bundle.out / 'image-config.json').write_text(json.dumps(image.inspect, indent=2) + '\n')


if __name__ == '__main__':
    main()
