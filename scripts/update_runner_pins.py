#!/usr/bin/env python3
"""Write frontend release pins for runner archives (maintainer review step).

Usage: python3 scripts/update_runner_pins.py ARCHIVE.tar.gz [...] [--check]

Each pin is derived from the archive bytes alone (archive size/SHA-256 and
every member's size/SHA-256), never from a pin.json shipped beside it. The
archive's manifest.json must name a known runner version, identity and
platform, and the archive must satisfy the installer's member rules. Writes
internal/runner/pins/release/<version>-<platform>.json. --check only reports
whether the existing pins already equal the archives (exit 1 otherwise).
Rebuild the CLI after changing pins; review the diff before committing.
"""
import argparse
import hashlib
import io
import json
from pathlib import Path, PurePosixPath
import sys
import tarfile

ROOT = Path(__file__).resolve().parents[1]
PINS = ROOT / 'internal/runner/pins/release'
IDENTITIES = {
    '0.5.0': 'gosvm-svm-runner 0.5.0 (LiteSVM 0.8.2 + rent-error patch)',
    '0.5.1': 'gosvm-svm-runner 0.5.1 (LiteSVM 0.8.2 + rent-error patch)',
    '0.6.0': 'gosvm-svm-runner 0.6.0 (LiteSVM 0.13.0 + rent-error patch; modern profiles)',
}
PLATFORMS = {'darwin-arm64', 'linux-amd64', 'linux-arm64'}


def sha(data):
    return hashlib.sha256(data).hexdigest()


def pin_for(path):
    data = path.read_bytes()
    files = {}
    with tarfile.open(fileobj=io.BytesIO(data), mode='r:gz') as tar:
        for member in tar.getmembers():
            name = member.name
            p = PurePosixPath(name)
            if (not member.isreg() or p.is_absolute() or '..' in p.parts or str(p) != name or '\\' in name
                    or name in files or member.size <= 0 or member.size > 32 << 20):
                sys.exit(f'{path}: unsafe, duplicate, empty or oversized member {name!r}')
            content = tar.extractfile(member).read()
            files[name] = {'sha256': sha(content), 'bytes': len(content)}
            if name == 'manifest.json':
                manifest = json.loads(content)
    if sum(f['bytes'] for f in files.values()) > 64 << 20:
        sys.exit(f'{path}: package exceeds the installer extraction limit')
    if 'bin/gosvm-svm-runner' not in files or 'manifest.json' not in files:
        sys.exit(f'{path}: missing runner binary or manifest')
    version, platform = manifest.get('version'), manifest.get('platform')
    if IDENTITIES.get(version) != manifest.get('identity') or platform not in PLATFORMS:
        sys.exit(f'{path}: unknown runner version/identity/platform {manifest}')
    name = f'gosvm-svm-runner-{version}-{platform}.tar.gz'
    if path.name != name:
        sys.exit(f'{path}: expected file name {name}')
    return {'schema': 1, 'version': version, 'identity': IDENTITIES[version], 'platform': platform,
            'archive_name': name, 'archive_sha256': sha(data), 'archive_bytes': len(data),
            'files': dict(sorted(files.items()))}


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('archives', nargs='+', type=Path)
    parser.add_argument('--check', action='store_true')
    args = parser.parse_args()
    stale = False
    for archive in args.archives:
        pin = pin_for(archive)
        text = json.dumps(pin, indent=2, sort_keys=True) + '\n'
        target = PINS / f"{pin['version']}-{pin['platform']}.json"
        current = target.read_text() if target.exists() else None
        if current == text:
            print(f'up to date: {target.relative_to(ROOT)} ({pin["archive_sha256"]})')
            continue
        stale = True
        if args.check:
            print(f'STALE: {target.relative_to(ROOT)} does not pin {archive.name} ({pin["archive_sha256"]})')
            continue
        PINS.mkdir(parents=True, exist_ok=True)
        target.write_text(text)
        print(f'wrote {target.relative_to(ROOT)}: {archive.name} {pin["archive_bytes"]} bytes {pin["archive_sha256"]}')
    if args.check and stale:
        sys.exit(1)


if __name__ == '__main__':
    main()
