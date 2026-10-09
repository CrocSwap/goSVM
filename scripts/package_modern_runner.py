#!/usr/bin/env python3
"""Package the modern 0.6.0 runner deterministically and emit a reviewable pin.

The historical 0.5.0 recipe (scripts/package_runner.py) is intentionally left
unchanged so its package remains reproducible. This never publishes anything.
Copy the emitted pin.json to internal/runner/pins/0.6.0-darwin-arm64.json only
after reviewing it; installed packages are trusted solely through that pin.
"""
import argparse
import gzip
import hashlib
import io
import json
import os
import platform
from pathlib import Path
import subprocess
import tarfile

ROOT = Path(__file__).resolve().parents[1]
VERSION = '0.6.0'
IDENTITY = 'gosvm-svm-runner 0.6.0 (LiteSVM 0.13.0 + rent-error patch; modern profiles)'
TARGET = 'aarch64-apple-darwin'
MANIFEST = 'benchmarks/svm-runner-modern/Cargo.toml'


def encoded(value):
    return (json.dumps(value, indent=2, sort_keys=True) + '\n').encode()


def sha(data):
    return hashlib.sha256(data).hexdigest()


def command(*args, env=None):
    return subprocess.check_output([str(a) for a in args], cwd=ROOT, text=True, env=env).strip()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, required=True, help='new directory for the archive and pin')
    parser.add_argument('--binary', type=Path,
                        help='package an existing runner build instead of building one')
    parser.add_argument('--target-dir', type=Path, default=ROOT / 'build/modern-runtime-target')
    args = parser.parse_args()
    if (platform.system(), platform.machine()) != ('Darwin', 'arm64'):
        parser.error('only the locally validated macOS arm64 package is enabled')
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=False)
    # Reconstruct/verify the pinned, patched LiteSVM source; it also makes the
    # Cargo patch path resolvable for the license inventory below.
    subprocess.run(['python3', str(ROOT / 'scripts/build_modern_runner.py'), '--offline', '--prepare-only'],
                   cwd=ROOT, check=True)
    if args.binary:
        binary = args.binary.resolve()
        build = {'kind': 'prebuilt', 'path': str(binary),
                 'note': 'Compiler of a supplied binary is not observable here; compare binary_sha256 with qualification evidence'}
    else:
        env = dict(os.environ, CARGO_TARGET_DIR=str(args.target_dir.resolve()))
        subprocess.run(['python3', str(ROOT / 'scripts/build_modern_runner.py'), '--offline'], cwd=ROOT, env=env, check=True)
        binary = args.target_dir.resolve() / 'release/gosvm-svm-runner'
        build = {'kind': 'built', 'rustc': command('rustc', '--version'), 'cargo': command('cargo', '--version')}
    assert command(binary, '--version') == IDENTITY, 'unexpected runner identity'
    subprocess.run(['codesign', '--verify', str(binary)], check=True)
    metadata = json.loads(command('cargo', 'metadata', '--locked', '--offline', '--format-version', '1',
                                  '--filter-platform', TARGET, '--manifest-path', MANIFEST))
    nodes = {n['id']: n for n in metadata['resolve']['nodes']}
    pending, ids = [metadata['resolve']['root']], set()
    while pending:
        key = pending.pop()
        if key not in ids:
            ids.add(key)
            pending.extend(d['pkg'] for d in nodes[key]['deps'])
    files = {'bin/gosvm-svm-runner': binary.read_bytes(),
             'THIRD_PARTY_NOTICES': (ROOT / 'THIRD_PARTY_NOTICES').read_bytes(),
             'Cargo.lock': (ROOT / 'benchmarks/svm-runner-modern/Cargo.lock').read_bytes(),
             'litesvm/build_modern_runner.py': (ROOT / 'scripts/build_modern_runner.py').read_bytes()}
    dependencies = []
    for package in sorted((p for p in metadata['packages'] if p['id'] in ids), key=lambda p: (p['name'], p['version'])):
        directory = Path(package['manifest_path']).parent
        candidates = {p for p in directory.rglob('*') if p.is_file() and
                      p.name.upper().startswith(('LICENSE', 'LICENCE', 'COPYING', 'NOTICE'))}
        if package['license_file']:
            candidates.add(directory / package['license_file'])
        members = []
        for source in sorted(candidates):
            relative = source.relative_to(directory)
            member = f"licenses/{package['name']}-{package['version']}/{relative.as_posix()}"
            content = source.read_bytes()
            assert len(content) <= 1 << 20, member
            if not content:
                continue  # The installer rejects empty members; an empty file carries no text.
            files[member] = content
            members.append(member)
        dependencies.append({'name': package['name'], 'version': package['version'],
                             'source': package['source'], 'license': package['license'],
                             'features': sorted(nodes[package['id']]['features']), 'license_members': members})
    files['dependencies.json'] = encoded(dependencies)
    sources = [ROOT / 'scripts/package_modern_runner.py', ROOT / 'scripts/build_modern_runner.py',
               ROOT / 'THIRD_PARTY_NOTICES']
    # Documentation is excluded so doc edits do not change the pinned package.
    sources.extend(p for p in (ROOT / 'benchmarks/svm-runner-modern').rglob('*') if p.is_file() and p.suffix != '.md')
    vendor = ROOT / 'build/modern-runtime-vendor/litesvm-0.13.0'
    prepared = {str(p.relative_to(vendor)): sha(p.read_bytes()) for p in sorted(vendor.rglob('*')) if p.is_file()}
    files['provenance.json'] = encoded({'schema': 1, 'rust_target': TARGET, 'build': build,
        'source_sha256': {str(p.relative_to(ROOT)): sha(p.read_bytes()) for p in sorted(set(sources))},
        'prepared_litesvm_sha256': sha(encoded(prepared)),
        'binary_sha256': sha(files['bin/gosvm-svm-runner']),
        'dynamic_dependencies': command('otool', '-L', binary).splitlines()[1:],
        'macho_build_version': command('vtool', '-show-build', binary).splitlines()[1:],
        'scope': 'Local experimental package; license inventory is not redistribution approval; no public release or notarization'})
    files['manifest.json'] = encoded({'schema': 1, 'version': VERSION, 'identity': IDENTITY,
                                     'platform': 'darwin-arm64', 'rust_target': TARGET})
    # Fixed metadata, ordering, gzip timestamp and filename make the same inputs
    # reproduce the same archive bytes.
    archive = output / f'gosvm-svm-runner-{VERSION}-darwin-arm64.tar.gz'
    with archive.open('wb') as raw:
        with gzip.GzipFile(filename='', mode='wb', fileobj=raw, mtime=0, compresslevel=9) as compressed:
            with tarfile.open(fileobj=compressed, mode='w', format=tarfile.USTAR_FORMAT) as tar:
                for name, data in sorted(files.items()):
                    info = tarfile.TarInfo(name)
                    info.size = len(data)
                    info.mode = 0o755 if name == 'bin/gosvm-svm-runner' else 0o644
                    tar.addfile(info, io.BytesIO(data))
    pin = {'schema': 1, 'version': VERSION, 'identity': IDENTITY, 'platform': 'darwin-arm64',
           'archive_name': archive.name, 'archive_sha256': sha(archive.read_bytes()),
           'archive_bytes': archive.stat().st_size,
           'files': {name: {'sha256': sha(data), 'bytes': len(data)} for name, data in sorted(files.items())}}
    (output / 'pin.json').write_bytes(encoded(pin))
    (output / 'dependencies.json').write_bytes(files['dependencies.json'])
    (output / 'provenance.json').write_bytes(files['provenance.json'])
    print(json.dumps({'archive': str(archive), 'bytes': pin['archive_bytes'],
                      'sha256': pin['archive_sha256'], 'binary_sha256': sha(files['bin/gosvm-svm-runner']),
                      'packages': len(dependencies),
                      'packages_without_license_files': sum(not p['license_members'] for p in dependencies)}, indent=2))


if __name__ == '__main__':
    main()
