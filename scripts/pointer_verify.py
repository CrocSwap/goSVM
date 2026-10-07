#!/usr/bin/env python3
"""Compare native Go pointer/method behavior with actual compiled SBF execution."""
import argparse
import hashlib
import json
import os
import platform
from pathlib import Path
import random
import shutil
import subprocess
import time

ROOT = Path(__file__).resolve().parents[1]
ALPHABET = '123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz'


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def save(path, value):
    path.write_text(json.dumps(value, indent=2) + '\n')


def load(path):
    return json.loads(path.read_text())


def b58(data):
    n, result = int.from_bytes(data, 'big'), ''
    while n:
        n, r = divmod(n, 58)
        result = ALPHABET[r] + result
    return '1' * (len(data) - len(data.lstrip(b'\0'))) + result


def require(ok, message):
    if not ok:
        raise RuntimeError(message)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', required=True, type=Path)
    parser.add_argument('--llvm', type=Path, default=Path.home() / '.cache/solana/v1.51/platform-tools/llvm')
    args = parser.parse_args()
    output = args.output.resolve()
    stage = ROOT / 'build/pointer-values' / output.name
    require(not output.exists() and not stage.exists(), 'choose a new output name; evidence/staging already exists')
    output.mkdir(parents=True)
    stage.mkdir(parents=True)
    env = dict(os.environ, GOCACHE=str(ROOT / 'build/lifecycle-go-cache'), GOSVM_CACHE=str(stage / 'cache'),
               GOPROXY='off', GOWORK='off')
    for name in ('GOSVM_TEST_RUNNER', 'GOSVM_TEST_FEATURES', 'SBF_LLVM', 'GOFLAGS'):
        env.pop(name, None)
    paths = [Path(__file__).resolve(), ROOT / 'scripts/array_native_driver.go.txt',
             ROOT / 'examples/pointer-values/program.go', ROOT / 'examples/pointer-values/model/model.go']
    for folder in ('internal/compiler', 'internal/testvm', 'internal/sbftest', 'internal/runner', 'cmd/gosvm'):
        paths.extend(p for p in (ROOT / folder).rglob('*') if p.is_file() and p.suffix != '.md')
    hashes = {str(p.relative_to(ROOT)): digest(p) for p in sorted(set(paths))}
    summary = {'schema': 1, 'passed': False, 'machine': platform.platform(), 'source_sha256': hashes, 'checks': [],
               'limitations': ['Value/pointer methods and constrained borrows; no escaping locals, pointer fields/elements, pointer-to-pointer, method values or heap allocation',
                               'Native-Go/SBF semantics comparison, not a fresh-validator oracle',
                               'Stack measurements are for this fixture, not a universal per-function bound',
                               'Compiler fixture; not bounded-starter, token-swap or dependency-heavy protocol performance']}

    def run(label, command, *, input_data=None):
        start = time.perf_counter()
        command = [str(x) for x in command]
        with (output / (label + '.log')).open('wb') as log:
            done = subprocess.run(command, cwd=ROOT, env=env, input=input_data, stdout=log,
                                  stderr=subprocess.STDOUT, timeout=180)
        summary['checks'].append({'check': label, 'command': command, 'exit_code': done.returncode,
                                  'wall_seconds': time.perf_counter() - start, 'log': label + '.log'})
        require(done.returncode == 0, f'{label}: see {output / (label + ".log")}')
        print(f'{label}: PASS', flush=True)

    try:
        run('differential-tests', ['go', 'test', './internal/compiler', '-run', 'TestPointer|TestImportedMethods', '-count=1', '-v'])
        cli = stage / 'gosvm'
        run('cli-build', ['bash', 'scripts/build-cli.sh', cli])
        archive = ROOT / 'results/svm/2026-10-05-runner-install-complete/gosvm-svm-runner-0.5.0-darwin-arm64.tar.gz'
        run('runner-install', [cli, 'runner', 'install', '-archive', archive])
        # Evidence must not become a package discovered by root go test ./....
        native_src = output / 'native-driver.go.txt'
        native_src.write_text((ROOT / 'scripts/array_native_driver.go.txt').read_text().replace('gosvm/examples/array-values', 'gosvm/examples/pointer-values'))
        native = stage / 'native'
        build_src = stage / 'native-driver.go'
        shutil.copyfile(native_src, build_src)
        run('native-build', ['go', 'build', '-ldflags=-linkmode=external', '-o', native, build_src])
        run('native-sign', ['codesign', '--force', '--sign', '-', native])
        rng = random.Random(322026)
        rows = []
        for sample in range(128):
            instruction = bytearray(rng.randrange(256) for _ in range(16))
            instruction[0], instruction[1] = 0, sample % 32
            rows.append({'Name': f'values-{sample:03}', 'State': bytes(rng.randrange(256) for _ in range(24)).hex(),
                         'Instruction': instruction.hex()})
        for mode in range(1, 16):
            for pos in (0, 3, 4, 255):
                instruction = bytearray(16)
                instruction[0], instruction[1], instruction[2] = mode, pos, pos
                instruction[8:16] = pos.to_bytes(8, 'little')
                rows.append({'Name': f'mode-{mode}-index-{pos}-seed-{pos}', 'State': bytes(24).hex(), 'Instruction': instruction.hex()})
        for index in (0, 3, 4, 1 << 32, 1 << 63, (1 << 64) - 1):
            instruction = bytearray(16)
            instruction[0], instruction[2] = 9, 255
            instruction[8:16] = index.to_bytes(8, 'little')
            rows.append({'Name': f'wide-index-{index}', 'State': bytes(24).hex(), 'Instruction': instruction.hex()})
        save(output / 'native-input.json', rows)
        run('native-expectations', [native], input_data=json.dumps(rows).encode())
        observed = load(output / 'native-expectations.log')
        save(output / 'native-expectations.json', observed)
        require(len(observed) == len(rows), 'native result count mismatch')
        elf = output / 'program.so'
        llvm = args.llvm.resolve()
        run('sbf-build', [cli, '-arch', 'v3', '-no-cache', '-llvm', llvm, '-o', elf, ROOT / 'examples/pointer-values'])
        run('emit-c', [cli, '-arch', 'v3', '-emit-c', '-o', output / 'program.c', ROOT / 'examples/pointer-values'])
        run('stack-compile', [llvm / 'bin/clang', '-target', 'sbf', '-mcpu=v3', '-O2', '-fno-builtin', '-fPIC',
            '-fno-stack-protector', '-std=c11', '-Werror', '-fstack-usage', '-c', output / 'program.c', '-o', stage / 'stack.o'])
        run('stack-link', [llvm / 'bin/ld.lld', '-z', 'notext', '-shared', '--Bdynamic', '--strip-all', '--entry',
            'entrypoint', '--script', ROOT / 'internal/compiler/sbf-v3.ld', '--no-undefined', '-o', stage / 'stack.so', stage / 'stack.o'])
        require(digest(elf) == digest(stage / 'stack.so'), 'stack instrumentation changed the tested ELF')
        shutil.copyfile(stage / 'stack.su', output / 'stack-usage.tsv')
        stack = []
        for line in (output / 'stack-usage.tsv').read_text().splitlines():
            function, size, kind = line.split('\t')
            stack.append({'function': function.rsplit(':', 1)[-1], 'bytes': int(size), 'kind': kind})
        require(stack and all(r['kind'] == 'static' and r['bytes'] <= 4096 for r in stack), 'fixture exceeds static SBF frame budget')
        program = b58(hashlib.sha256(b'pointer semantics program; never deploy').digest())
        account = b58(hashlib.sha256(b'pointer semantics state').digest())

        def state(data):
            return {'data': data, 'owner': program, 'lamports': 10000000, 'executable': False, 'rent_epoch': 0}

        def instruction(data):
            return {'program': 'program', 'accounts': [{'account': 'state', 'signer': False, 'writable': True}], 'data': data}

        cases = []
        for row, result in zip(rows, observed):
            require(row['Name'] == result['Name'], 'native row ordering mismatch')
            mode = bytes.fromhex(row['Instruction'])[0]
            if mode == 0:
                require(not result['Panicked'] and result['Code'] == 0, f'native success fixture failed: {row["Name"]}')
            if result['Panicked']:
                error = {'InstructionError': [0, 'ProgramFailedToComplete']}
            elif result['Code']:
                error = {'InstructionError': [0, {'Custom': result['Code']}]}
            else:
                error = None
            expected = row['State'] if error else result['Data']
            cases.append({'name': row['Name'], 'overrides': [{'name': 'state', 'state': state(row['State'])}],
                          'steps': [{'instructions': [instruction(row['Instruction'])],
                                     'expect': {'error': error, 'accounts': [{'account': 'state', 'state': state(expected)}]}}]})
        bad_write = next(row for row in rows if row['Name'] == 'mode-1-index-0-seed-0')
        cases.append({'name': 'successful-pointer-update-then-nil-rollback',
                      'steps': [{'instructions': [instruction(rows[0]['Instruction']), instruction(bad_write['Instruction'])],
                                 'expect': {'error': {'InstructionError': [1, 'ProgramFailedToComplete']},
                                            'accounts': [{'account': 'state', 'state': state(bytes(24).hex())}]}}]})
        custom = next(row for row in rows if row['Name'] == 'mode-11-index-255-seed-255')
        cases.append({'name': 'successful-pointer-update-then-custom-error-rollback',
                      'steps': [{'instructions': [instruction(rows[0]['Instruction']), instruction(custom['Instruction'])],
                                 'expect': {'error': {'InstructionError': [1, {'Custom': 9}]},
                                            'accounts': [{'account': 'state', 'state': state(bytes(24).hex())}]}}]})
        suite = {'format': 'gosvm-svm-fixtures-v1', 'program_id': program,
                 'payer_seed': hashlib.sha256(b'pointer semantics local payer; never fund').hexdigest(),
                 'accounts': [{'name': 'state', 'address': account, 'initial': state(bytes(24).hex())}], 'cases': cases}
        fixture = output / 'fixtures.json'
        save(fixture, suite)
        for sample in range(3):
            report = output / f'svm-{sample}.json'
            run(f'svm-{sample}', [cli, 'svm-test', '-elf', elf, '-fixtures', fixture, '-report', report])
            actual = load(report)
            require(actual['elf_sha256'] == digest(elf), 'SBF report points at a different ELF')
            if sample:
                require(actual['cases'] == load(output / 'svm-0.json')['cases'], 'case/CU/log results changed between runs')
        require(hashes == {str(p.relative_to(ROOT)): digest(p) for p in sorted(set(paths))}, 'sources changed during verification')
        summary.update(passed=True, scenarios=len(cases), transactions=sum(len(c['steps']) for c in cases),
                       native_vectors=len(rows), native_c_differential_vectors=1060, samples=3,
                       expected_failures=sum(s['expect']['error'] is not None for c in cases for s in c['steps']),
                       elf_bytes=elf.stat().st_size, elf_sha256=digest(elf), stack_frames=stack,
                       fixture_sha256=digest(fixture), native_expectations_sha256=digest(output / 'native-expectations.json'),
                       runner_sha256=actual['runner_sha256'], runtime_version=actual['runtime_version'])
        print(f'PASS: {len(cases)} pointer scenarios, native-Go/SBF bytes and failures, three repetitions. {output}', flush=True)
    except Exception as error:
        summary['failure'] = str(error)
        raise
    finally:
        save(output / 'summary.json', summary)


if __name__ == '__main__':
    main()
