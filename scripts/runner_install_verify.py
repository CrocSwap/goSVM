#!/usr/bin/env python3
"""Validate pinned offline runner installation and managed compiled-SBF tests."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import time

ROOT = Path(__file__).resolve().parents[1]


def load(path):
    return json.loads(path.read_text())


def save(path, value):
    path.write_text(json.dumps(value, indent=2) + '\n')


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--archive', type=Path, required=True)
    parser.add_argument('--cli', type=Path, default=ROOT / 'build/gosvm')
    args = parser.parse_args()
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=False)
    stage = ROOT / 'build/runner-install' / output.name
    stage.mkdir(parents=True, exist_ok=False)
    cli, archive = args.cli.resolve(), args.archive.resolve()
    pin_path = ROOT / 'internal/runner/pins/darwin-arm64.json'
    pin = load(pin_path)
    assert digest(archive) == pin['archive_sha256']
    sources = [ROOT / 'scripts/package_runner.py', ROOT / 'scripts/runner_install_verify.py']
    for folder in ('cmd/gosvm', 'internal/runner', 'internal/testvm', 'internal/sbftest', 'internal/project'):
        sources.extend(p for p in (ROOT / folder).rglob('*') if p.is_file() and p.suffix != '.md')
    hashes = {str(p.relative_to(ROOT)): digest(p) for p in sorted(set(sources))}
    # No Cargo, rustc, validator or host Clang on PATH during installation/testing.
    tools = stage / 'path'
    tools.mkdir()
    (tools / 'go').symlink_to(shutil.which('go'))
    env = dict(os.environ, PATH=str(tools), GOSVM_CACHE=str(stage / 'cache'),
               GOCACHE=str(ROOT / 'build/lifecycle-go-cache'), GOWORK='off', GOPROXY='off', CGO_ENABLED='0')
    env.pop('GOSVM_TEST_RUNNER', None)
    env.pop('GOSVM_TEST_FEATURES', None)
    checks = []

    def run(label, command, *, expected=0, contains=None, selected_env=env):
        start = time.perf_counter()
        completed = subprocess.run([str(x) for x in command], env=selected_env, cwd=ROOT,
                                   stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=120)
        wall = time.perf_counter() - start
        (output / (label + '.log')).write_bytes(completed.stdout)
        assert completed.returncode == expected, (label, completed.stdout.decode())
        if contains:
            assert contains.encode() in completed.stdout, (label, completed.stdout.decode())
        checks.append({'check': label, 'command': [str(x) for x in command], 'exit_code': completed.returncode,
                       'wall_seconds': wall, 'expected': expected, 'log': label + '.log'})
        print(f'{label}: PASS ({wall:.3f}s)', flush=True)

    run('missing-download', [cli, 'runner', 'install'], expected=1, contains='no public runner download')
    assert not (stage / 'cache').exists()
    bad = stage / 'bad.tar.gz'
    raw = bytearray(archive.read_bytes())
    raw[len(raw) // 2] ^= 1
    bad.write_bytes(raw)
    run('bad-archive', [cli, 'runner', 'install', '-archive', bad], expected=1, contains='SHA-256 mismatch')
    managed = stage / 'cache/runners/0.5.0/darwin-arm64'
    assert not managed.exists() and not Path(str(managed) + '.lock').exists()
    assert not list(managed.parent.glob('.runner-install-*'))
    run('install', [cli, 'runner', 'install', '-archive', archive])
    run('status', [cli, 'runner', 'status'])
    binary = managed / 'bin/gosvm-svm-runner'
    assert digest(binary) == pin['files']['bin/gosvm-svm-runner']['sha256']
    assert load(managed / 'receipt.json') == pin
    stamp = binary.stat().st_mtime_ns
    run('idempotent', [cli, 'runner', 'install'], contains='already installed and verified')
    assert binary.stat().st_mtime_ns == stamp
    # Fresh scaffold exercises embedded templates and managed selection, without
    # any explicit runner path or environment override.
    project = stage / 'starter'
    run('scaffold', [cli, 'new', '-module', 'example.com/installed-runner', project])
    llvm = Path.home() / '.cache/solana/v1.51/platform-tools/llvm'
    command = [cli, 'test', '--svm', '-dir', project, '-llvm', llvm, '-timings']
    run('starter', command)
    run('doctor', [cli, 'doctor', '-dir', project, '-llvm', llvm], contains='OK optional SVM runner')
    starter = load(project / 'build/svm-results.json')
    assert len(starter['cases']) == 14 and starter['runner_sha256'] == digest(binary)
    assert starter['cases'] == load(ROOT / 'results/svm/2026-10-05-workflow-complete/bounded-restored-0-results.json')['cases']
    shutil.copyfile(project / 'build/svm-results.json', output / 'starter.json')
    for label, directory, elf, fixture, reference in (
        ('full-token', ROOT / 'results/svm/2026-10-05-general-complete', ROOT / 'build/anchor/go-token.so',
         'token-fixtures.json', 'general-0.json'),
        ('lifecycle', ROOT / 'results/svm/2026-10-05-lifecycle-verified',
         ROOT / 'results/svm/2026-10-05-lifecycle-verified/go-token.so', 'fixtures.json', 'runner-results.json')):
        report = output / (label + '.json')
        run(label, [cli, 'svm-test', '-elf', elf, '-fixtures', directory / fixture, '-report', report])
        value = load(report)
        expected = load(directory / reference)
        assert value['cases'] == expected['cases'] and value['elf_sha256'] == expected['elf_sha256']
        assert value['runner_sha256'] == digest(binary)
    # Corruption cannot be hidden by a receipt, a PATH fallback or a stale report.
    original = binary.read_bytes()
    binary.write_bytes(bytes([original[0] ^ 1]) + original[1:])
    run('damaged-status', [cli, 'runner', 'status'], expected=1, contains='SHA-256 mismatch')
    run('damaged-test', command, expected=1, contains='managed runner integrity check')
    assert not (project / 'build/svm-results.json').exists()
    receipt_path = managed / 'receipt.json'
    original_receipt = receipt_path.read_bytes()
    forged = load(receipt_path)
    forged['files']['bin/gosvm-svm-runner']['sha256'] = digest(binary)
    save(receipt_path, forged)
    run('forged-receipt', [cli, 'runner', 'status'], expected=1, contains="frontend's pinned package")
    binary.write_bytes(original)
    receipt_path.write_bytes(original_receipt)
    run('restored-status', [cli, 'runner', 'status'])
    run('restored-starter', command)
    assert load(project / 'build/svm-results.json')['cases'] == starter['cases']
    assert not Path(str(managed) + '.lock').exists()
    assert not list(managed.parent.glob('.runner-install-*'))
    assert hashes == {str(p.relative_to(ROOT)): digest(p) for p in sorted(set(sources))}
    shutil.copyfile(archive, output / archive.name)
    shutil.copyfile(pin_path, output / 'pin.json')
    for name in ('provenance.json', 'dependencies.json'):
        shutil.copyfile(managed / name, output / name)
    save(output / 'summary.json', {'schema': 1, 'passed': True, 'checks': checks,
         'archive_sha256': digest(archive), 'archive_bytes': archive.stat().st_size,
         'runner_sha256': digest(binary), 'runner_bytes': binary.stat().st_size, 'cli_sha256': digest(cli),
         'source_sha256': hashes, 'installed_bytes': sum(p.stat().st_size for p in managed.rglob('*') if p.is_file()),
         'path_programs': sorted(p.name for p in tools.iterdir()),
         'fixture_counts': {'bounded': 14, 'full_token': 108, 'lifecycle_scenarios': 6, 'lifecycle_transactions': 26},
         'limitations': ['Local macOS arm64 package, not a public release or notarized distribution',
                         'Installed Go and LLVM used for the starter; no Rust/Cargo/validator on test PATH',
                         'Captured-validator-compatible regressions; no fresh validator run',
                         'Dependency license files/inventory retained; redistribution review and project license remain open',
                         'Other hosts and clean OS installations are not validated']})
    print(f'PASS: pinned installation and managed SVM workflows. {output}', flush=True)


if __name__ == '__main__':
    main()
