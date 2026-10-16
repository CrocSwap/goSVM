#!/usr/bin/env python3
"""Reconstruct the pinned modern runtime patch without tracking crate binaries."""
import argparse, hashlib, json, os, pathlib, subprocess, tarfile, tempfile, urllib.request
ROOT = pathlib.Path(__file__).resolve().parents[1]
ARCHIVE_SHA = '55763dae5b5f992c34eb39c49333f90dd6d5b851f878429ed2b08667b329baee'
SOURCE_SHA = 'edf9e06f7fca37b50b3bbd0d7ee22f1463db65a9deb3dd81628b3c0c018ccc4b'
BEFORE = '''                if let Err(err) = self.check_accounts_rent(tx, &context, &rent) {
                    tx_result = Err(err);
                };'''
AFTER = '''                // Preserve an existing instruction error before rent validation.
                if tx_result.is_ok() {
                    if let Err(err) = self.check_accounts_rent(tx, &context, &rent) {
                        tx_result = Err(err);
                    }
                };'''
def sha(data): return hashlib.sha256(data).hexdigest()
def main():
    ap=argparse.ArgumentParser(description=__doc__)
    ap.add_argument('--archive',type=pathlib.Path)
    ap.add_argument('--prepare-only',action='store_true')
    ap.add_argument('--offline',action='store_true')
    args=ap.parse_args()
    vendor=ROOT/'build/modern-runtime-vendor/litesvm-0.13.0'
    cargo_home=pathlib.Path(os.environ.get('CARGO_HOME') or pathlib.Path.home()/'.cargo')
    archives=list((cargo_home/'registry/cache').glob('*/litesvm-0.13.0.crate'))
    archive=args.archive or (archives[0] if archives else None)
    if archive: data=archive.read_bytes()
    elif args.offline: raise SystemExit('Pinned LiteSVM archive unavailable; supply --archive')
    else:
        with urllib.request.urlopen('https://static.crates.io/crates/litesvm/litesvm-0.13.0.crate') as response: data=response.read()
    if sha(data)!=ARCHIVE_SHA: raise SystemExit('LiteSVM archive SHA-256 mismatch')
    # Work in a private stage; never patch the registry or historical runner.
    vendor.parent.mkdir(parents=True,exist_ok=True)
    with tempfile.TemporaryDirectory(dir=vendor.parent) as tmp:
        tmp=pathlib.Path(tmp); srcarchive=tmp/'source.crate';srcarchive.write_bytes(data)
        with tarfile.open(srcarchive,'r:gz') as tf:
            for member in tf.getmembers():
                p=pathlib.PurePosixPath(member.name)
                if p.parts[0]!='litesvm-0.13.0' or '..' in p.parts or not (member.isfile() or member.isdir()): raise SystemExit('Unsafe crate archive')
            tf.extractall(tmp)
        stage=tmp/'litesvm-0.13.0';lib=stage/'src/lib.rs';original=lib.read_bytes()
        if sha(original)!=SOURCE_SHA or original.decode().count(BEFORE)!=1: raise SystemExit('Unexpected rent-check source')
        lib.write_text(original.decode().replace(BEFORE,AFTER))
        # Refuse changed prepared sources instead of silently overwriting them.
        if vendor.exists():
            expected={p.relative_to(stage):sha(p.read_bytes()) for p in stage.rglob('*') if p.is_file()}
            actual={p.relative_to(vendor):sha(p.read_bytes()) for p in vendor.rglob('*') if p.is_file()}
            if expected!=actual: raise SystemExit('Prepared modern runtime changed; remove its generated directory and rebuild')
        else: stage.rename(vendor)
    print('Prepared LiteSVM 0.13.0 with rent-error precedence patch:',vendor)
    if not args.prepare_only:
        command=['cargo','build','--release','--locked','--manifest-path',str(ROOT/'benchmarks/svm-runner-modern/Cargo.toml')]
        if args.offline: command.append('--offline')
        env=dict(os.environ);env.setdefault('CARGO_TARGET_DIR',str(ROOT/'build/modern-runtime-target'))
        subprocess.run(command,env=env,check=True)
if __name__=='__main__': main()
