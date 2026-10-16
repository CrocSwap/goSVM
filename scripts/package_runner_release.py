#!/usr/bin/env python3
"""Build a deterministic SVM runner release archive and its reviewable pin.

Never publishes anything. Writes <output>/gosvm-svm-runner-<version>-<platform>.tar.gz,
pin.json (copy it into the frontend with scripts/update_runner_pins.py, which
re-derives the pin from the archive bytes), dependencies.json and provenance.json.

Binary sources:
  default            build from this checkout with Cargo (--locked). For a
                     reproducible Linux build run it through
                     scripts/build_runner_linux.sh (pinned container, fixed paths).
  --from-archive A   repackage the runner binary of an earlier pinned package A
                     (its archive must match a pin embedded in the frontend);
                     the released macOS packages reuse the qualified binaries
                     this way.
  --binary B         repackage an existing runner build (provenance records it).

Archive layout: bin/gosvm-svm-runner, LICENSE, NOTICE, THIRD_PARTY_NOTICES,
README, Cargo.lock, litesvm/ (patch provenance and LiteSVM's Apache-2.0 text),
licenses/<crate>-<version>/ (license files shipped by each crate),
licenses/Apache-2.0.txt, CRATE_LICENSES.txt (every crate in the target's
dependency graph with its declared license), dependencies.json,
provenance.json, manifest.json. Tar metadata, ordering and gzip header are
fixed, so identical inputs reproduce identical bytes.
"""
import argparse
import gzip
import hashlib
import io
import json
import os
from pathlib import Path
import platform
import shutil
import subprocess
import sys
import tarfile

ROOT = Path(__file__).resolve().parents[1]
VERSIONS = {
    '0.5.0': {
        'identity': 'gosvm-svm-runner 0.5.0 (LiteSVM 0.8.2 + rent-error patch)',
        'crate': 'benchmarks/svm-runner',
        'litesvm': {'litesvm/GOSVM_PATCH.md': 'third_party/litesvm-0.8.2/GOSVM_PATCH.md',
                    'litesvm/GOSVM_PROVENANCE.json': 'third_party/litesvm-0.8.2/GOSVM_PROVENANCE.json',
                    'litesvm/LICENSE-APACHE': 'third_party/litesvm-0.8.2/LICENSE-APACHE'},
        'historical_pin': 'internal/runner/pins/darwin-arm64.json',
    },
    '0.6.0': {
        'identity': 'gosvm-svm-runner 0.6.0 (LiteSVM 0.13.0 + rent-error patch; modern profiles)',
        'crate': 'benchmarks/svm-runner-modern',
        # The 0.13.0 crate ships no license file; upstream LiteSVM is Apache-2.0.
        'litesvm': {'litesvm/build_modern_runner.py': 'scripts/build_modern_runner.py',
                    'litesvm/LICENSE-APACHE': 'third_party/litesvm-0.8.2/LICENSE-APACHE'},
        'historical_pin': 'internal/runner/pins/0.6.0-darwin-arm64.json',
    },
}
PLATFORMS = {('Darwin', 'arm64'): ('darwin-arm64', 'aarch64-apple-darwin'),
             ('Linux', 'x86_64'): ('linux-amd64', 'x86_64-unknown-linux-gnu'),
             ('Linux', 'aarch64'): ('linux-arm64', 'aarch64-unknown-linux-gnu')}
README = """gosvm-svm-runner {version} for {platform}

{identity}

The local SVM test runtime used by `gosvm test --svm` (goSVM,
https://github.com/CrocSwap/goSVM). Install it with `gosvm runner install`,
which downloads this archive and verifies it against the SHA-256 pins built
into the gosvm CLI, or offline with `gosvm runner install -archive <this file>`.

goSVM is licensed under the Apache License 2.0 (LICENSE, NOTICE). The runner
statically links LiteSVM (Apache-2.0) with the patch described in litesvm/,
and the Rust crates listed in CRATE_LICENSES.txt under their own licenses;
license texts shipped by those crates are under licenses/. Experimental
software without a production audit; provided "AS IS" (LICENSE, section 7).
"""


def encoded(value):
    return (json.dumps(value, indent=2, sort_keys=True) + '\n').encode()


