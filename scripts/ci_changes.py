"""Skip regular CI only for positively identified documentation-only changes.

Release workflows invoke verify.yml directly and never use this filter.
Read the complete git diff, including both sides of renames, rather than a
truncated API file list. Unknown event/history/path means full verification.
"""
import json
import os
from pathlib import Path, PurePosixPath
import re
import subprocess


GUIDES = {
    'README.md', 'CONTRIBUTING.md', 'SECURITY.md', 'ROADMAP.md',
    'ORCHESTRATION.md', 'RUNTIME.md', 'HARNESS.md', 'RELEASE_VERIFICATION.md',
}


def documentation(path):
    p = PurePosixPath(path)
    # License/source-access files, runtime vendoring records, frontend files,
    # and non-Markdown inventory files deliberately still run full CI.
    return path in GUIDES or (path.startswith('docs/') and p.suffix == '.md')


def needs_verification(event_name, event, directory='.'):
    if event_name == 'pull_request':
        pr = event.get('pull_request', {})
        base, head = pr.get('base', {}).get('sha'), pr.get('head', {}).get('sha')
        separator = '...'
    elif event_name == 'push':
        base, head = event.get('before'), event.get('after')
        separator = '..'
    else:
        return True  # Manual dispatch and unfamiliar events always verify.
    if not all(isinstance(sha, str) and re.fullmatch('[a-f0-9]{40}', sha)
               and sha != '0' * 40 for sha in (base, head)):
        return True
    diff = subprocess.run(
        ['git', 'diff', '--name-only', '-z', '--no-renames', base + separator + head, '--'],
        cwd=directory, capture_output=True,
    )
    if diff.returncode:
        return True  # Missing history (including force-pushes) is not docs-only.
    paths = [p for p in diff.stdout.decode('utf-8', errors='replace').split('\0') if p]
    return not paths or not all(documentation(p) for p in paths)


def main():
    event = json.loads(Path(os.environ['GITHUB_EVENT_PATH']).read_text())
    verify = needs_verification(os.environ['GITHUB_EVENT_NAME'], event)
    with open(os.environ['GITHUB_OUTPUT'], 'a') as output:
        output.write('run_verify=' + str(verify).lower() + '\n')
    print('Full verification required.' if verify else 'Documentation-only change: skipping builds and integration checks.')


if __name__ == '__main__':
    main()
