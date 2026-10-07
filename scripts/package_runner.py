#!/usr/bin/env python3
"""Build a deterministic local experimental runner package, never publish it."""
import argparse
import gzip
import hashlib
import io
import json
import platform
from pathlib import Path
import subprocess
import tarfile

ROOT = Path(__file__).resolve().parents[1]
VERSION = '0.5.0'
IDENTITY = 'gosvm-svm-runner 0.5.0 (LiteSVM 0.8.2 + rent-error patch)'
TARGET = 'aarch64-apple-darwin'


def encoded(value):
    return (json.dumps(value, indent=2, sort_keys=True) + '\n').encode()


def sha(data):
    return hashlib.sha256(data).hexdigest()


def command(*args):
    return subprocess.check_output([str(a) for a in args], cwd=ROOT, text=True).strip()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--target-dir', type=Path, default=ROOT / 'build/svm-fixtures-target')
    args = parser.parse_args()
    if (platform.system(), platform.machine()) != ('Darwin', 'arm64'):
        parser.error('only the locally validated macOS arm64 package is enabled')
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=False)
    target = args.target_dir.resolve()
    subprocess.run(['cargo', 'build', '--release', '--locked', '--offline', '--bin', 'gosvm-svm-runner',
                    '--manifest-path', str(ROOT / 'benchmarks/svm-runner/Cargo.toml'),
                    '--target-dir', str(target)], cwd=ROOT, check=True)
    binary = target / 'release/gosvm-svm-runner'
    assert command(binary, '--version') == IDENTITY
    subprocess.run(['codesign', '--verify', str(binary)], check=True)
    metadata = json.loads(command('cargo', 'metadata', '--locked', '--offline', '--format-version', '1',
                                  '--filter-platform', TARGET, '--manifest-path', 'benchmarks/svm-runner/Cargo.toml'))
    nodes = {n['id']: n for n in metadata['resolve']['nodes']}
    pending, ids = [metadata['resolve']['root']], set()
    while pending:
        key = pending.pop()
        if key not in ids:
            ids.add(key)
            pending.extend(d['pkg'] for d in nodes[key]['deps'])
    files = {'bin/gosvm-svm-runner': binary.read_bytes(),
             'THIRD_PARTY_NOTICES': (ROOT / 'THIRD_PARTY_NOTICES').read_bytes(),
             'Cargo.lock': (ROOT / 'benchmarks/svm-runner/Cargo.lock').read_bytes(),
             'litesvm/GOSVM_PATCH.md': (ROOT / 'third_party/litesvm-0.8.2/GOSVM_PATCH.md').read_bytes(),
             'litesvm/GOSVM_PROVENANCE.json': (ROOT / 'third_party/litesvm-0.8.2/GOSVM_PROVENANCE.json').read_bytes()}
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
            files[member] = content
            members.append(member)
        dependencies.append({'name': package['name'], 'version': package['version'],
                             'source': package['source'], 'license': package['license'],
                             'features': sorted(nodes[package['id']]['features']), 'license_members': members})
    files['dependencies.json'] = encoded(dependencies)
    sources = [ROOT / 'scripts/package_runner.py', ROOT / 'THIRD_PARTY_NOTICES',
               ROOT / 'third_party/litesvm-0.8.2/GOSVM_PATCH.md']
    for directory in ('benchmarks/svm-runner', 'third_party/litesvm-0.8.2'):
        sources.extend(p for p in (ROOT / directory).rglob('*') if p.is_file() and p.suffix != '.md')
    source_hashes = {str(p.relative_to(ROOT)): sha(p.read_bytes()) for p in sorted(set(sources))}
    files['provenance.json'] = encoded({'schema': 1, 'rust_target': TARGET,
        'rustc': command('rustc', '--version'), 'cargo': command('cargo', '--version'),
        'source_sha256': source_hashes, 'binary_sha256': sha(files['bin/gosvm-svm-runner']),
        'dynamic_dependencies': command('otool', '-L', binary).splitlines()[1:],
        'macho_build_version': command('vtool', '-show-build', binary).splitlines()[1:],
        'scope': 'Local experimental package; license inventory is not redistribution approval; no public release or notarization'})
    files['manifest.json'] = encoded({'schema': 1, 'version': VERSION, 'identity': IDENTITY,
                                     'platform': 'darwin-arm64', 'rust_target': TARGET})
    # Fixed metadata, ordering, gzip timestamp and filename make the same inputs
    # reproduce the same archive bytes. Toolchain-to-toolchain builds may differ.
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
                      'sha256': pin['archive_sha256'], 'binary_bytes': binary.stat().st_size,
                      'packages': len(dependencies),
                      'packages_without_license_files': sum(not p['license_members'] for p in dependencies)}, indent=2))


if __name__ == '__main__':
    main()
