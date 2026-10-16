#!/usr/bin/env python3
"""Build and package the gosvm CLI for one platform as a release archive.

Never publishes. Writes <output>/gosvm-<version>-<platform>.tar.gz containing
gosvm-<version>-<platform>/{gosvm, LICENSE, NOTICE, THIRD_PARTY_NOTICES, README}.
Platforms: darwin-arm64 (build on macOS: external linking, ad-hoc signed),
linux-amd64 and linux-arm64 (static, CGO_ENABLED=0, buildable on any host).
`--checksums DIR` instead writes DIR/SHA256SUMS for every release file in DIR.
"""
import argparse
import gzip
import hashlib
import io
import os
from pathlib import Path
import re
import subprocess
import sys
import tarfile
import tempfile

ROOT = Path(__file__).resolve().parents[1]
PLATFORMS = {'darwin-arm64': ('darwin', 'arm64'), 'linux-amd64': ('linux', 'amd64'), 'linux-arm64': ('linux', 'arm64')}
README = """gosvm {version} ({platform})

Write Solana and Fogo programs in Go. Experimental; see
https://github.com/CrocSwap/goSVM for documentation.

Install: put `gosvm` on PATH, then run
  gosvm toolchain install   # official platform-tools backend + pinned SVM runners
  gosvm doctor
Go 1.22 or newer is needed for projects (`gosvm test` runs `go test`).

gosvm downloads Anza's official platform-tools archives and goSVM's runner
release archives only during `gosvm toolchain install` / `gosvm runner install`,
and verifies them against SHA-256 pins built into this binary. Licensed under
the Apache License 2.0 (LICENSE, NOTICE); third-party notices in
THIRD_PARTY_NOTICES. Provided "AS IS", without warranty (LICENSE, section 7).
"""


def sha(data):
    return hashlib.sha256(data).hexdigest()


def checksums(directory):
    names = sorted(p.name for p in directory.iterdir()
                   if p.is_file() and p.name != 'SHA256SUMS' and not p.name.startswith('.'))
    lines = [f'{sha((directory / n).read_bytes())}  {n}' for n in names]
    (directory / 'SHA256SUMS').write_text('\n'.join(lines) + '\n')
    print((directory / 'SHA256SUMS').read_text(), end='')


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--version', help='release version, e.g. v0.1.0-alpha.1')
    parser.add_argument('--platform', choices=sorted(PLATFORMS))
    parser.add_argument('--output', type=Path)
    parser.add_argument('--checksums', type=Path, help='write SHA256SUMS for the files in this directory')
    args = parser.parse_args()
    if args.checksums:
        return checksums(args.checksums)
    if not (args.version and args.platform and args.output):
        parser.error('--version, --platform and --output are required')
    if not re.fullmatch(r'v\d+\.\d+\.\d+(-[0-9A-Za-z.]+)?', args.version):
        parser.error('version must look like v0.1.0 or v0.1.0-alpha.1')
    goos, goarch = PLATFORMS[args.platform]
    args.output.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory() as tmp:
        binary = Path(tmp) / 'gosvm'
        env = dict(os.environ, GOOS=goos, GOARCH=goarch, GOSVM_VERSION=args.version)
        subprocess.run(['bash', str(ROOT / 'scripts/build-cli.sh'), str(binary)], cwd=ROOT, env=env, check=True)
        prefix = f'gosvm-{args.version}-{args.platform}'
        files = {'gosvm': binary.read_bytes(),
                 'README': README.format(version=args.version, platform=args.platform).encode()}
        for name in ('LICENSE', 'NOTICE', 'THIRD_PARTY_NOTICES'):
            files[name] = (ROOT / name).read_bytes()
    archive = args.output / f'{prefix}.tar.gz'
    with archive.open('wb') as raw:
        with gzip.GzipFile(filename='', mode='wb', fileobj=raw, mtime=0, compresslevel=9) as compressed:
            with tarfile.open(fileobj=compressed, mode='w', format=tarfile.USTAR_FORMAT) as tar:
                for name, data in sorted(files.items()):
                    info = tarfile.TarInfo(f'{prefix}/{name}')
                    info.size = len(data)
                    info.mode = 0o755 if name == 'gosvm' else 0o644
                    tar.addfile(info, io.BytesIO(data))
    print(f'{archive} {archive.stat().st_size} bytes sha256 {sha(archive.read_bytes())} (gosvm {sha(files["gosvm"])})')


if __name__ == '__main__':
    sys.exit(main())
