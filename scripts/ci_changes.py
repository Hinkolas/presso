"""Scope pull request CI from the complete change set.

Regular CI is skipped only for positively identified documentation-only
changes, and image source collection (the slowest check) runs only when an
input to an image or to collection changed. Release workflows invoke
verify.yml directly and never use this filter. Read the complete git diff,
including both sides of renames, rather than a truncated API file list.
Unknown event/history/path means full verification.
"""
import json
import os
from pathlib import Path, PurePosixPath
import re
import subprocess


GUIDES = {
    'README.md', 'CONTRIBUTING.md', 'SECURITY.md', 'ROADMAP.md',
}


def documentation(path):
    p = PurePosixPath(path)
    if path == 'docs/licensing.md':
        return False
    # License/source-access files, runtime vendoring records, frontend files,
    # and non-Markdown inventory files deliberately still run full CI.
    return path in GUIDES or (path.startswith('docs/') and p.suffix == '.md')


# Inputs to the platform and runtime images or to their source bundles.
# Workflow and detector changes also collect, so CI changes are exercised.
SOURCE_DIRS = ('build/', 'runtime/', 'scripts/compliance/', '.github/workflows/')
SOURCE_FILES = {
    'docker-bake.hcl', 'internal/compat/matrix.json', 'scripts/ci_changes.py',
    'LICENSE', 'NOTICE', 'docs/licensing.md',
}


def affects_sources(path):
    return path in SOURCE_FILES or path.startswith(SOURCE_DIRS)


def changed_paths(event_name, event, directory='.'):
    """The changed paths, or None when they cannot be determined."""
    if event_name == 'pull_request':
        pr = event.get('pull_request', {})
        base, head = pr.get('base', {}).get('sha'), pr.get('head', {}).get('sha')
        separator = '...'
    elif event_name == 'push':
        base, head = event.get('before'), event.get('after')
        separator = '..'
    else:
        return None  # Manual dispatch, schedules and unfamiliar events.
    if not all(isinstance(sha, str) and re.fullmatch('[a-f0-9]{40}', sha)
               and sha != '0' * 40 for sha in (base, head)):
        return None
    diff = subprocess.run(
        ['git', 'diff', '--name-only', '-z', '--no-renames', base + separator + head, '--'],
        cwd=directory, capture_output=True,
    )
    if diff.returncode:
        return None  # Missing history, including force-pushes.
    paths = [p for p in diff.stdout.decode('utf-8', errors='replace').split('\0') if p]
    return paths or None


def needs_verification(event_name, event, directory='.'):
    paths = changed_paths(event_name, event, directory)
    return paths is None or not all(documentation(p) for p in paths)


def needs_sources(event_name, event, directory='.'):
    paths = changed_paths(event_name, event, directory)
    return paths is None or any(affects_sources(p) for p in paths)


def main():
    event = json.loads(Path(os.environ['GITHUB_EVENT_PATH']).read_text())
    name = os.environ['GITHUB_EVENT_NAME']
    verify = needs_verification(name, event)
    sources = verify and needs_sources(name, event)
    with open(os.environ['GITHUB_OUTPUT'], 'a') as output:
        output.write('run_verify=' + str(verify).lower() + '\n')
        output.write('sources=' + str(sources).lower() + '\n')
    if not verify:
        print('Documentation-only change: skipping builds and integration checks.')
    elif sources:
        print('Full verification required, including image source collection.')
    else:
        print('Verification required; no image or source collection inputs changed.')


if __name__ == '__main__':
    main()
