#!/usr/bin/env python3
"""Check new-account lifecycle against a fresh local validator and the fast CLI."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import subprocess

ROOT = Path(__file__).resolve().parents[1]


def run(command, log, env=None, success=True):
    with log.open('w') as out:
        result = subprocess.run(command, cwd=ROOT, env=env, stdout=out,
                                stderr=subprocess.STDOUT, timeout=300)
    if success and result.returncode:
        raise RuntimeError(f'Command failed ({result.returncode}); see {log}')
    if not success and result.returncode == 0:
        raise AssertionError(f'Expected rejection; see {log}')


def load(path):
    return json.loads(path.read_text())


def save(path, value):
    path.write_text(json.dumps(value, indent=2) + '\n')


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def without_epochs(value):
    if isinstance(value, dict):
        return {k: without_epochs(v) for k, v in value.items() if k != 'rent_epoch'}
    if isinstance(value, list):
        return [without_epochs(v) for v in value]
    return value


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', required=True, type=Path)
    parser.add_argument('--samples', default=3, type=int)
    parser.add_argument('--validator-evidence', type=Path,
                        help='reuse an already captured validator oracle when local RPC binding is unavailable')
    parser.add_argument('--llvm', type=Path,
                        default=Path.home() / '.cache/solana/v1.51/platform-tools/llvm')
    args = parser.parse_args()
    if not 1 <= args.samples <= 20:
        parser.error('--samples requires 1..20')
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=False)
    stage = ROOT / 'build/svm-lifecycle' / output.name
    stage.mkdir(parents=True, exist_ok=False)
    target = ROOT / 'build/svm-fixtures-target'
    manifest = 'benchmarks/svm-runner/Cargo.toml'
    run(['cargo', 'test', '--release', '--locked', '--offline', '--manifest-path', manifest,
         '--target-dir', str(target)], output / 'rust-tests.log')
    run(['cargo', 'build', '--release', '--locked', '--offline', '--manifest-path', manifest,
         '--target-dir', str(target)], output / 'runner-build.log')
    runner = target / 'release/gosvm-svm-runner'
    cli, verifier = stage / 'gosvm', stage / 'verify'
    run(['bash', 'scripts/build-cli.sh', str(cli)], output / 'cli-build.log')
    flags = ['-ldflags=-linkmode=external'] if platform.system() == 'Darwin' else []
    run(['go', 'build', *flags, '-o', str(verifier), './cmd/verify'], output / 'verifier-build.log')
    if platform.system() == 'Darwin':
        run(['codesign', '--force', '--sign', '-', str(verifier)], output / 'verifier-sign.log')
    # Freshly compile the original manual token swap, without the matched Anchor
    # wire adapter. Do not reuse or overwrite historical benchmark build outputs.
    elf = stage / 'go-token.so'
    run([str(cli), '-arch', 'v3', '-llvm', str(args.llvm.resolve()), '-no-cache',
         '-o', str(elf), 'examples/tokenswap/swap.go'], output / 'program-build.log')
    oracle_env = dict(os.environ, GOSVM_VERIFY_BUILD_DIR=str(stage), GOSVM_TEST_RUNNER='')
    evidence = stage
    if args.validator_evidence:
        evidence = args.validator_evidence.resolve()
    else:
        run([str(verifier), 'lifecycle'], output / 'validator.log', oracle_env)
    for name in ('fixtures.json', 'validator-fixtures.json', 'validator-results.json',
                 'validator-features.json', 'spl-token.so', 'validator-detail.log'):
        shutil.copyfile(evidence / name, output / name)
    if args.validator_evidence:
        shutil.copyfile(evidence / 'validator.log', output / 'captured-validator.log')
    shutil.copyfile(elf, output / 'go-token.so')
    oracle = load(output / 'validator-results.json')
    assert oracle['passed'] and digest(elf) == oracle['elf_sha256']
    assert digest(output / 'spl-token.so') == oracle['token_sha256']
    suite = load(output / 'fixtures.json')
    validator_suite = load(output / 'validator-fixtures.json')
    assert without_epochs(suite) == without_epochs(validator_suite)
    # The exact known rent_epoch difference is intentional and not hidden by the
    # CLI: prove the validator expectations are rejected by the unchanged runner.
    env = dict(os.environ, GOSVM_TEST_FEATURES=str(output / 'validator-features.json'))
    report = output / 'runner-results.json'
    common = [str(cli), 'svm-test', '-elf', str(elf), '-runner', str(runner)]
    for sample in range(args.samples):
        run([*common, '-fixtures', str(output / 'fixtures.json'), '-report', str(report)],
            output / f'runner-{sample}.log', env)
        shutil.copyfile(report, output / f'runner-{sample}.json')
        data = load(report)
        assert data['elf_sha256'] == oracle['elf_sha256']
        assert data['program_sha256']['token'] == oracle['token_sha256']
        assert data['runner']['active_features'] == oracle['active_features']
        assert data['native_programs'] == suite['native_programs']
        assert data['cases'] == oracle['cases'], 'CU/error/log/checked submission differs'
    run([*common, '-fixtures', str(output / 'validator-fixtures.json'), '-report', str(report)],
        output / 'rent-epoch-rejection.log', env, success=False)
    assert not report.exists()
    assert 'rent_epoch=0' in (output / 'rent-epoch-rejection.log').read_text()
    # Corrupt a successful-close phase expectation to ensure zero-account bytes
    # are checked before submission, rather than treating simulation as absence.
    corrupt = load(output / 'fixtures.json')
    closed = [step for step in corrupt['cases'][0]['steps']
              if step['expect'].get('simulation_accounts')]
    assert len(closed) == 1
    closed[0]['expect']['simulation_accounts'][0]['state']['owner'] = suite['program_id']
    bad = output / 'bad-close-fixtures.json'
    save(bad, corrupt)
    run([*common, '-fixtures', str(bad), '-run', '^create-mint-', '-report', str(report)],
        output / 'close-state-rejection.log', env, success=False)
    assert not report.exists()
    assert 'simulated metadata' in (output / 'close-state-rejection.log').read_text()
    selected = output / 'selected-results.json'
    run([*common, '-fixtures', str(output / 'fixtures.json'), '-run', '^badrent$',
         '-report', str(selected)], output / 'selected.log', env)
    assert len(load(selected)['cases']) == 1
    # Restore the full current report after deliberate negative checks.
    run([*common, '-fixtures', str(output / 'fixtures.json'), '-report', str(report)],
        output / 'runner-restored.log', env)
    assert load(report)['cases'] == oracle['cases']
    # Retain both starter workflows and the old eight-account general corpus.
    run([str(cli), 'test', '--svm', '-dir', 'examples/typed-swap', '-svm-runner', str(runner)],
        output / 'starter.log', env)
    shutil.copyfile(ROOT / 'examples/typed-swap/build/svm-results.json', output / 'starter.json')
    assert len(load(output / 'starter.json')['cases']) == 14
    run([str(cli), 'test', '--svm', '-dir', 'examples/typed-swap', '-svm-runner', str(runner),
         '-svm-fixtures', str(ROOT / 'examples/typed-swap/testdata/svm.json')], output / 'project.log', env)
    shutil.copyfile(ROOT / 'examples/typed-swap/build/svm-results.json', output / 'project.json')
    assert len(load(output / 'project.json')['cases']) == 3
    old = ROOT / 'results/svm/2026-10-05-general-complete'
    run([str(cli), 'svm-test', '-elf', str(ROOT / 'build/anchor/go-token.so'),
         '-fixtures', str(old / 'token-fixtures.json'), '-runner', str(runner),
         '-report', str(output / 'general-regression.json')], output / 'general-regression.log', env)
    assert load(output / 'general-regression.json')['cases'] == load(old / 'general-0.json')['cases']
    sources = [ROOT / 'scripts/svm_lifecycle.py', ROOT / 'examples/tokenswap/swap.go']
    for directory in ('internal/sbftest', 'internal/testvm', 'cmd/gosvm', 'cmd/verify', 'benchmarks/svm-runner/src'):
        sources.extend(p for p in (ROOT / directory).rglob('*') if p.is_file())
    sources.extend(ROOT / 'benchmarks/svm-runner' / n for n in ('Cargo.toml', 'Cargo.lock'))
    vendor = ROOT / 'third_party/litesvm-0.8.2'
    provenance = load(vendor / 'GOSVM_PROVENANCE.json')
    changed = [name for name, sha in provenance['upstream_file_sha256'].items()
               if digest(vendor / name) != sha]
    assert changed == ['src/lib.rs'], f'Unexpected vendored changes: {changed}'
    sources.extend(p for p in vendor.rglob('*') if p.is_file())
    steps = sum(len(c['steps']) for c in suite['cases'])
    failures = sum(step['expect']['error'] is not None for c in suite['cases'] for step in c['steps'])
    save(output / 'summary.json', {
        'schema': 1, 'passed': True, 'samples': args.samples, 'machine': platform.platform(),
        'scenarios': len(suite['cases']), 'steps_per_run': steps, 'expected_failures_per_run': failures,
        'runtime_version': data['runtime_version'], 'validator_version': oracle['validator'],
        'validator_execution': 'reused captured oracle' if args.validator_evidence else 'fresh local validator',
        'validator_evidence': str(evidence.relative_to(ROOT)) if evidence.is_relative_to(ROOT) else str(evidence),
        'validator_report_sha256': digest(output / 'validator-results.json'),
        'known_metadata_difference': oracle['known_metadata_difference'],
        'runtime_patch': 'LiteSVM 0.8.2 post-execution rent checks preserve prior instruction errors',
        'fixtures_sha256': digest(output / 'fixtures.json'),
        'validator_fixtures_sha256': digest(output / 'validator-fixtures.json'),
        'source_sha256': {str(p.relative_to(ROOT)): digest(p) for p in sorted(set(sources))},
        'artifacts': {p.name: {'bytes': p.stat().st_size, 'sha256': digest(p)}
                      for p in (cli, verifier, runner, elf, output / 'spl-token.so')},
        'limitations': ['rent_epoch metadata differs explicitly; no claim of identical whole-account metadata',
                        'Go pool state remains genesis seeded; token mints/accounts are transaction created',
                        'System creation is top-level, not Go-authored System CPI or account resizing',
                        'macOS arm64 only; no workflow latency or distribution guarantee',
                        'Bounded starter and full token swap tested; dependency-heavy protocol not run']})
    print(f'PASS: {len(suite["cases"])} lifecycle scenarios, {steps} transactions/run, '
          f'{args.samples} repetitions; validator CU/error/state agreement with explicit rent_epoch difference. {output}')


if __name__ == '__main__':
    main()
