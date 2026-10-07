#!/usr/bin/env python3
"""Compare shared Go packages in native Go and SBF, including an isolated client."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import random
import shutil
import subprocess
import time

from array_verify import ROOT, b58, digest, load, require, save

APP = ROOT / 'examples/shared-packages'


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--llvm', type=Path, default=Path.home() / '.cache/solana/v1.51/platform-tools/llvm')
    args = parser.parse_args()
    output, llvm = args.output.resolve(), args.llvm.resolve()
    stage = ROOT / 'build/shared-imports' / output.name
    require(not output.exists() and not stage.exists(), 'choose new evidence/staging directories')
    output.mkdir(parents=True)
    stage.mkdir(parents=True)
    env = dict(os.environ, GOCACHE=str(ROOT / 'build/lifecycle-go-cache'), GOSVM_CACHE=str(stage / 'cache'),
               GOPROXY='off', GOWORK='off', GOTOOLCHAIN='local', GOSUMDB='off')
    for name in ('GOSVM_TEST_RUNNER', 'GOSVM_TEST_FEATURES', 'SBF_LLVM', 'GOFLAGS'):
        env.pop(name, None)
    paths = [Path(__file__).resolve(), ROOT / 'scripts/array_verify.py', ROOT / 'scripts/array_native_driver.go.txt']
    for folder in ('internal/compiler', 'internal/runner', 'internal/sbftest', 'internal/testvm', 'cmd/gosvm', 'examples/shared-packages'):
        paths.extend(p for p in (ROOT / folder).rglob('*') if p.is_file() and p.suffix != '.md')
    hashes = {str(p.relative_to(ROOT)): digest(p) for p in sorted(set(paths))}
    summary = {'schema': 1, 'passed': False, 'machine': platform.platform(), 'source_sha256': hashes, 'checks': [],
               'limitations': ['Offline module imports and compiler subset; no workspace/vendor selection',
                               'Bounded arithmetic/state example, not full token swap or dependency-heavy protocol',
                               'Handwritten shared wire codecs, not schema-2 generated clients',
                               'Unpublished example modules use local development replacements',
                               'Native-Go/SBF comparison, not fresh-validator verification']}

    def run(label, command, *, cwd=ROOT, input_data=None):
        command = [str(x) for x in command]
        start = time.perf_counter()
        with (output / (label + '.log')).open('wb') as log:
            done = subprocess.run(command, cwd=cwd, env=env, input=input_data, stdout=log,
                                  stderr=subprocess.STDOUT, timeout=180)
        summary['checks'].append({'check': label, 'command': command, 'cwd': str(cwd), 'exit_code': done.returncode,
                                  'wall_seconds': time.perf_counter() - start, 'log': label + '.log'})
        require(done.returncode == 0, f'{label}: see {output / (label + ".log")}')
        print(f'{label}: PASS', flush=True)

    try:
        run('compiler-tests', ['go', 'test', '-ldflags=-linkmode=external', './internal/compiler', '-run', 'TestShared|TestImported|TestModule|TestResolver', '-count=1', '-v'])
        run('application-tests', ['go', 'test', './...', '-count=1'], cwd=APP)
        cli = stage / 'gosvm'
        run('cli-build', ['bash', 'scripts/build-cli.sh', cli])
        run('runner-install', [cli, 'runner', 'install', '-archive', ROOT / 'results/svm/2026-10-05-runner-install-complete/gosvm-svm-runner-0.5.0-darwin-arm64.tar.gz'])
        # Copy only pure shared libraries; the independent client has no SDK,
        # compiler, on-chain entrypoint or reference to the goSVM checkout.
        shared, client = stage / 'shared', stage / 'service'
        shared.mkdir()
        shutil.copyfile(APP / 'go.mod', shared / 'go.mod')
        for name in ('model', 'math', 'quote', 'wire'):
            shutil.copytree(APP / name, shared / name)
        shutil.copytree(APP / 'service', client)
        (client / 'go.mod').write_text((client / 'go.mod').read_text().replace('=> ..', '=> ../shared'))
        run('isolated-service', ['go', 'run', '-ldflags=-linkmode=external', '.'], cwd=client)
        service = load(output / 'isolated-service.log')
        require(service['Quote'] == 1998, 'isolated quote changed')
        run('isolated-dependencies', ['go', 'list', '-deps', '-json', '.'], cwd=client)
        # A stream of JSON objects; no non-standard dependency may resolve back
        # into the checkout. The copied source directories are ignored staging.
        raw = (output / 'isolated-dependencies.log').read_text()
        decoder, position, modules = json.JSONDecoder(), 0, []
        while position < len(raw):
            if raw[position].isspace():
                position += 1
                continue
            package, position = decoder.raw_decode(raw, position)
            if not package.get('Standard'):
                require(Path(package['Dir']).is_relative_to(stage), 'client depends on checkout or external module')
                modules.append({'import': package['ImportPath'], 'dir': package['Dir']})
        summary['isolated_service'] = {'result': service, 'nonstandard_packages': modules,
                                       'sdk_dependency': False, 'compiler_dependency': False}
        native_dir = stage / 'native'
        native_dir.mkdir()
        (native_dir / 'go.mod').write_text('module nativecheck\n\ngo 1.22\nrequire example.com/shared-app v0.0.0\nreplace example.com/shared-app => ' + str(APP) + '\n')
        driver = (ROOT / 'scripts/array_native_driver.go.txt').read_text().replace('gosvm/examples/array-values', 'example.com/shared-app/program')
        (output / 'native-driver.go.txt').write_text(driver)
        (native_dir / 'main.go').write_text(driver)
        native = stage / 'native-driver'
        run('native-build', ['go', 'build', '-ldflags=-linkmode=external', '-o', native, '.'], cwd=native_dir)
        run('native-sign', ['codesign', '--force', '--sign', '-', native])
        rng, rows = random.Random(20261005), []

        def add(name, x, y, sequence, amount, minimum):
            state = b''.join(n.to_bytes(8, 'little') for n in (x, y, sequence))
            instruction = b''.join(n.to_bytes(8, 'little') for n in (amount, minimum))
            rows.append({'Name': name, 'State': state.hex(), 'Instruction': instruction.hex()})

        for j in range(128):
            x, y = rng.randrange(1, 500000001), rng.randrange(1, 1000000001)
            add(f'quote-{j:03}', x, y, rng.randrange(1 << 63), rng.randrange(1, 1000000001 - x),
                0 if j % 2 == 0 else rng.randrange(y))
        maximum = (1 << 64) - 1
        for name, vector in (
            ('zero-x', (0, 1, 0, 1, 0)), ('zero-y', (1, 0, 0, 1, 0)), ('zero-input', (1, 1, 0, 0, 0)),
            ('bound-x', (1000000000, 1, 0, 1, 0)), ('over-y', (1, 1000000001, 0, 1, 0)),
            ('max-valid', (999999999, 1000000000, 0, 1, 0)), ('max-sequence', (1, 2, maximum, 1, 0)),
            ('last-sequence', (1, 2, maximum - 1, 1, 0)), ('slippage', (1, 2, 0, 1, maximum)),
            ('max-input', (1, 2, 0, maximum, 0)), ('max-x', (maximum, 1, 0, 1, 0)),
            ('client-wire-vector', (1000000, 2000000, 4, 1000, 1998)),
        ):
            add(name, *vector)
        save(output / 'native-input.json', rows)
        run('native-expectations', [native], input_data=json.dumps(rows).encode())
        expected = load(output / 'native-expectations.log')
        save(output / 'native-expectations.json', expected)
        require(len(rows) == len(expected) and all(not r['Panicked'] for r in expected), 'native fixture panicked/count changed')
        elf = output / 'program.so'
        run('sbf-build', [cli, '-arch', 'v3', '-no-cache', '-llvm', llvm, '-o', elf, APP / 'program'])
        run('emit-c', [cli, '-arch', 'v3', '-emit-c', '-o', output / 'program.c', APP / 'program'])
        run('stack-compile', [llvm / 'bin/clang', '-target', 'sbf', '-mcpu=v3', '-O2', '-fno-builtin', '-fPIC',
            '-fno-stack-protector', '-std=c11', '-Werror', '-fstack-usage', '-c', output / 'program.c', '-o', stage / 'stack.o'])
        run('stack-link', [llvm / 'bin/ld.lld', '-z', 'notext', '-shared', '--Bdynamic', '--strip-all', '--entry',
            'entrypoint', '--script', ROOT / 'internal/compiler/sbf-v3.ld', '--no-undefined', '-o', stage / 'stack.so', stage / 'stack.o'])
        require(digest(elf) == digest(stage / 'stack.so'), 'stack instrumentation differs')
        shutil.copyfile(stage / 'stack.su', output / 'stack-usage.tsv')
        frames = []
        for line in (output / 'stack-usage.tsv').read_text().splitlines():
            function, size, shape = line.split('\t')
            frames.append({'function': function.rsplit(':', 1)[-1], 'bytes': int(size), 'kind': shape})
        require(frames and all(r['kind'] == 'static' and r['bytes'] <= 4096 for r in frames), 'frame budget exceeded')
        program = b58(hashlib.sha256(b'shared-package bounded program; never deploy').digest())
        address = b58(hashlib.sha256(b'shared-package state').digest())

        def state(data):
            return {'data': data, 'owner': program, 'lamports': 10000000, 'executable': False, 'rent_epoch': 0}

        def instruction(data):
            return {'program': 'program', 'accounts': [{'account': 'state', 'signer': False, 'writable': True}], 'data': data}

        cases = []
        for row, result in zip(rows, expected):
            require(row['Name'] == result['Name'] and result['Code'] in (0, 7, 9), 'native ordering/status mismatch')
            error = {'InstructionError': [0, {'Custom': result['Code']}]} if result['Code'] else None
            cases.append({'name': row['Name'], 'overrides': [{'name': 'state', 'state': state(row['State'])}],
                'steps': [{'instructions': [instruction(row['Instruction'])], 'expect': {'error': error,
                    'accounts': [{'account': 'state', 'state': state(row['State'] if error else result['Data'])}]}}]})
        client_row = next(r for r in rows if r['Name'] == 'client-wire-vector')
        require(client_row['State'] == service['Pool'] and client_row['Instruction'] == service['Instruction'], 'client/program wire mismatch')
        for bad_name, code in (('slippage', 9), ('zero-input', 7)):
            bad = next(r for r in rows if r['Name'] == bad_name)
            cases.append({'name': 'successful-swap-then-' + bad_name + '-rollback',
                'overrides': [{'name': 'state', 'state': state(client_row['State'])}],
                'steps': [{'instructions': [instruction(client_row['Instruction']), instruction(bad['Instruction'])],
                    'expect': {'error': {'InstructionError': [1, {'Custom': code}]},
                        'accounts': [{'account': 'state', 'state': state(client_row['State'])}]}}]})
        fixture = output / 'fixtures.json'
        save(fixture, {'format': 'gosvm-svm-fixtures-v1', 'program_id': program,
            'payer_seed': hashlib.sha256(b'shared-program local payer; never fund').hexdigest(),
            'accounts': [{'name': 'state', 'address': address, 'initial': state(bytes(24).hex())}], 'cases': cases})
        for sample in range(3):
            report = output / f'svm-{sample}.json'
            run(f'svm-{sample}', [cli, 'svm-test', '-elf', elf, '-fixtures', fixture, '-report', report])
            actual = load(report)
            require(actual['elf_sha256'] == digest(elf), 'tested artifact changed')
            if sample:
                require(actual['cases'] == load(output / 'svm-0.json')['cases'], 'case/CU/log repetitions changed')
        # Rebuild a copied module to verify actual cache behavior when shared
        # source changes, without mutating the example or historical evidence.
        copied = stage / 'cache-app'
        shutil.copytree(APP, copied)
        cached = stage / 'cache-program.so'
        command = [cli, '-arch', 'v3', '-llvm', llvm, '-o', cached, copied / 'program']
        run('cache-first', command)
        require(digest(cached) == digest(elf), 'relocation changes generated program')
        first, stamp = load(Path(str(cached) + '.gosvm-cache')), cached.stat().st_mtime_ns
        run('cache-noop', command)
        require(cached.stat().st_mtime_ns == stamp, 'unchanged source rebuilt')
        helper = copied / 'math/math.go'
        original = helper.read_text()
        changed = original.replace('return x * y / denominator', 'return x * y / denominator + 1')
        require(changed != original, 'cache mutation not applied')
        helper.write_text(changed)
        run('cache-helper-edit', command)
        edited = load(Path(str(cached) + '.gosvm-cache'))
        require(edited['Input'] != first['Input'] and digest(cached) != digest(elf), 'transitive edit reused stale ELF')
        helper.write_text(original)
        run('cache-restored', command)
        require(digest(cached) == digest(elf), 'restored shared source differs')
        save(output / 'cache-check.json', {'passed': True, 'noop_kept_output_mtime': True,
            'original': first, 'edited': edited, 'restored': load(Path(str(cached) + '.gosvm-cache'))})
        require(hashes == {str(p.relative_to(ROOT)): digest(p) for p in sorted(set(paths))}, 'sources changed during verification')
        summary.update(passed=True, scenarios=len(cases), native_vectors=len(rows), native_c_vectors=1011,
            samples=3, expected_failures=sum(c['steps'][0]['expect']['error'] is not None for c in cases),
            elf_bytes=elf.stat().st_size, elf_sha256=digest(elf), stack_frames=frames,
            runner_sha256=actual['runner_sha256'], runtime_version=actual['runtime_version'])
        print(f'PASS: {len(cases)} shared-package SBF scenarios, three repetitions, isolated native client and dependency cache. {output}', flush=True)
    except Exception as error:
        summary['failure'] = str(error)
        raise
    finally:
        save(output / 'summary.json', summary)


if __name__ == '__main__':
    main()