def sha(data):
    return hashlib.sha256(data).hexdigest()


def command(*args, env=None):
    return subprocess.check_output([str(a) for a in args], cwd=ROOT, text=True, env=env).strip()


def cargo(toolchain):
    return ['cargo'] + ([f'+{toolchain}'] if toolchain else [])


def historical_binary(archive, pin_path):
    """Return the runner binary of an archive that matches a historical pin."""
    pin = json.loads((ROOT / pin_path).read_text())
    data = archive.read_bytes()
    if len(data) != pin['archive_bytes'] or sha(data) != pin['archive_sha256']:
        sys.exit(f'{archive} does not match the embedded pin {pin_path}')
    with tarfile.open(fileobj=io.BytesIO(data), mode='r:gz') as tar:
        member = tar.getmember('bin/gosvm-svm-runner')
        binary = tar.extractfile(member).read()
        original = tar.extractfile(tar.getmember('provenance.json')).read()
    expected = pin['files']['bin/gosvm-svm-runner']
    if sha(binary) != expected['sha256'] or len(binary) != expected['bytes']:
        sys.exit('pinned runner binary mismatch')
    return binary, json.loads(original), pin


def build(spec, version, toolchain, target_dir, offline):
    env = dict(os.environ)
    flags = ['--release', '--locked'] + (['--offline'] if offline else [])
    if version == '0.6.0':
        prepare = [sys.executable, str(ROOT / 'scripts/build_modern_runner.py'), '--prepare-only']
        subprocess.run(prepare + (['--offline'] if offline else []), cwd=ROOT, check=True)
    subprocess.run(cargo(toolchain) + ['build', *flags, '--bin', 'gosvm-svm-runner',
                                       '--manifest-path', str(ROOT / spec['crate'] / 'Cargo.toml'),
                                       '--target-dir', str(target_dir)], cwd=ROOT, env=env, check=True)
    return target_dir / 'release/gosvm-svm-runner'


def host_details(binary, plat):
    if plat.startswith('darwin'):
        subprocess.run(['codesign', '--verify', str(binary)], check=True)
        return {'dynamic_dependencies': command('otool', '-L', binary).splitlines()[1:],
                'macho_build_version': command('vtool', '-show-build', binary).splitlines()[1:]}
    details = {}
    if shutil.which('readelf'):
        dynamic = command('readelf', '-d', binary)
        details['dynamic_dependencies'] = [l.split('[')[-1].rstrip(']') for l in dynamic.splitlines() if '(NEEDED)' in l]
        versions = command('readelf', '-V', binary)
        details['glibc_versions'] = sorted({w for w in versions.split() if w.startswith('GLIBC_')})
    if Path('/etc/os-release').exists():
        details['build_os'] = next((l.split('=', 1)[1].strip('"') for l in Path('/etc/os-release').read_text().splitlines()
                                    if l.startswith('PRETTY_NAME=')), '')
    return details


