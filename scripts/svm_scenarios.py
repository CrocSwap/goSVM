#!/usr/bin/env python3
"""Validate ordered Clock/Rent fixture controls with the real SBF syscall probe."""
import argparse
import copy
import hashlib
import json
import os
from pathlib import Path
import shutil
import socket
import struct
import subprocess
import time

ROOT = Path(__file__).resolve().parents[1]
CLOCK_ID = 'SysvarC1ock11111111111111111111111111111111'
RENT_ID = 'SysvarRent111111111111111111111111111111111'
SYSVAR_OWNER = 'Sysvar1111111111111111111111111111111111111'
ALPHABET = '123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz'


def b58(data):
    value = int.from_bytes(data, 'big')
    out = ''
    while value:
        value, digit = divmod(value, 58)
        out = ALPHABET[digit] + out
    return '1' * (len(data) - len(data.lstrip(b'\0'))) + out


def key(label):
    return hashlib.sha256(label.encode()).digest()


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def save(path, value):
    path.write_text(json.dumps(value, indent=2) + '\n')


def load(path):
    return json.loads(path.read_text())


def clock_bytes(clock):
    return struct.pack('<QqQQq', clock['slot'], clock['epoch_start_timestamp'], clock['epoch'],
                       clock['leader_schedule_epoch'], clock['unix_timestamp'])


def rent_bytes(rent):
    return struct.pack('<QdB', rent['lamports_per_byte_year'], rent['exemption_threshold'], rent['burn_percent'])


