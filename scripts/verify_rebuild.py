#!/usr/bin/env python3
"""Fresh-rebuild reproducibility for the platform-tools v1.57 targets.

For each target (fogo-v0, solana-v3) and workload, copies the unchanged source
into two fresh temporary directories with different absolute paths, builds each
copy with -no-cache and its own empty GOCACHE/TMPDIR, and compares the ELF
SHA-256. Each ELF is also scanned for the temporary paths, the workspace path and
the host username. Exits non-zero on any mismatch or embedded host string.
No network access and no runner are needed.
"""
import argparse, getpass, hashlib, json, os, pathlib, platform, shutil, subprocess, sys, tempfile

ROOT = pathlib.Path(__file__).resolve().parents[1]
TARGETS = {'fogo-v0': 'fogo', 'solana-v3': 'solana'}


def sha(path): return hashlib.sha256(pathlib.Path(path).read_bytes()).hexdigest()


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument('--output', type=pathlib.Path, required=True)
    ap.add_argument('--cli', type=pathlib.Path, default=ROOT / 'build/gosvm')
    ap.add_argument('--reference', type=pathlib.Path,
                    default=ROOT / 'results/compiler/2026-10-07-target-profiles-final/metrics.json',
                    help='earlier metrics.json whose ELF hashes are compared (informational)')
    a = ap.parse_args(); a.output = a.output.resolve(); a.cli = a.cli.resolve()
    if a.output.exists():
        sys.exit(f'{a.output} exists; use a new results directory')
    a.output.mkdir(parents=True)
    base = {k: v for k, v in os.environ.items() if k not in ('GOCACHE', 'TMPDIR', 'SBF_LLVM')}
    base.update(GOWORK='off', GOTOOLCHAIN='local', GOPROXY='off', GOSUMDB='off')
    user = getpass.getuser()
    logindex = 0

    def run(args, cwd, env):
        nonlocal logindex
        logindex += 1
        log = a.output / f'{logindex:02d}.log'
        p = subprocess.run([str(a.cli), *map(str, args)], cwd=cwd, env=env, capture_output=True, text=True)
        log.write_text('$ gosvm ' + ' '.join(map(str, args)) + '\n' + p.stdout + p.stderr)
        if p.returncode:
            sys.exit(f'gosvm {" ".join(map(str, args))} failed; see {log}')
        return p.stdout + p.stderr

    workloads = {
        'full-swap': {'src': ROOT / 'examples/full-swap', 'kind': 'project'},
        'clock2': {'src': ROOT / 'examples/clock2', 'kind': 'low-level'},
    }
    summary = {'schema': 1, 'network_validation': False, 'host': platform.platform(), 'machine': platform.machine(),
               'cli': str(a.cli.relative_to(ROOT)) if a.cli.is_relative_to(ROOT) else str(a.cli),
               'cli_sha256': sha(a.cli), 'build_flags': ['-no-cache'], 'go_version': subprocess.run(
                   ['go', 'version'], env=base, capture_output=True, text=True).stdout.strip(),
               'workloads': {k: str(v['src'].relative_to(ROOT)) for k, v in workloads.items()},
               'skipped': {'go-bend-rust': 'needs pinned Bend producer manifests and the full Rust producer tree; '
                                           'not part of this offline rebuild check'},
               'results': []}
    passed = True
    for profile, network in TARGETS.items():
        for name, w in workloads.items():
            sides = []
            for side in ('a', 'bb-longer-path'):
                root = pathlib.Path(tempfile.mkdtemp(prefix=f'gosvm-rebuild-{side}-'))
                try:
                    work = root / 'work' / side / name
                    shutil.copytree(w['src'], work, ignore=shutil.ignore_patterns('build'))
                    env = dict(base, GOCACHE=str(root / 'gocache'), TMPDIR=str(root / 'tmp'))
                    os.makedirs(env['TMPDIR'])
                    if w['kind'] == 'project':
                        text = run(['build', '-no-cache', '-target', profile, '-dir', work], work, env)
                        elf = work / 'build' / network / 'program.so'
                    else:
                        elf = root / 'out' / 'program.so'
                        text = run(['-no-cache', '-target', profile, '-o', elf, work], root, env)
                    data = elf.read_bytes()
                    needles = {'workspace': str(work), 'tmp_root': str(root), 'tmpdir': env['TMPDIR'],
                               'repository': str(ROOT), 'username': user}
                    found = sorted(k for k, s in needles.items() if s.encode() in data)
                    sides.append({'side': side, 'path': str(work), 'elf_sha256': hashlib.sha256(data).hexdigest(),
                                  'size': len(data), 'embedded_host_strings': found})
                finally:
                    shutil.rmtree(root, ignore_errors=True)
            identical = len({s['elf_sha256'] for s in sides}) == 1
            clean = all(not s['embedded_host_strings'] for s in sides)
            passed &= identical and clean
            summary['results'].append({'target': profile, 'workload': name, 'kind': w['kind'],
                                       'identical': identical, 'no_embedded_host_strings': clean,
                                       'elf_sha256': sides[0]['elf_sha256'], 'sides': sides})
            print(f'{profile:10} {name:10} {"identical" if identical else "DIFFERENT"} '
                  f'{"clean" if clean else "EMBEDDED " + str([s["embedded_host_strings"] for s in sides])} '
                  f'{sides[0]["elf_sha256"]}')
    doctor = subprocess.run([str(a.cli), 'doctor'], env=base, capture_output=True, text=True)
    home = str(pathlib.Path.home())
    summary['backends'] = [line.replace(home, '~') for line in (doctor.stdout + doctor.stderr).splitlines()
                           if line.startswith('OK') and 'backend' in line]
    llvm = pathlib.Path.home() / '.cache/solana/v1.57/platform-tools/llvm/bin'
    summary['unmanaged_v1_57_llvm_sha256'] = {t: sha((llvm / t).resolve()) for t in ('clang', 'ld.lld') if (llvm / t).exists()}
    if a.reference:
        ref = json.loads(a.reference.read_text())
        if 'runs' in ref:  # Target-profile acceptance metrics.
            known = {(r['target'], r['workload']): r['elf_sha256'] for r in ref['runs']}
            names = {'full-swap': 'token-swap', 'clock2': 'clock'}
        else:  # An earlier run of this script.
            known = {(r['target'], r['workload']): r['elf_sha256'] for r in ref['results']}
            names = {'full-swap': 'full-swap', 'clock2': 'clock2'}
        summary['reference'] = str(a.reference.resolve().relative_to(ROOT))
        for r in summary['results']:
            r['matches_reference'] = known.get((r['target'], names[r['workload']])) == r['elf_sha256']
    summary['passed'] = passed
    for f in a.output.glob('*.log'):
        f.write_text(f.read_text().replace(home, '~'))
    (a.output / 'metrics.json').write_text(json.dumps(summary, indent=2) + '\n')
    if not passed:
        sys.exit(f'Rebuilds differ or embed host strings; see {a.output / "metrics.json"}')
    print('Fresh rebuilds identical:', a.output)


if __name__ == '__main__': main()