def license_inventory(spec, rust_target, toolchain, offline, files):
    metadata = json.loads(command(*cargo(toolchain), 'metadata', '--locked', *(['--offline'] if offline else []),
                                  '--format-version', '1', '--filter-platform', rust_target,
                                  '--manifest-path', f"{spec['crate']}/Cargo.toml"))
    nodes = {n['id']: n for n in metadata['resolve']['nodes']}
    pending, ids = [metadata['resolve']['root']], set()
    while pending:
        key = pending.pop()
        if key not in ids:
            ids.add(key)
            pending.extend(d['pkg'] for d in nodes[key]['deps'])
    dependencies, lines = [], []
    for package in sorted((p for p in metadata['packages'] if p['id'] in ids), key=lambda p: (p['name'], p['version'])):
        directory = Path(package['manifest_path']).parent
        candidates = {p for p in directory.rglob('*') if p.is_file() and
                      p.name.upper().startswith(('LICENSE', 'LICENCE', 'COPYING', 'NOTICE'))}
        if package['license_file']:
            candidates.add(directory / package['license_file'])
        members = []
        for source in sorted(candidates):
            member = f"licenses/{package['name']}-{package['version']}/{source.relative_to(directory).as_posix()}"
            content = source.read_bytes()
            assert len(content) <= 1 << 20, member
            if content:  # The installer rejects empty members; an empty file carries no text.
                files[member] = content
                members.append(member)
        license_name = package['license']
        if package['source'] is None and package['name'] == 'gosvm-svm-runner':
            license_name, members = 'Apache-2.0 (goSVM)', ['LICENSE', 'NOTICE']
        elif package['source'] is None and package['name'] == 'litesvm':
            members = members or ['litesvm/LICENSE-APACHE']
        dependencies.append({'name': package['name'], 'version': package['version'],
                             'source': package['source'], 'license': license_name,
                             'features': sorted(nodes[package['id']]['features']), 'license_members': members})
        texts = ', '.join(members) if members else (
            'no license file in the crate; see licenses/Apache-2.0.txt' if 'Apache-2.0' in (license_name or '')
            else 'no license file in the crate; review required')
        origin = 'crates.io' if (package['source'] or '').startswith('registry+') else (package['source'] or 'local')
        lines.append(f"{package['name']} {package['version']}\t{license_name or 'UNDECLARED'}\t{origin}\t{texts}")
    header = ('# Rust crates in the runner dependency graph for this target (build dependencies included;\n'
              '# not every crate contributes linked code). name version<TAB>declared license<TAB>source<TAB>texts\n')
    files['CRATE_LICENSES.txt'] = (header + '\n'.join(lines) + '\n').encode()
    files['dependencies.json'] = encoded(dependencies)
    return dependencies


