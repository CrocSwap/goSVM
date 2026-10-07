#!/usr/bin/env python3
"""Remove build caches; --deep archives snapshots and clears all of build/.

Without --apply, only print the candidate inventory. Basanos, the sibling
benchmark, external tools and historical results are untouched. Default cleanup
also retains linked artifacts and current caches; --deep archives non-cache
snapshots before deleting build/, including those retained caches.
"""
import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tarfile

ROOT = Path(__file__).resolve().parents[1]
BUILD = ROOT / 'build'


def snapshot_files():
    """Keep scratch sources/pins, excluding reconstructible tool/build caches."""
    for here, dirs, files in os.walk(BUILD, followlinks=False):
        retained = []
        for name in sorted(dirs):
            p = Path(here) / name
            if p.is_symlink():
                yield p
            elif name not in ('cache', 'go-cache', 'go-harness-cache',
                               'lifecycle-go-cache', 'target', 'toolchains', '.git') and not name.endswith('-target'):
                retained.append(name)
        dirs[:] = retained
        for name in sorted(files):
            yield Path(here) / name


def archive_snapshots(destination):
    """Deduplicate identical files and verify the archive before removing build."""
    entries = []
    seen = {}
    if destination.exists():
        # Resume only if the complete archive matches every current snapshot.
        # Never overwrite an archive from an earlier/interrupted attempt.
        for p in snapshot_files():
            name = str(p.relative_to(ROOT))
            if p.is_symlink():
                entries.append(dict(path=name, symlink=str(p.readlink())))
            else:
                st = p.stat()
                entries.append(dict(path=name, sha256=sha(p), bytes=st.st_size, mode=st.st_mode & 0o7777))
    else:
        with tarfile.open(destination, 'w:gz', compresslevel=1) as archive:
            for p in snapshot_files():
                name = str(p.relative_to(ROOT))
                info = archive.gettarinfo(str(p), arcname=name)
                info.mode &= 0o7777
                if p.is_symlink():
                    entries.append(dict(path=name, symlink=info.linkname))
                    archive.addfile(info)
                    continue
                info.type = tarfile.REGTYPE
                info.linkname = ''
                info.size = p.stat().st_size
                digest = sha(p)
                entries.append(dict(path=name, sha256=digest, bytes=info.size, mode=info.mode))
                key = (digest, info.mode)
                if key in seen:
                    info.type = tarfile.LNKTYPE
                    info.linkname = seen[key]
                    info.size = 0
                    archive.addfile(info)
                else:
                    seen[key] = name
                    with p.open('rb') as data:
                        archive.addfile(info, data)
    expected = {row['path']: row for row in entries}
    # Read every unique payload back; duplicated hardlinks point to verified data.
    verified = {}
    with tarfile.open(destination, 'r:gz') as archive:
        for member in archive:
            row = expected.pop(member.name)
            if member.issym():
                assert member.linkname == row['symlink']
            elif member.islnk():
                assert verified[member.linkname] == (row['sha256'], row['bytes'], row['mode'])
                verified[member.name] = verified[member.linkname]
            else:
                data = archive.extractfile(member)
                digest = hashlib.sha256()
                size = 0
                for block in iter(lambda: data.read(1024 * 1024), b''):
                    digest.update(block)
                    size += len(block)
                assert (digest.hexdigest(), size, member.mode) == (row['sha256'], row['bytes'], row['mode'])
                verified[member.name] = (digest.hexdigest(), size, member.mode)
    assert not expected, 'archive is missing snapshots'
    return dict(path=str(destination.relative_to(ROOT)), sha256=sha(destination),
                bytes=destination.stat().st_size, verified=True, files=entries)


def allocated(path):
    if path.is_file():
        return path.stat().st_blocks * 512
    total = 0
    for here, dirs, files in os.walk(path, followlinks=False):
        dirs[:] = [d for d in dirs if not (Path(here) / d).is_symlink()]
        for name in files:
            p = Path(here) / name
            if not p.is_symlink():
                total += p.stat().st_blocks * 512
    return total


def candidates():
    assert not BUILD.is_symlink() and BUILD.resolve().is_relative_to(ROOT.resolve())
    if not BUILD.exists():
        return []
    targets = [BUILD / 'protocol/host-target', BUILD / 'protocol/sbf-target']
    targets += [p for p in BUILD.iterdir() if p.is_dir() and p.name.endswith('-target')]
    anchor = BUILD / 'anchor'
    if anchor.exists():
        targets += [p for p in anchor.iterdir() if p.is_dir() and p.name.endswith('-target')]
    targets += [BUILD / 'svm-experiments/2026-10-05-litesvm/runner-target']
    found = []
    for target in targets:
        if not target.exists() or target.is_symlink():
            continue
        for here, dirs, files in os.walk(target, followlinks=False):
            dirs[:] = [d for d in dirs if not (Path(here) / d).is_symlink()]
            folder = Path(here)
            if folder.name in ('incremental', '.fingerprint', 'build'):
                found.append(folder)
                dirs[:] = []
            elif folder.name == 'deps':
                # Keep executables, .so/.dylib and other linked outputs.
                found += [folder / name for name in files
                          if Path(name).suffix in ('.rlib', '.rmeta', '.o', '.d')
                          and not (folder / name).is_symlink()]
    acceptance = BUILD / 'milestone1-acceptance'
    if acceptance.exists():
        for run in acceptance.iterdir():
            if not run.is_dir() or run.is_symlink() or run.name == '2026-10-05-m1-acceptance-final':
                continue
            for relative in ('go-cache', 'cache/toolchains'):
                p = run / relative
                if p.exists() and not p.is_symlink():
                    found.append(p)
    found = sorted(set(found))
    assert all(p.resolve().is_relative_to(BUILD.resolve()) and not p.is_symlink() for p in found)
    return found


