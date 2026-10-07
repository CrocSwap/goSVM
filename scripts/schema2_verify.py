#!/usr/bin/env python3
"""Verify schema-2 generated authoring against independent wire/math and compiled SBF."""
import argparse
import copy
import hashlib
import json
import os
import platform
from pathlib import Path
import random
import shutil
import struct
import subprocess
import time

ROOT = Path(__file__).resolve().parents[1]
ALPHABET = '123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz'
MAX = (1 << 64) - 1


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def save(path, value):
    path.write_text(json.dumps(value, indent=2) + '\n')


def load(path):
    return json.loads(path.read_text())


def require(ok, message):
    if not ok:
        raise RuntimeError(message)


def b58(data):
    n, result = int.from_bytes(data, 'big'), ''
    while n:
        n, r = divmod(n, 58)
        result = ALPHABET[r] + result
    return '1' * (len(data) - len(data.lstrip(b'\0'))) + result


def disc(kind, name):
    # Independent wire oracle, not an import or parse of generator offsets.
    return hashlib.sha256(f'gosvm:{kind}:{name}:v1'.encode()).digest()[:8]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', required=True, type=Path)
    parser.add_argument('--llvm', type=Path, default=Path.home() / '.cache/solana/v1.51/platform-tools/llvm')
    args = parser.parse_args()
    output = args.output.resolve()
    stage = ROOT / 'build/schema2' / output.name
    require(not output.exists() and not stage.exists(), 'choose a new output name; evidence/staging already exists')
    output.mkdir(parents=True)
    stage.mkdir(parents=True)
    env = dict(os.environ, GOCACHE=str(ROOT / 'build/lifecycle-go-cache'), GOSVM_CACHE=str(stage / 'cache'),
               GOPROXY='off', GOSUMDB='off', GOTOOLCHAIN='local', GOWORK='off')
    for name in ('GOSVM_TEST_RUNNER', 'GOSVM_TEST_FEATURES', 'SBF_LLVM', 'GOFLAGS'):
        env.pop(name, None)
    example = ROOT / 'examples/multi-state'
    paths = [Path(__file__).resolve(), ROOT / 'scripts/schema2_native_driver.go.txt']
    for folder in ('internal/compiler', 'internal/project', 'internal/testvm', 'internal/sbftest', 'internal/runner', 'cmd/gosvm'):
        paths.extend(p for p in (ROOT / folder).rglob('*') if p.is_file() and p.suffix != '.md')
    paths.extend(p for p in example.rglob('*') if p.is_file() and 'build' not in p.relative_to(example).parts)
    hashes = {str(p.relative_to(ROOT)): digest(p) for p in sorted(set(paths))}
    summary = {'schema': 1, 'passed': False, 'machine': platform.platform(), 'source_sha256': hashes, 'checks': [],
               'limitations': ['Two instructions/two owned layouts; preloaded ledger accounts, no SPL-token transfers or lifecycle',
                               'Native-Go and compiled SBF; no fresh-validator comparison or external developer trial',
                               'Frame/CU/footprint measurements apply only to this prototype',
                               'Checked token/PDA/CPI and full-token/escrow application gates remain open']}

    def run(label, command, *, input_data=None, cwd=ROOT):
        start = time.perf_counter()
        command = [str(x) for x in command]
        with (output / (label + '.log')).open('wb') as log:
            done = subprocess.run(command, cwd=cwd, env=env, input=input_data, stdout=log, stderr=subprocess.STDOUT, timeout=180)
        summary['checks'].append({'check': label, 'command': command, 'cwd': str(cwd), 'exit_code': done.returncode,
                                  'wall_seconds': time.perf_counter() - start, 'log': label + '.log'})
        require(done.returncode == 0, f'{label}: see {output / (label + ".log")}')
        print(f'{label}: PASS', flush=True)

    try:
        run('generator-tests', ['go', 'test', './internal/project', '-run', 'TestSchema2', '-count=1', '-v'])
        cli = stage / 'gosvm'
        run('cli-build', ['bash', 'scripts/build-cli.sh', cli])
        archive = ROOT / 'results/svm/2026-10-05-runner-install-complete/gosvm-svm-runner-0.5.0-darwin-arm64.tar.gz'
        run('runner-install', [cli, 'runner', 'install', '-archive', archive])
        # Reproduce authoring through the CLI; the committed example's generated
        # files must match a fresh scaffold byte-for-byte.
        project = stage / 'multi-state'
        run('scaffold', [cli, 'new', '-schema', '2', '-module', 'example.org/gosvm/multi-state', project])
        run('check', [cli, 'check', '-dir', project])
        run('native-tests', [cli, 'test', '-dir', project])
        for relative in ('zz_gosvm.go', 'client/zz_gosvm.go', 'idl.json', 'layouts.json'):
            require((example / relative).read_bytes() == (project / relative).read_bytes(), f'scaffold differs: {relative}')
        native_dir = stage / 'native'
        native_dir.mkdir()
        source = ROOT / 'scripts/schema2_native_driver.go.txt'
        shutil.copyfile(source, native_dir / 'main.go')
        shutil.copyfile(source, output / 'native-driver.go.txt')
        (native_dir / 'go.mod').write_text('module schema2-native\n\ngo 1.22\n\nrequire (\n example.org/gosvm/multi-state v0.0.0\n gosvm v0.0.0\n)\n'
                                         f'replace example.org/gosvm/multi-state => {project}\nreplace gosvm => {project / ".gosvm/sdk"}\n')
        native = native_dir / 'native'
        run('native-build', ['go', 'build', '-ldflags=-linkmode=external', '-o', native, '.'], cwd=native_dir)
        run('native-sign', ['codesign', '--force', '--sign', '-', native])
        seed = hashlib.sha256(b'schema2 local authority; never fund').hexdigest()
        run('authority-key', [native, '--key', seed])
        authority = bytes.fromhex((output / 'authority-key.log').read_text().strip())
        program_bytes = hashlib.sha256(b'schema2 local program; never deploy').digest()
        program = b58(program_bytes)
        addresses = {name: hashlib.sha256(f'schema2 {name}'.encode()).digest() for name in ('source', 'destination', 'policy')}
        addresses['authority'] = authority
        refs = [('source', False, True), ('destination', False, True), ('policy', False, False), ('authority', True, False)]

        def vault(balance, moves=0, tag=3, epoch=0x12345678, key=authority):
            return (disc('account', 'Vault') + key + struct.pack('<BIQQ', tag, epoch, balance, moves)).hex()

        def policy(limit, key=authority):
            return (disc('account', 'Policy') + key + struct.pack('<Q', limit)).hex()

        def move(amount):
            return (disc('instruction', 'Move') + struct.pack('<Q', amount)).hex()

        def limit(value):
            return (disc('instruction', 'SetLimit') + struct.pack('<Q', value)).hex()

        def account(data, owner=program, executable=False):
            return {'data': data, 'owner': owner, 'lamports': 10000000, 'executable': executable, 'rent_epoch': 0}

        def initial(source=100, destination=5, ceiling=50, sm=0, dm=0, tag=3, epoch=0x12345678):
            return {'source': account(vault(source, sm, tag, epoch)), 'destination': account(vault(destination, dm, tag, epoch)),
                    'policy': account(policy(ceiling)), 'authority': account('', b58(bytes(32)))}

        def instruction(data, meta=refs):
            return {'program': 'program', 'accounts': [{'account': n, 'signer': s, 'writable': w} for n, s, w in meta], 'data': data}

        def assertions(states):
            return [{'account': name, 'state': state} for name, state in states.items()]

        def error(code, index=0):
            return None if code == 0 else {'InstructionError': [index, {'Custom': code}]}

        rows, cases, independent = [], [], []

        def add(name, before, data, code, after=None, meta=refs):
            after = before if after is None else after
            # Native contexts receive message-wide merged flags, including aliases.
            merged = {}
            for n, s, w in meta:
                old = merged.get(n, (False, False))
                merged[n] = (old[0] or s, old[1] or w)
            rows.append({'Name': name, 'ID': program_bytes.hex(), 'Instruction': data,
                         'Accounts': [{'Key': addresses[n].hex(), 'Owner': (program_bytes if before[n]['owner'] == program else bytes(32)).hex(),
                                       'Data': before[n]['data'], 'Signer': merged[n][0], 'Writable': merged[n][1],
                                       'Executable': before[n]['executable']} for n, _, _ in meta]})
            # Cases changing owner use an explicit independent hex key below.
            for native_account, (n, _, _) in zip(rows[-1]['Accounts'], meta):
                if before[n]['owner'] == b58(bytes([9]) * 32):
                    native_account['Owner'] = (bytes([9]) * 32).hex()
            independent.append({'Name': name, 'Code': code, 'Data': [after[n]['data'] for n, _, _ in meta]})
            cases.append({'name': name, 'overrides': [{'name': n, 'state': s} for n, s in before.items()],
                          'steps': [{'instructions': [instruction(data, meta)], 'expect': {'error': error(code), 'accounts': assertions(after)}}]})

        rng = random.Random(22026)
        for sample in range(128):
            amount = rng.randrange(1, 1 << 63)
            src, dst = rng.randrange(amount, 1 << 64), rng.randrange(0, MAX - amount + 1)
            ceiling = rng.randrange(amount, 1 << 64)
            tag, epoch = rng.randrange(256), rng.randrange(1 << 32)
            before = initial(src, dst, ceiling, sample, sample + 3, tag, epoch)
            after = copy.deepcopy(before)
            after['source']['data'] = vault(src - amount, sample + 1, tag, epoch)
            after['destination']['data'] = vault(dst + amount, sample + 4, tag, epoch)
            add(f'move-wide-{sample:03}', before, move(amount), 0, after)
        add('set-limit-wide', initial(), limit(MAX), 0, dict(initial(), policy=account(policy(MAX))), [('policy', False, True), ('authority', True, False)])
        add('zero', initial(), move(0), 1)
        add('limit', initial(), move(51), 1)
        add('insufficient', initial(source=9), move(10), 1)
        add('destination-overflow', initial(destination=MAX), move(10), 1)
        add('source-counter', initial(sm=MAX), move(10), 2)
        add('destination-counter', initial(dm=MAX), move(10), 2)
        add('no-accounts', initial(), move(10), 6000, meta=[])
        add('few-accounts', initial(), move(10), 6000, meta=refs[:3])
        add('extra-account', initial(), move(10), 6000, meta=refs + [refs[-1]])
        for which in (0, 1):
            meta = list(refs)
            meta[which] = (meta[which][0], False, False)
            add(f'readonly-{which}', initial(), move(10), 6001, meta=meta)
        meta = refs[:-1] + [('authority', False, False)]
        add('missing-signature', initial(), move(10), 6001, meta=meta)
        before = initial()
        before['source']['owner'] = b58(bytes([9]) * 32)
        add('owner', before, move(10), 6002)
        for name in ('source', 'destination', 'policy'):
            for kind in ('short', 'long', 'tag'):
                before = initial()
                data = bytes.fromhex(before[name]['data'])
                before[name]['data'] = (data[:-1] if kind == 'short' else data + b'\0' if kind == 'long' else bytes([data[0] ^ 1]) + data[1:]).hex()
                add(f'{name}-{kind}', before, move(10), 6003)
        add('duplicate-vault', initial(), move(10), 6006, meta=[refs[0], refs[0], refs[2], refs[3]])
        add('duplicate-policy-before-layout', initial(), move(10), 6006, meta=[refs[0], refs[1], ('source', False, False), refs[3]])
        for name in ('source', 'destination'):
            before = initial()
            data = bytes.fromhex(before[name]['data'])
            before[name]['data'] = (data[:8] + bytes([data[8] ^ 1]) + data[9:]).hex()
            add(f'{name}-authority', before, move(10), 6007)
        data = bytes.fromhex(move(10))
        for name, bad in (('instruction-short', data[:7]), ('instruction-long', data + b'\0'), ('instruction-unknown', bytes([data[0] ^ 1]) + data[1:])):
            add(name, initial(), bad.hex(), 6004)
        add('instruction-before-count', initial(), '', 6004, meta=[])
        before = initial()
        before['source']['owner'] = b58(bytes([9]) * 32)
        add('privileges-before-owner', before, move(10), 6001, meta=[('source', False, False)] + refs[1:])
        before = initial()
        before['source']['owner'] = b58(bytes([9]) * 32)
        add('owner-before-alias', before, move(10), 6002, meta=[refs[0], refs[0], refs[2], refs[3]])
        save(output / 'native-input.json', rows)
        save(output / 'independent-expectations.json', independent)
        run('native-expectations', [native], input_data=json.dumps(rows).encode())
        observed = load(output / 'native-expectations.log')
        require(observed == independent, 'native output differs from independent packed-wire/math/error oracle')
        save(output / 'native-expectations.json', observed)

        # A writable policy in SetLimit becomes writable across the message even
        # though Move declares it readonly. The readonly result must stay ignored.
        before = initial()
        after = initial(source=90, destination=15, ceiling=20, sm=1, dm=1)
        cases.append({'name': 'set-limit-then-move-merged-privileges', 'steps': [{'instructions': [instruction(limit(20), [('policy', False, True), ('authority', True, False)]), instruction(move(10))], 'expect': {'error': None, 'accounts': assertions(after)}}]})
        cases.append({'name': 'set-limit-then-failed-move-atomic-rollback', 'steps': [{'instructions': [instruction(limit(3), [('policy', False, True), ('authority', True, False)]), instruction(move(10))], 'expect': {'error': error(1, 1), 'accounts': assertions(before)}}]})
        cases.append({'name': 'move-then-invalid-dispatch-atomic-rollback', 'steps': [{'instructions': [instruction(move(10)), instruction('')], 'expect': {'error': error(6004, 1), 'accounts': assertions(before)}}]})
        changed = dict(before, policy=account(policy(20)))
        cases.append({'name': 'ordered-stateful-transactions', 'steps': [
            {'instructions': [instruction(limit(20), [('policy', False, True), ('authority', True, False)])], 'expect': {'error': None, 'accounts': assertions(changed)}},
            {'instructions': [instruction(move(10))], 'expect': {'error': None, 'accounts': assertions(after)}}]})
        suite = {'format': 'gosvm-svm-fixtures-v1', 'program_id': program,
                 'payer_seed': hashlib.sha256(b'schema2 local payer; never fund').hexdigest(),
                 'signers': [{'name': 'authority', 'seed': seed}],
                 'accounts': [{'name': n, 'address': b58(addresses[n]), 'initial': s} for n, s in initial().items()], 'cases': cases}
        fixture = output / 'fixtures.json'
        save(fixture, suite)
        llvm = args.llvm.resolve()
        run('sbf-build', [cli, 'build', '-dir', project, '-no-cache', '-llvm', llvm, '-timings'])
        elf = output / 'program.so'
        shutil.copyfile(project / 'build/program.so', elf)
        run('emit-c', [cli, '-arch', 'v3', '-emit-c', '-o', output / 'program.c', project])
        run('stack-compile', [llvm / 'bin/clang', '-target', 'sbf', '-mcpu=v3', '-O2', '-fno-builtin', '-fPIC', '-fno-stack-protector', '-std=c11', '-Werror', '-DGOSVM_SBF_V3=1', '-fstack-usage', '-c', output / 'program.c', '-o', stage / 'stack.o'])
        run('stack-link', [llvm / 'bin/ld.lld', '-z', 'notext', '-shared', '--Bdynamic', '--strip-all', '--entry', 'entrypoint', '--script', ROOT / 'internal/compiler/sbf-v3.ld', '--no-undefined', '-o', stage / 'stack.so', stage / 'stack.o'])
        require(digest(elf) == digest(stage / 'stack.so'), 'stack instrumentation changed the tested ELF')
        shutil.copyfile(stage / 'stack.su', output / 'stack-usage.tsv')
        frames = []
        for line in (output / 'stack-usage.tsv').read_text().splitlines():
            name, size, kind = line.split('\t')
            frames.append({'function': name.rsplit(':', 1)[-1], 'bytes': int(size), 'kind': kind})
        require(frames and all(f['kind'] == 'static' and f['bytes'] <= 4096 for f in frames), 'prototype exceeds SBF frame bound')
        for sample in range(3):
            report = output / f'svm-{sample}.json'
            run(f'svm-{sample}', [cli, 'svm-test', '-elf', elf, '-fixtures', fixture, '-report', report])
            actual = load(report)
            require(actual['elf_sha256'] == digest(elf), 'report executed a different ELF')
            if sample:
                require(actual['cases'] == load(output / 'svm-0.json')['cases'], 'case/CU/log results changed between repetitions')
        # Also exercise the project's native/build/fast-SVM command end to end.
        run('project-svm-workflow', [cli, 'test', '-dir', project, '-svm', '-svm-fixtures', fixture, '-svm-run', '^set-limit-then', '-llvm', llvm])
        require(hashes == {str(p.relative_to(ROOT)): digest(p) for p in sorted(set(paths))}, 'sources changed during verification')
        summary.update(passed=True, native_vectors=len(rows), scenarios=len(cases), transactions=sum(len(c['steps']) for c in cases), samples=3,
                       expected_failures=sum(s['expect']['error'] is not None for c in cases for s in c['steps']),
                       elf_bytes=elf.stat().st_size, elf_sha256=digest(elf), stack_frames=frames, fixture_sha256=digest(fixture),
                       native_expectations_sha256=digest(output / 'native-expectations.json'), runner_sha256=actual['runner_sha256'], runtime_version=actual['runtime_version'])
        print(f'PASS: {len(cases)} schema-2 SBF scenarios in three repetitions. {output}', flush=True)
    except Exception as error:
        summary['failure'] = str(error)
        raise
    finally:
        save(output / 'summary.json', summary)


if __name__ == '__main__':
    main()
