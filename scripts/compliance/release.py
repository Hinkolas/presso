#!/usr/bin/env python3
"""Package verified materials and enforce source-before-binary publication."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile
import urllib.request
import uuid

from collect import ROOT, digest, run, safe_name, verify

PART_SIZE = 1024 * 1024 * 1024  # below GitHub's 2 GiB per-asset limit


def pack(directory, output, name, part_size=PART_SIZE):
    manifest = verify(directory)
    name = safe_name(name) + '-' + digest(directory / 'manifest.json')[:16]
    output.mkdir(parents=True, exist_ok=True)
    with tempfile.NamedTemporaryFile(suffix='.tar.gz') as f:
        with tarfile.open(f.name, 'w:gz') as t:
            t.add(directory, arcname=name)
        parts = []
        with open(f.name, 'rb') as source:
            index = 0
            while data := source.read(min(part_size, 1024 * 1024)):
                part = output / f'{name}.tar.gz.part-{index:04d}'
                with part.open('wb') as dest:
                    dest.write(data)
                    remaining = part_size - len(data)
                    while remaining and (data := source.read(min(remaining, 1024 * 1024))):
                        dest.write(data)
                        remaining -= len(data)
                parts.append(part)
                index += 1
    record = output / (name + '.manifest.json')
    record.write_text(json.dumps({'bundle': name, 'subject': manifest['subject'], 'parts': [
        {'name': p.name, 'sha256': digest(p), 'size': p.stat().st_size} for p in parts
    ], 'manifest_sha256': digest(directory / 'manifest.json')}, indent=2) + '\n')
    checksum = output / (name + '.sha256')
    checksum.write_text(''.join(f'{digest(p)}  {p.name}\n' for p in parts + [record]))
    return name, parts + [record, checksum]


def repo():
    value = os.environ.get('GITHUB_REPOSITORY', 'Hinkolas/presso')
    import re
    if not re.fullmatch(r'[\w.-]+/[\w.-]+', value):
        raise ValueError('invalid repository')
    return value


def prepare(release):
    public = json.loads(run('gh', 'repo', 'view', repo(), '--json', 'visibility'))
    if public['visibility'] != 'PUBLIC':
        raise ValueError('Source downloads must be public before image publication. Make the repository public first; this tool will not change its visibility.')
    tag = 'sources-' + release
    view = subprocess.run(['gh', 'release', 'view', tag, '--repo', repo(), '--json', 'isDraft,isPrerelease'], capture_output=True)
    if view.returncode == 0:
        state = json.loads(view.stdout)
        if state['isDraft'] or not state['isPrerelease']:
            raise ValueError('existing source release must be a published prerelease')
        return
    with tempfile.NamedTemporaryFile(mode='w', suffix='.md') as body:
        body.write(f'Source code and third-party notices for presso {release}.\n\n'
                   'These are distribution materials, not an installable presso release. '
                   'Each bundle is uploaded and its anonymous download checked before the corresponding image is published. '
                   'Keep all parts and use the included checksums. See SOURCE_ACCESS.md in each bundle.\n')
        body.flush()
        run('gh', 'release', 'create', tag, '--repo', repo(), '--target', os.environ['GITHUB_SHA'],
            '--title', f'Source materials for {release}', '--prerelease', '--latest=false', '--notes-file', body.name)


def publish_sources(release, files):
    # Called only after prepare, independently by native architecture jobs.
    # Asset names include content identity, so retries never replace materials.
    tag = 'sources-' + release
    existing = json.loads(run('gh', 'release', 'view', tag, '--repo', repo(), '--json', 'isDraft,isPrerelease,assets'))
    if existing['isDraft'] or not existing['isPrerelease']:
        raise ValueError('source release is not publicly published as a prerelease')
    names = {a['name'] for a in existing['assets']}
    for f in files:
        if f.name not in names:
            run('gh', 'release', 'upload', tag, str(f), '--repo', repo())
        url = f'https://github.com/{repo()}/releases/download/{tag}/{f.name}'
        # Do not use gh/curl authentication: this proves recipients can obtain
        # exactly the bytes whose manifest we verified, including on retries.
        h = hashlib.sha256()
        with urllib.request.urlopen(url, timeout=120) as response:
            for chunk in iter(lambda: response.read(1024 * 1024), b''):
                h.update(chunk)
        if h.hexdigest() != digest(f):
            raise ValueError(f'public source asset is missing or differs: {f.name}')


def binary(args):
    out = ROOT / 'compliance-output' / ('binary_' + args.arch)
    # Each hook invocation owns one architecture directory. Refuse stale runs.
    if out.exists():
        shutil.rmtree(out)
    run('python3', str(ROOT / 'scripts/compliance/collect.py'), 'binary', str(Path(args.binary).resolve()), '--out', str(out / 'bundle'))
    name, files = pack(out / 'bundle', out / 'assets', f'pressod-{args.release}-{args.arch}-sources')
    for n in ('THIRD_PARTY_LICENSES.txt', 'SOURCE_ACCESS.md'):
        shutil.copyfile(out / 'bundle' / n, out / n)
    (out / 'SOURCE_BUNDLE').write_text(f'{name}\nhttps://github.com/{repo()}/releases/tag/sources-{args.release}\n')
    if os.environ.get('PRESSO_PUBLISH_SOURCES') == '1':
        publish_sources(args.release, files)


def image(args):
    with tempfile.TemporaryDirectory(prefix='presso-distribution-') as td:
        directory = Path(td)
        bundle = directory / 'bundle'
        # Collection and hash verification must precede both source upload and
        # binary publication. No --skip flag exists on the publishing path.
        run('python3', str(ROOT / 'scripts/compliance/collect.py'), 'image', args.image, '--out', str(bundle))
        m = verify(bundle)
        arch = m['subject']['architecture']
        name, files = pack(bundle, args.output, safe_name(args.target) + '-' + arch + '-sources')
        if args.push:
            publish_sources(args.release, files)
        # Only attach documentation: no packages are installed after collection.
        # The base image ID in the manifest identifies the exact collected root.
        ctx = directory / 'context'
        ctx.mkdir()
        for n in ('THIRD_PARTY_LICENSES.txt', 'SOURCE_ACCESS.md', 'manifest.json'):
            shutil.copyfile(bundle / n, ctx / n)
        (ctx / 'SOURCE_BUNDLE').write_text(f'{name}\nhttps://github.com/{repo()}/releases/tag/sources-{args.release}\n')
        (ctx / 'Dockerfile').write_text('ARG BASE=scratch\nFROM ${BASE}\nCOPY . /usr/share/licenses/presso/\n'
                                      'ARG SOURCE_URL\nLABEL org.opencontainers.image.source="https://github.com/Hinkolas/presso" '
                                      'io.presso.source-bundle=$SOURCE_URL\n')
        base = m['subject']['image_id']
        # BuildKit interprets raw config IDs as registry names. Give the exact
        # inspected ID a unique local tag; verify its rootfs below before push.
        base_tag = 'presso-compliance-base:' + uuid.uuid4().hex
        run('docker', 'tag', base, base_tag)
        try:
            run('docker', 'build', '--build-arg', 'BASE=' + base_tag, '--build-arg', 'SOURCE_URL=https://github.com/' + repo() + '/releases/tag/sources-' + args.release,
                '-t', args.target, str(ctx))
        finally:
            run('docker', 'image', 'rm', base_tag)
        final = json.loads(run('docker', 'image', 'inspect', args.target))[0]
        # Documentation adds one filesystem layer; the original rootfs remains
        # an exact prefix. No binary/source mismatch may pass this boundary.
        original = json.loads(run('docker', 'image', 'inspect', base))[0]
        layers = original['RootFS']['Layers']
        if final['RootFS']['Layers'][:len(layers)] != layers or len(final['RootFS']['Layers']) != len(layers) + 1:
            raise ValueError('distribution image no longer matches collected rootfs')
        if args.push:
            run('docker', 'push', args.target)
            final = json.loads(run('docker', 'image', 'inspect', args.target))[0]
        record = {'image': args.target, 'image_id': final['Id'], 'source_image_id': base, 'bundle': name,
                  'architecture': arch, 'repo_digests': final.get('RepoDigests', [])}
        args.output.mkdir(parents=True, exist_ok=True)
        (args.output / (safe_name(args.target) + '.image.json')).write_text(json.dumps(record, indent=2) + '\n')


def main():
    p = argparse.ArgumentParser(description=__doc__)
    sub = p.add_subparsers(dest='command', required=True)
    prepare_parser = sub.add_parser('prepare')
    prepare_parser.add_argument('--release', required=True)
    b = sub.add_parser('binary')
    b.add_argument('--binary', required=True)
    b.add_argument('--arch', choices=['amd64', 'arm64'], required=True)
    b.add_argument('--release', required=True)
    i = sub.add_parser('image')
    i.add_argument('--image', required=True)
    i.add_argument('--target', required=True)
    i.add_argument('--release', required=True)
    i.add_argument('--output', type=Path, required=True)
    i.add_argument('--push', action='store_true')
    args = p.parse_args()
    if '/' in args.release or not args.release or args.release.startswith('-'):
        raise ValueError('invalid release name')
    {'prepare': lambda a: prepare(a.release), 'binary': binary, 'image': image}[args.command](args)


if __name__ == '__main__':
    main()
