#!/usr/bin/env python3
"""Qualify Go+Bend and Go+Bend+Rust linked images on every network target.

Creates fresh projects from tests/bend-library and tests/go-bend-rust, binds
the pinned Bend producer manifests (one per target) and the in-repo Rust quote
crate through gosvm.json, then runs native tests and `gosvm test --svm`
(each target with its managed runner) for several repetitions.
"""
import argparse, hashlib, json, pathlib, re, shutil, subprocess, sys

ROOT = pathlib.Path(__file__).resolve().parent.parent

def sha(p): return hashlib.sha256(pathlib.Path(p).read_bytes()).hexdigest()

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--gosvm', required=True, type=pathlib.Path)
    ap.add_argument('--output', required=True, type=pathlib.Path)
    ap.add_argument('--bend', required=True, nargs='+', type=pathlib.Path, help='producer manifests, one per target')
    ap.add_argument('--repetitions', type=int, default=3)
    a = ap.parse_args()
    if a.output.exists():
        sys.exit(f'{a.output} exists; use a new results directory')
    a.output.mkdir(parents=True)
    work = ROOT / 'build' / 'go-bend-rust-qualification' / a.output.name
    if work.exists():
        shutil.rmtree(work)
    work.mkdir(parents=True)
    gosvm = str(a.gosvm.resolve())
    def run(args, cwd, log):
        p = subprocess.run([gosvm] + args, cwd=cwd, capture_output=True, text=True)
        (a.output / log).write_text(p.stdout + p.stderr)
        if p.returncode:
            sys.exit(f'gosvm {" ".join(args)} failed; see {a.output / log}')
        return p.stdout
    summary = {'schema': 1, 'gosvm_sha256': sha(a.gosvm), 'repetitions': a.repetitions, 'network_validation': False,
               'bend_manifests': {}, 'fixtures': {}}
    for m in a.bend:
        summary['bend_manifests'][json.loads(m.read_text())['targetProfile']] = {'path': str(m.resolve()), 'sha256': sha(m)}
    fixtures = {
        'go-bend': {'module': 'example.com/bend-mixed', 'src': ROOT / 'tests/bend-library', 'rust': False,
                    'rewrite': ('example.com/bend-mixed/bend', 'example.com/bend-mixed/libs/bend')},
        'go-bend-rust': {'module': 'example.com/trio', 'src': ROOT / 'tests/go-bend-rust', 'rust': True, 'rewrite': None},
    }
    for name, f in fixtures.items():
        project = work / name
        run(['new', '-module', f['module'], str(project)], work, f'{name}-new.log')
        run(['library', 'add', '-name', 'bend'] + [str(m.resolve()) for m in a.bend], project, f'{name}-bend.log')
        if f['rust']:
            shutil.copytree(ROOT / 'internal/library/testdata/rust/quote', project / 'rust/quote')
            run(['library', 'add', '-name', 'quote', '-function', 'Quote', '-rust', 'rust/quote'], project, f'{name}-rust.log')
        for src, dst in [('program.go.txt', 'program.go'), ('program_test.go.txt', 'program_test.go')]:
            text = (f['src'] / src).read_text()
            if f['rewrite']:
                text = text.replace(*f['rewrite'])
            (project / dst).write_text(text)
        shutil.copyfile(f['src'] / 'sbf.json', project / 'testdata/sbf.json')
        runs = []
        for i in range(a.repetitions):
            out = run(['test', '--svm', '--', '-ldflags=-linkmode=external', '-count=1'], project, f'{name}-{i}.log')
            for built in re.findall(r'^Built (\S+) \(', out, re.M):
                built = pathlib.Path(built)
                report = json.loads((built.parent / 'svm-results.json').read_text())
                runs.append({'run': i, 'target_profile': report['target_profile'], 'runtime': report['runtime_version'],
                             'runner_sha256': report['runner_sha256'], 'elf_sha256': sha(built), 'elf_bytes': built.stat().st_size,
                             'cases': [{'name': c['name'], 'error': c['error'], 'cu': c['cu']} for c in report['cases']]})
        summary['fixtures'][name] = {'fixture_sha256': sha(f['src'] / 'sbf.json'), 'runs': runs,
                                     'libraries': json.loads((project / 'gosvm.json').read_text())['libraries']}
    # gosvm exits nonzero on any case mismatch; also require every target in every run.
    targets = set(summary['bend_manifests'])
    for name, f in summary['fixtures'].items():
        for i in range(a.repetitions):
            seen = {r['target_profile'] for r in f['runs'] if r['run'] == i and r['cases']}
            if seen != targets:
                sys.exit(f'{name} run {i} covered {sorted(seen)}, expected {sorted(targets)}')
    summary['passed'] = True
    (a.output / 'metrics.json').write_text(json.dumps(summary, indent=2) + '\n')
    print('Qualification passed:', a.output)

if __name__ == '__main__':
    main()