def sha(p):
    return hashlib.sha256(p.read_bytes()).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--apply', action='store_true', help='Delete the listed regenerable caches')
    parser.add_argument('--deep', action='store_true', help='Archive non-cache snapshots, then remove all of build/')
    parser.add_argument('--report', type=Path, help='Write a cleanup inventory/integrity report')
    args = parser.parse_args()
    report_path = args.report.resolve() if args.report else None
    if args.deep and args.apply and report_path is None:
        stamp = datetime.now(timezone.utc).strftime('%Y-%m-%dT%H%M%S.%fZ')
        report_path = ROOT / 'results/maintenance' / (stamp + '-deep-clean') / 'cleanup.json'
    if report_path:
        assert report_path.is_relative_to(ROOT / 'results') and not report_path.exists()
    if args.deep:
        assert not BUILD.is_symlink() and BUILD.resolve().is_relative_to(ROOT.resolve())
        paths = [BUILD] if BUILD.exists() else []
    else:
        paths = candidates()
    rows = [dict(path=str(p.relative_to(ROOT)), directory=p.is_dir(), allocated_bytes=allocated(p)) for p in paths]
    total = sum(r['allocated_bytes'] for r in rows)
    print(f'{len(rows)} cache entries; approximately {total / 2**30:.2f} GiB of allocated files', flush=True)
    grouped = {}
    for row in rows:
        parts = Path(row['path']).parts
        group = '/'.join(parts[:3]) if len(parts) > 1 and parts[1] in ('protocol', 'anchor') else '/'.join(parts[:2])
        grouped[group] = grouped.get(group, 0) + row['allocated_bytes']
    for group, size in sorted(grouped.items(), key=lambda item: -item[1]):
        print(f'  {size / 2**30:.2f} GiB  {group}', flush=True)
    report = dict(applied=False, candidate_allocated_bytes=total, candidates=rows, retained='results, linked binaries/libraries, source/SDK snapshots, current Go cache, latest M1 acceptance cache, external toolchains and sibling projects')
    if args.deep:
        report['retained'] = 'all historical results and non-cache build snapshots in a verified compressed archive; external tools and sibling projects untouched'
    if args.apply:
        # Avoid clearing files beneath any currently open build path. Inventory
        # is local and lsof output is filtered before it is reported.
        lsof = subprocess.run(['/usr/sbin/lsof', '-nP', '-F', 'n'], capture_output=True, text=True, timeout=20)
        assert lsof.returncode == 0, 'cannot inventory active build files'
        opened = [line[1:] for line in lsof.stdout.splitlines() if line.startswith('n' + str(BUILD) + '/')]
        assert not opened, 'build files are currently open: ' + repr(opened)
        preserved = {str(p.relative_to(ROOT)): sha(p) for p in (ROOT / 'results').rglob('*') if p.is_file()}
        if args.deep and BUILD.exists():
            report_path.parent.mkdir(parents=True, exist_ok=True)
            print('Archiving and verifying non-cache source/artifact snapshots before deletion...', flush=True)
            report['snapshot_archive'] = archive_snapshots(report_path.parent / 'build-snapshots.tar.gz')
            # Persist the restoration inventory before the destructive step.
            report_path.write_text(json.dumps(report, indent=2) + '\n')
        for p in paths:
            assert p.resolve().is_relative_to(BUILD.resolve()) and not p.is_symlink()
            if p.is_dir():
                shutil.rmtree(p)
            else:
                p.unlink()
        assert all(sha(ROOT / name) == want for name, want in preserved.items()), 'result evidence changed'
        report.update(applied=True, historical_result_files=len(preserved), historical_results_unchanged=True, historical_result_sha256=preserved)
        print('Removed regenerable caches; all historical results retain their hashes.', flush=True)
        if args.deep:
            print('Cleanup report: ' + str(report_path.relative_to(ROOT)), flush=True)
    if report_path:
        report_path.parent.mkdir(parents=True, exist_ok=True)
        report_path.write_text(json.dumps(report, indent=2) + '\n')


if __name__ == '__main__':
    main()
