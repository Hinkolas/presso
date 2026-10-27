#!/usr/bin/env python3
"""Scan all available Git history and publishable working files with Gitleaks.

Requires Gitleaks 8.30.1 on PATH. Ignored local state is never copied into the
working-file scan; tracked files are scanned even when an ignore rule matches.
Scanner output is redacted. Do not upload scanner reports as public artifacts.
"""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile


def main():
    root = Path(__file__).resolve().parent.parent
    scanner = shutil.which('gitleaks')
    if scanner is None:
        raise SystemExit('Install Gitleaks 8.30.1 and put gitleaks on PATH first.')
    common = [scanner, '--config', str(root / '.gitleaks.toml'),
              '--redact=100', '--no-banner', '--ignore-gitleaks-allow']
    # Both scans run even when history has a finding. Errors fail the check too.
    history = subprocess.run(common + ['git', '--log-opts=--all', str(root)])
    with tempfile.TemporaryDirectory(prefix='presso-secret-scan-') as directory:
        target = Path(directory)
        paths = subprocess.check_output(
            ['git', 'ls-files', '--cached', '--others', '--exclude-standard', '-z'],
            cwd=root,
        )
        for name in set(paths.split(b'\0')) - {b''}:
            relative = Path(os.fsdecode(name))
            source, dest = root / relative, target / relative
            if not source.exists() and not source.is_symlink():
                continue  # A tracked deletion in the working tree.
            dest.parent.mkdir(parents=True, exist_ok=True)
            if source.is_symlink():
                dest.write_text(os.readlink(source))
            else:
                shutil.copyfile(source, dest)
        working = subprocess.run(common + ['dir', str(target)])
    raise SystemExit(1 if history.returncode or working.returncode else 0)


if __name__ == '__main__':
    main()