def write_archive(path, files):
    with path.open('wb') as raw:
        with gzip.GzipFile(filename='', mode='wb', fileobj=raw, mtime=0, compresslevel=9) as compressed:
            with tarfile.open(fileobj=compressed, mode='w', format=tarfile.USTAR_FORMAT) as tar:
                for name, data in sorted(files.items()):
                    info = tarfile.TarInfo(name)
                    info.size = len(data)
                    info.mode = 0o755 if name == 'bin/gosvm-svm-runner' else 0o644
                    tar.addfile(info, io.BytesIO(data))


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--version', required=True, choices=sorted(VERSIONS))
    parser.add_argument('--output', type=Path, required=True, help='new directory for the archive and pin')
    source = parser.add_mutually_exclusive_group()
    source.add_argument('--from-archive', type=Path, help='earlier pinned package whose binary is repackaged')
    source.add_argument('--binary', type=Path, help='existing runner build to package')
    parser.add_argument('--toolchain', default='', help='rustup toolchain for source builds (e.g. 1.98.1)')
    parser.add_argument('--target-dir', type=Path, default=ROOT / 'build/runner-release-target')
    parser.add_argument('--offline', action='store_true', help='use only cached crates (cargo --offline)')
    args = parser.parse_args()
    key = (platform.system(), platform.machine())
    if key not in PLATFORMS:
        parser.error(f'unsupported build host {key}')
    plat, rust_target = PLATFORMS[key]
    spec = VERSIONS[args.version]
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=False)
    files = {}
    if args.from_archive:
        if plat != 'darwin-arm64':
            parser.error('--from-archive repackages the historical macOS arm64 packages only')
        binary_bytes, original, pin = historical_binary(args.from_archive.resolve(), spec['historical_pin'])
        files['original/provenance.json'] = encoded(original)
        build_info = {'kind': 'repackaged', 'from_archive_sha256': pin['archive_sha256'],
                      'from_archive_pin': spec['historical_pin'],
                      'note': 'Runner binary unchanged from the earlier pinned package; its original build '
                              'provenance is original/provenance.json'}
        binary = output / '.runner'
        binary.write_bytes(binary_bytes)
        binary.chmod(0o755)
        if args.version == '0.6.0':  # Resolves the Cargo patch path for the license inventory.
            subprocess.run([sys.executable, str(ROOT / 'scripts/build_modern_runner.py'), '--prepare-only']
                           + (['--offline'] if args.offline else []), cwd=ROOT, check=True)
    elif args.binary:
        binary = args.binary.resolve()
        build_info = {'kind': 'prebuilt', 'path_name': binary.name,
                      'note': 'Compiler of a supplied binary is not observable here; compare binary_sha256 with qualification evidence'}
        if args.version == '0.6.0':
            subprocess.run([sys.executable, str(ROOT / 'scripts/build_modern_runner.py'), '--prepare-only']
                           + (['--offline'] if args.offline else []), cwd=ROOT, check=True)
    else:
        binary = build(spec, args.version, args.toolchain, args.target_dir.resolve(), args.offline)
        build_info = {'kind': 'built', 'rustc': command('rustc', *([f'+{args.toolchain}'] if args.toolchain else []), '--version'),
                      'cargo': command(*cargo(args.toolchain), '--version'), 'checkout': str(ROOT),
                      'cargo_home': os.environ.get('CARGO_HOME', str(Path.home() / '.cargo')),
                      'container_image': os.environ.get('GOSVM_RUNNER_BUILD_IMAGE', '')}
    identity = command(binary, '--version')
    if identity != spec['identity']:
        sys.exit(f'unexpected runner identity {identity!r}')
    files['bin/gosvm-svm-runner'] = binary.read_bytes()
    details = host_details(binary, plat)
    if args.from_archive:
        binary.unlink()
    for name in ('LICENSE', 'NOTICE', 'THIRD_PARTY_NOTICES'):
        files[name] = (ROOT / name).read_bytes()
    files['licenses/Apache-2.0.txt'] = (ROOT / 'LICENSE').read_bytes()
    files['Cargo.lock'] = (ROOT / spec['crate'] / 'Cargo.lock').read_bytes()
    for member, path in spec['litesvm'].items():
        files[member] = (ROOT / path).read_bytes()
    files['README'] = README.format(version=args.version, platform=plat, identity=spec['identity']).encode()
    dependencies = license_inventory(spec, rust_target, args.toolchain, args.offline, files)
    sources = [ROOT / 'scripts/package_runner_release.py', ROOT / 'LICENSE', ROOT / 'NOTICE', ROOT / 'THIRD_PARTY_NOTICES']
    sources += [ROOT / p for p in spec['litesvm'].values()]
    sources += [p for p in (ROOT / spec['crate']).rglob('*') if p.is_file() and p.suffix != '.md']
    if args.version == '0.5.0':
        sources += [p for p in (ROOT / 'third_party/litesvm-0.8.2').rglob('*') if p.is_file() and p.suffix != '.md']
    files['provenance.json'] = encoded({
        'schema': 2, 'version': args.version, 'platform': plat, 'rust_target': rust_target, 'build': build_info,
        'source_sha256': {str(p.relative_to(ROOT)): sha(p.read_bytes()) for p in sorted(set(sources))},
        'binary_sha256': sha(files['bin/gosvm-svm-runner']), **details,
        'scope': 'Experimental release package; the crate license inventory is not a legal review; '
                 'no notarization or publisher signature'})
    files['manifest.json'] = encoded({'schema': 1, 'version': args.version, 'identity': spec['identity'],
                                      'platform': plat, 'rust_target': rust_target})
    archive = output / f'gosvm-svm-runner-{args.version}-{plat}.tar.gz'
    write_archive(archive, files)
    data = archive.read_bytes()
    pin = {'schema': 1, 'version': args.version, 'identity': spec['identity'], 'platform': plat,
           'archive_name': archive.name, 'archive_sha256': sha(data), 'archive_bytes': len(data),
           'files': {name: {'sha256': sha(content), 'bytes': len(content)} for name, content in sorted(files.items())}}
    total = sum(len(c) for c in files.values())
    if any(len(c) > 32 << 20 or not c for c in files.values()) or total > 64 << 20:
        sys.exit('package exceeds the installer limits (32 MiB per member, 64 MiB total, no empty members)')
    (output / 'pin.json').write_bytes(encoded(pin))
    (output / 'dependencies.json').write_bytes(files['dependencies.json'])
    (output / 'provenance.json').write_bytes(files['provenance.json'])
    print(json.dumps({'archive': str(archive), 'bytes': len(data), 'sha256': pin['archive_sha256'],
                      'binary_sha256': sha(files['bin/gosvm-svm-runner']), 'packages': len(dependencies),
                      'packages_without_license_files': sum(not p['license_members'] for p in dependencies)}, indent=2))


if __name__ == '__main__':
    main()
