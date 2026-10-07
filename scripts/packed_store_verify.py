#!/usr/bin/env python3
"""Verify canonical packed-store codegen against the frozen compact handler."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import statistics
import subprocess
import time

ROOT = Path(__file__).resolve().parents[1]
BENCH = ROOT.parent / 'goSVM-benchmarks'
BASE = ROOT / 'results/compiler/2026-10-06-compact-signer-canonical-final'
SOURCE = ROOT / 'build/compact-signer-canonical/2026-10-06-compact-signer-canonical-final'


def sha(p):
    return hashlib.sha256(p.read_bytes()).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', required=True, type=Path)
    args = parser.parse_args()
    out = args.output.resolve()
    stage = ROOT / 'build/packed-store' / out.name
    assert out.is_relative_to(ROOT / 'results') and not out.exists() and not stage.exists()
    out.mkdir(parents=True)
    stage.mkdir(parents=True)
    summary = dict(passed=False, checks=[], scope='Canonical SDK-2 packed-store optimization; frozen compact fixed-fee/event-omitting handler application')
    inputs = [ROOT / 'go.mod', ROOT / 'scripts/build-cli.sh', Path(__file__), BASE / 'handler.json', BASE / 'canonical-0.json', BENCH / 'scripts/run_budget_suite.py', BENCH / 'results/whirlpool/2026-10-06-compact-signer-v1-verified/rust-0.json']
    for folder in ('cmd', 'internal', 'sdk', 'solana'):
        inputs += [p for p in (ROOT / folder).rglob('*') if p.is_file()]
    for folder in (SOURCE / 'handler', SOURCE / 'go'):
        inputs += [p for p in folder.rglob('*') if p.is_file() and 'build' not in p.relative_to(folder).parts]
    summary['source_sha256'] = {str(p): sha(p) for p in inputs}
    env = dict(os.environ, GOWORK='off', GOTOOLCHAIN='local', GOPROXY='off', GOSUMDB='off', GOCACHE=str(ROOT / 'build/lifecycle-go-cache'), PYTHONDONTWRITEBYTECODE='1')

    def save():
        (out / 'summary.json').write_text(json.dumps(summary, indent=2) + '\n')

    def run(name, command, cwd=ROOT):
        start = time.perf_counter()
        with (out / (name + '.log')).open('wb') as log:
            p = subprocess.run([str(x) for x in command], cwd=cwd, env=env, stdout=log, stderr=subprocess.STDOUT)
        summary['checks'].append(dict(name=name, command=[str(x) for x in command], cwd=str(cwd), exit_code=p.returncode, seconds=time.perf_counter() - start))
        save()
        assert p.returncode == 0, name + ': see log'
        print(name + ': PASS', flush=True)

    try:
        cli, llvm = stage / 'gosvm', Path.home() / '.cache/solana/v1.51/platform-tools/llvm/bin'
        run('packed-store-tests', ['go', 'test', '-ldflags=-linkmode=external', './internal/compiler', '-run', 'TestPackedStore', '-count=1', '-v'])
        run('cli-build', ['bash', ROOT / 'scripts/build-cli.sh', cli])
        run('sdk-scaffold', [cli, 'new', '-schema', '2', '-sdk', '2', stage / 'scaffold'])
        for name in ('handler', 'go'):
            shutil.copytree(SOURCE / name, stage / name, ignore=shutil.ignore_patterns('build', '.gosvm'))
            shutil.copytree(stage / 'scaffold/.gosvm', stage / name / '.gosvm')
        for name in ('handler', 'go'):
            run('native-' + name, ['go', 'test', '-ldflags=-linkmode=external', './...', '-count=1'], stage / name)
        elf, c, obj = out / 'handler.so', out / 'handler.c', out / 'stack.o'
        run('sbf-build', [cli, '-no-cache', '-llvm', llvm.parent, '-o', elf, stage / 'handler'])
        controls = json.loads((ROOT / 'results/compiler/2026-10-06-packed-codegen-controls-final/summary.json').read_text())
        assert controls['passed'] and sha(elf) == controls['variants']['write-words']['elf_sha256'], 'canonical output differs from the isolated proven write variant'
        run('emit-c', [cli, '-emit-c', '-o', c, stage / 'handler'])
        run('stack-compile', [llvm / 'clang', '-target', 'sbf', '-mcpu=v3', '-O2', '-fno-builtin', '-fPIC', '-fno-stack-protector', '-std=c11', '-Werror', '-fstack-usage', '-c', c, '-o', obj])
        run('stack-link', [llvm / 'ld.lld', '-z', 'notext', '-shared', '--Bdynamic', '--strip-all', '--entry', 'entrypoint', '--script', ROOT / 'internal/compiler/sbf-v3.ld', '--no-undefined', '-o', out / 'stack.so', obj])
        assert sha(elf) == sha(out / 'stack.so')
        baseline = json.loads((BASE / 'canonical-0.json').read_text())
        rust = json.loads((BENCH / 'results/whirlpool/2026-10-06-compact-signer-v1-verified/rust-0.json').read_text())
        fixture = json.loads((BASE / 'handler.json').read_text())
        program_prefix = 'Program ' + fixture['program_id'] + ' consumed '
        verified = BENCH / 'build/verification/2026-10-06-fee-growth-handler'
        runner = next((verified / 'cache').rglob('gosvm-svm-runner'))
        reports = []
        def logs(xs):
            return [re.sub(r' of \d+ compute units$', ' of <remaining> compute units', x) for x in xs if not x.startswith(program_prefix)]
        for i in range(3):
            report = out / ('canonical-' + str(i) + '.json')
            run('sbf-' + str(i), ['python3', BENCH / 'scripts/run_budget_suite.py', '--runner', runner, '--maker', verified / 'transaction-maker', '--elf', elf, '--features', ROOT / 'internal/testvm/profiles/validator-3.0.15.json', '--fixtures', BASE / 'handler.json', '--report', report])
            actual = json.loads(report.read_text())
            assert actual['passed'] and len(actual['cases']) == len(baseline['cases'])
            for key in ('fixtures_sha256', 'runner_sha256', 'transaction_maker_sha256', 'features', 'runtime_version', 'compute_unit_limit'):
                assert actual[key] == baseline[key], key
            for a, b in zip(actual['cases'], baseline['cases']):
                assert a['name'] == b['name'] and len(a['steps']) == len(b['steps'])
                for s, t in zip(a['steps'], b['steps']):
                    assert s['error'] == t['error'] and s['committed'] == t['committed'] and logs(s['logs']) == logs(t['logs'])
            if reports:
                assert actual['cases'] == reports[0]['cases']
            reports.append(actual)
        rows = []
        for a, b, r in zip(reports[0]['cases'], baseline['cases'], rust['cases']):
            assert a['name'] == r['name']
            for s, t, u in zip(a['steps'], b['steps'], r['steps']):
                rows.append(dict(name=a['name'], success=s['error'] is None, baseline_cu=t['cu'], go_cu=s['cu'], rust_cu=u['cu'], saving=t['cu'] - s['cu'], paired_ratio=s['cu'] / u['cu']))
        successes = [r for r in rows if r['success']]
        failures = [r for r in rows if not r['success']]
        summary.update(elf_bytes=elf.stat().st_size, elf_sha256=sha(elf), cli_sha256=sha(cli), generated_c_sha256=sha(c), max_static_frame=max(int(x.split('\t')[1]) for x in (out / 'stack.su').read_text().splitlines()), scenarios=len(reports[0]['cases']), repetitions=3, median_paired_ratio=statistics.median(r['paired_ratio'] for r in successes), median_success_saving=statistics.median(r['saving'] for r in successes), median_success_percent=statistics.median(r['saving'] / r['baseline_cu'] * 100 for r in successes), success_improve=sum(r['saving'] > 0 for r in successes), success_regress=sum(r['saving'] < 0 for r in successes), failure_improve=sum(r['saving'] > 0 for r in failures), failure_regress=sum(r['saving'] < 0 for r in failures))
        (out / 'per-case-cu.json').write_text(json.dumps(rows, indent=2) + '\n')
        assert all(sha(Path(p)) == h for p, h in summary['source_sha256'].items()), 'source drift'
        summary['passed'] = True
        print('PASS: canonical packed stores match the isolated ELF and complete handler behavior.', flush=True)
    finally:
        save()


if __name__ == '__main__':
    main()