def state(data, owner, lamports):
    return {'data': data.hex(), 'owner': owner, 'lamports': lamports, 'executable': False, 'rent_epoch': 0}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--samples', type=int, default=3)
    parser.add_argument('--llvm', type=Path, default=Path.home() / '.cache/solana/v1.51/platform-tools/llvm')
    args = parser.parse_args()
    if not 3 <= args.samples <= 10:
        parser.error('--samples requires 3..10')
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=False)
    stage = ROOT / 'build/svm-scenarios' / output.name
    stage.mkdir(parents=True, exist_ok=False)
    env = dict(os.environ, GOCACHE=str(ROOT / 'build/lifecycle-go-cache'), GOSVM_CACHE=str(stage / 'cache'),
               GOWORK='off', GOPROXY='off')
    env.pop('GOSVM_TEST_RUNNER', None)
    env.pop('GOSVM_TEST_FEATURES', None)
    hashes = {}
    for folder in ('internal/testvm', 'internal/sbftest', 'internal/runner', 'cmd/gosvm', 'internal/project'):
        hashes.update({str(p.relative_to(ROOT)): digest(p) for p in (ROOT / folder).rglob('*')
                       if p.is_file() and p.suffix != '.md'})
    for name in ('scripts/svm_scenarios.py', 'benchmarks/svm-controls/probe.c', 'internal/compiler/sbf-v3.ld'):
        hashes[name] = digest(ROOT / name)
    checks = []

    def run(label, command, *, expected=0, contains=None):
        start = time.perf_counter()
        result = subprocess.run([str(x) for x in command], cwd=ROOT, env=env, stdout=subprocess.PIPE,
                                stderr=subprocess.STDOUT, timeout=180)
        (output / (label + '.log')).write_bytes(result.stdout)
        assert result.returncode == expected, (label, result.stdout.decode())
        if contains:
            assert contains.encode() in result.stdout, (label, result.stdout.decode())
        checks.append({'check': label, 'command': [str(x) for x in command], 'exit_code': result.returncode,
                       'wall_seconds': time.perf_counter() - start, 'expected': expected})
        print(f'{label}: PASS', flush=True)

    cli = stage / 'gosvm'
    run('cli-build', ['bash', 'scripts/build-cli.sh', cli])
    archive = ROOT / 'results/svm/2026-10-05-runner-install-complete/gosvm-svm-runner-0.5.0-darwin-arm64.tar.gz'
    run('runner-install', [cli, 'runner', 'install', '-archive', archive])
    runner = stage / 'cache/runners/0.5.0/darwin-arm64/bin/gosvm-svm-runner'
    probe, llvm = output / 'probe.so', args.llvm.resolve()
    run('probe-compile', [llvm / 'bin/clang', '-target', 'sbf', '-mcpu=v3', '-O2', '-fno-builtin', '-fPIC',
        '-fno-stack-protector', '-std=c11', '-Werror', '-c', 'benchmarks/svm-controls/probe.c', '-o', stage / 'probe.o'])
    run('probe-link', [llvm / 'bin/ld.lld', '-z', 'notext', '-shared', '--Bdynamic', '--strip-all', '--entry',
        'entrypoint', '--script', 'internal/compiler/sbf-v3.ld', '--no-undefined', '-o', probe, stage / 'probe.o'])
    assert digest(probe) == load(ROOT / 'results/svm/2026-10-05-controls-final/summary.json')['artifacts']['probe.so']['sha256']
    program = b58(key('ordered-sysvar syscall probe'))
    a = {'clock': {'slot': 9007199254740993, 'epoch_start_timestamp': -23, 'epoch': 7,
                   'leader_schedule_epoch': 9, 'unix_timestamp': -123456},
         'rent': {'lamports_per_byte_year': 777, 'exemption_threshold': 1.5, 'burn_percent': 17}}
    b = {'clock': {'slot': 18446744073709551615, 'epoch_start_timestamp': -9223372036854775808,
                   'epoch': 11, 'leader_schedule_epoch': 12, 'unix_timestamp': 9223372036854775807},
         'rent': {'lamports_per_byte_year': 999, 'exemption_threshold': 2.5, 'burn_percent': 19}}
    c = {'clock': {'slot': 42, 'epoch_start_timestamp': -99, 'epoch': 0,
                   'leader_schedule_epoch': 2, 'unix_timestamp': -876543},
         'rent': {'lamports_per_byte_year': 432, 'exemption_threshold': 0.625, 'burn_percent': 0}}
    ba, ab = {'clock': b['clock'], 'rent': a['rent']}, {'clock': a['clock'], 'rent': b['rent']}

    def step(command, effective, counter, *, controls=None, failed=False, atomic=False, persisted=None, fresh=False):
        # Independent little-endian ABI encoding, not decoded runner output.
        observed = persisted if persisted is not None else effective
        data = clock_bytes(observed['clock']) + rent_bytes(observed['rent']) + bytes(7) + struct.pack('<Q', counter)
        instructions = [{'program': 'program', 'accounts': [{'account': 'state', 'writable': True, 'signer': False}],
                         'data': bytes([command]).hex()}]
        if atomic:
            instructions.append(dict(instructions[0], data='01'))
        error = {'InstructionError': [1 if atomic else 0, {'Custom': 9}]} if failed else None
        value = {'instructions': instructions, 'expect': {'error': error, 'accounts': [
            {'account': 'state', 'state': state(data, program, 10000000)},
            {'account': 'clock', 'state': state(clock_bytes(effective['clock']), SYSVAR_OWNER, 1)},
            {'account': 'rent', 'state': state(rent_bytes(effective['rent']), SYSVAR_OWNER, 1)}]}}
        if controls:
            value['sysvars'] = controls
        if fresh:
            value['fresh_blockhash'] = True
        return value

    suite = {'format': 'gosvm-svm-fixtures-v1', 'program_id': program,
             'payer_seed': key('ordered-sysvar fee payer; never fund').hex(), 'sysvars': a,
             'accounts': [{'name': 'state', 'address': b58(key('ordered-sysvar state')),
                           'initial': state(bytes(72), program, 10000000)},
                          {'name': 'clock', 'address': CLOCK_ID}, {'name': 'rent', 'address': RENT_ID}],
             'cases': [
                 {'name': 'ordered-updates', 'steps': [step(0, a, 1), step(2, ba, 2, controls={'clock': b['clock']}),
                     step(3, b, 3, controls={'rent': b['rent']}),
                     step(4, c, 3, controls=c, failed=True, atomic=True, persisted=b),
                     step(5, c, 4), step(0, c, 5, fresh=True)]},
                 {'name': 'reset-after-updates', 'steps': [step(0, a, 1)]},
                 {'name': 'partial-clock', 'steps': [step(2, ba, 1, controls={'clock': b['clock']}), step(3, ba, 2)]},
                 {'name': 'partial-rent', 'steps': [step(2, ab, 1, controls={'rent': b['rent']}), step(3, ab, 2)]},
                 {'name': 'reset-after-partial', 'steps': [step(0, a, 1)]},
                 {'name': 'combined-failed-rollback', 'steps': [step(6, b, 1, controls=b),
                     step(1, c, 1, controls=c, failed=True, persisted=b), step(7, c, 2)]}]}
    fixture = output / 'fixtures.json'
    save(fixture, suite)
    save(output / 'fixtures-initial.json', suite)
    report = stage / 'results.json'
    common = [cli, 'svm-test', '-elf', probe, '-fixtures', fixture, '-report', report]
    expected_sysvars = [[a, ba, b, c, c, c], [a], [ba, ba], [ab, ab], [a], [b, c, c]]
    reference = None
    for sample in range(args.samples):
        run(f'ordered-{sample}', common)
        value = load(report)
        if reference is None:
            reference = value['cases']
            # Freeze CU only after independent bytes/errors pass the first run;
            # these are repeatability checks, not a validator CU oracle.
            for case, row in zip(suite['cases'], reference):
                for declared, actual in zip(case['steps'], row['steps']):
                    declared['expect']['cu'] = actual['cu']
            save(fixture, suite)
        assert value['cases'] == reference
        assert [[step['sysvars'] for step in case['steps']] for case in value['cases']] == expected_sysvars
        assert value['runner_sha256'] == digest(runner)
        shutil.copyfile(report, output / f'ordered-{sample}.json')
    # Replay the CU-pinned file after calibration, including the first sample.
    run('cu-pinned', common)
    assert load(report)['cases'] == reference
    shutil.copyfile(report, output / 'ordered-results.json')
    run('selected', [*common, '-run', '^reset-after-updates$'])
    assert load(report)['cases'] == [reference[1]]
    shutil.copyfile(report, output / 'selected.json')
    reversed_suite = copy.deepcopy(suite)
    reversed_suite['cases'].reverse()
    reverse = output / 'reversed.json'
    save(reverse, reversed_suite)
    run('reversed', [cli, 'svm-test', '-elf', probe, '-fixtures', reverse, '-report', report])
    assert load(report)['cases'] == list(reversed(reference))
    shutil.copyfile(report, output / 'reversed-results.json')
    # A Clock update must not silently erase duplicate history/change blockhash.
    duplicate = copy.deepcopy(suite)
    duplicate['cases'] = [{'name': 'duplicate-clock', 'steps': [step(0, a, 1), step(0, ba, 2, controls={'clock': b['clock']})]}]
    duplicate_path = output / 'duplicate-clock.json'
    save(duplicate_path, duplicate)
    run('duplicate-rejection', [cli, 'svm-test', '-elf', probe, '-fixtures', duplicate_path, '-report', report],
        expected=1, contains='AlreadyProcessed')
    assert not report.exists()
    for label, controls in [('incomplete', {'clock': {'slot': 1}}), ('empty', {}), ('null', None),
                             ('bad-rent', dict(a, rent=dict(a['rent'], burn_percent=101)))]:
        invalid = copy.deepcopy(suite)
        invalid['cases'][-1]['steps'][0]['sysvars'] = controls
        invalid_path = output / (label + '.json')
        save(invalid_path, invalid)
        shutil.copyfile(output / 'ordered-results.json', report)
        run(label + '-rejection', [cli, 'svm-test', '-elf', probe, '-fixtures', invalid_path, '-report', report,
                                  '-run', '^reset-after-updates$'], expected=1, contains='sysvars')
        assert not report.exists()
    for label, elf, fixtures, expected in (
            ('full-token', ROOT / 'build/anchor/go-token.so', ROOT / 'results/svm/2026-10-05-general-complete/token-fixtures.json',
             ROOT / 'results/svm/2026-10-05-general-complete/general-0.json'),
            ('lifecycle', ROOT / 'results/svm/2026-10-05-lifecycle-verified/go-token.so',
             ROOT / 'results/svm/2026-10-05-lifecycle-verified/fixtures.json',
             ROOT / 'results/svm/2026-10-05-lifecycle-verified/runner-results.json')):
        run(label, [cli, 'svm-test', '-elf', elf, '-fixtures', fixtures, '-report', report])
        assert load(report)['cases'] == load(expected)['cases']
        shutil.copyfile(report, output / (label + '.json'))
    project = stage / 'starter'
    run('scaffold', [cli, 'new', project])
    run('starter', [cli, 'test', '--svm', '-dir', project, '-llvm', llvm])
    starter_report = project / 'build/svm-results.json'
    assert load(starter_report)['cases'] == load(ROOT / 'results/svm/2026-10-05-workflow-complete/bounded-restored-0-results.json')['cases']
    shutil.copyfile(starter_report, output / 'starter.json')
    run('restored', common)
    assert load(report)['cases'] == reference
    # Record the current sandbox's ability to start a fresh local RPC oracle.
    # No validator agreement is inferred from this runtime-only corpus.
    with socket.socket() as local:
        try:
            local.bind(('127.0.0.1', 0))
            oracle = {'rpc_bind_allowed': True, 'fresh_validator_run': False}
        except OSError as error:
            oracle = {'rpc_bind_allowed': False, 'fresh_validator_run': False, 'error': str(error)}
    assert hashes == {p: digest(ROOT / p) for p in hashes}
    save(output / 'summary.json', {'schema': 1, 'passed': True, 'checks': checks, 'samples': args.samples,
        'scenarios': len(reference), 'transactions': sum(len(c['steps']) for c in reference),
        'initial_fixture_sha256': digest(output / 'fixtures-initial.json'),
        'source_sha256': hashes, 'probe_sha256': digest(probe), 'probe_bytes': probe.stat().st_size,
        'runner_sha256': digest(runner), 'cli_sha256': digest(cli), 'fixture_sha256': digest(fixture),
        'validator': oracle, 'limitations': ['C runtime probe; Go SDK sysvar authoring remains open',
            'Clock/Rent controls do not advance consensus/SlotHashes/history or reset blockhash',
            'CU calibrated after independent state/error validation; no new validator CU oracle',
            'macOS arm64, local installed tools; no performance ranking or dependency-heavy protocol measurement']})
    print(f'PASS: ordered sysvars, rollback/isolation, full-token/lifecycle/starter regressions. {output}', flush=True)


if __name__ == '__main__':
    main()
