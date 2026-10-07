#!/usr/bin/env python3
"""Rebuild the preserved compact Whirlpools handler with the canonical SDK.

Reads the sibling benchmark checkout; writes only new goSVM-owned directories.
The benchmark's Rust comparisons remain its preserved independent evidence.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import time

ROOT = Path(__file__).resolve().parents[1]
BENCH = ROOT.parent / 'goSVM-benchmarks'


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', required=True, type=Path)
    args = parser.parse_args()
    out = args.output.resolve()
    stage = ROOT / 'build/compact-signer-canonical' / out.name
    assert out.is_relative_to(ROOT / 'results') and not out.exists()
    out.mkdir(parents=True)
    stage.mkdir(parents=True, exist_ok=False)
    summary = dict(passed=False, checks=[], scope='Canonical SDK rebuild of the preserved fixed-fee, event-omitting compact handler')
    inputs = [ROOT / 'go.mod', Path(__file__), ROOT / 'scripts/build-cli.sh',
              ROOT / 'internal/testvm/profiles/validator-3.0.15.json',
              BENCH / 'scripts/run_budget_suite.py']
    if (ROOT / 'go.sum').exists():
        inputs.append(ROOT / 'go.sum')
    for folder in ('cmd/gosvm', 'internal/compiler', 'internal/project', 'sdk', 'solana'):
        inputs += [p for p in (ROOT / folder).rglob('*') if p.is_file()]
    package = BENCH / 'programs/whirlpool/go-handler-compact-v1'
    shared = BENCH / 'programs/whirlpool/go'
    for folder in (package, shared):
        inputs += [p for p in folder.rglob('*') if p.is_file() and 'build' not in p.relative_to(folder).parts]
    summary['source_sha256'] = {str(p): digest(p) for p in inputs}
    env = dict(os.environ, GOWORK='off', GOTOOLCHAIN='local', PYTHONDONTWRITEBYTECODE='1', GOCACHE=str(ROOT / 'build/lifecycle-go-cache'))

    def save():
        (out / 'summary.json').write_text(json.dumps(summary, indent=2) + '\n')

    def run(name, command, cwd=ROOT):
        started = time.perf_counter()
        with (out / (name + '.log')).open('wb') as log:
            result = subprocess.run([str(x) for x in command], cwd=cwd, env=env, stdout=log, stderr=subprocess.STDOUT)
        summary['checks'].append(dict(name=name, command=[str(x) for x in command], cwd=str(cwd), exit_code=result.returncode, seconds=time.perf_counter() - started))
        save()
        assert result.returncode == 0, name + ': see log'
        print(name + ': PASS', flush=True)

    try:
        cli = stage / 'gosvm'
        run('cli-build', ['bash', ROOT / 'scripts/build-cli.sh', cli])
        run('sdk-scaffold', [cli, 'new', '-schema', '2', '-sdk', '2', stage / 'scaffold'])
        for source, destination in ((package, stage / 'handler'), (shared, stage / 'go')):
            shutil.copytree(source, destination, ignore=shutil.ignore_patterns('build', '.gosvm'))
            shutil.copytree(stage / 'scaffold/.gosvm', destination / '.gosvm')
        # The original sdk.lock remains an archived input, not a claim about this fresh pin.
        (stage / 'handler/sdk.lock.json').unlink()
        run('native-handler', ['go', 'test', '-ldflags=-linkmode=external', './...', '-count=1'], stage / 'handler')
        elf = out / 'handler.so'
        run('sbf-build', [cli, '-no-cache', '-llvm', Path.home() / '.cache/solana/v1.51/platform-tools/llvm', '-o', elf, stage / 'handler'])
        old = BENCH / 'results/whirlpool/2026-10-06-compact-signer-v1-verified/compact-0.json'
        expected = json.loads(old.read_text())
        assert expected['passed'] and digest(elf) == expected['elf_sha256']
        summary['elf_bytes'], summary['elf_sha256'] = elf.stat().st_size, digest(elf)
        summary['reference_report_sha256'] = digest(old)
        verified = BENCH / 'build/verification/2026-10-06-fee-growth-handler'
        fixture = out / 'handler.json'
        shutil.copy2(verified / 'handler.json', fixture)
        runner = next((verified / 'cache').rglob('gosvm-svm-runner'))
        for i in range(3):
            report = out / ('canonical-' + str(i) + '.json')
            run('sbf-' + str(i), ['python3', BENCH / 'scripts/run_budget_suite.py', '--runner', runner, '--maker', verified / 'transaction-maker', '--elf', elf, '--features', ROOT / 'internal/testvm/profiles/validator-3.0.15.json', '--fixtures', fixture, '--report', report])
            actual = json.loads(report.read_text())
            assert actual['passed'] and actual['cases'] == expected['cases']
            for key in ('elf_sha256', 'fixtures_sha256', 'runner_sha256',
                        'transaction_maker_sha256', 'features', 'runtime_version',
                        'compute_unit_limit'):
                assert actual[key] == expected[key], 'reference pin mismatch: ' + key
        assert all(digest(Path(p)) == h for p, h in summary['source_sha256'].items()), 'source drift'
        summary.update(passed=True, scenarios=len(expected['cases']), repetitions=3, exact_reference_cases=True, cli_sha256=digest(cli))
        print('PASS: canonical ELF and all case errors/CU/logs reproduce the preserved benchmark.', flush=True)
    finally:
        save()


if __name__ == '__main__':
    main()
