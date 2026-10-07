#!/usr/bin/env python3
"""Exercise the 1 KiB scalar-array boundary, freestanding copies and SBF stack."""
import argparse
import hashlib
import json
import platform
import os
from pathlib import Path
import random
import shutil
import subprocess

from array_verify import ROOT, b58, digest, load, require, save


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--llvm', type=Path, default=Path.home() / '.cache/solana/v1.51/platform-tools/llvm')
    args = parser.parse_args()
    output, llvm = args.output.resolve(), args.llvm.resolve()
    stage = ROOT / 'build/array-capacity' / output.name
    require(not output.exists() and not stage.exists(), 'choose a new output/staging name')
    output.mkdir(parents=True)
    stage.mkdir(parents=True)
    env = dict(os.environ, GOCACHE=str(ROOT / 'build/lifecycle-go-cache'), GOSVM_CACHE=str(stage / 'cache'),
               GOPROXY='off', GOWORK='off')
    for name in ('GOSVM_TEST_RUNNER', 'GOSVM_TEST_FEATURES', 'SBF_LLVM', 'GOFLAGS'):
        env.pop(name, None)
    sources = [Path(__file__).resolve(), ROOT / 'scripts/array_verify.py', ROOT / 'scripts/array_native_driver.go.txt',
               ROOT / 'examples/array-capacity/program.go']
    for folder in ('internal/compiler', 'internal/testvm', 'internal/sbftest', 'internal/runner', 'cmd/gosvm'):
        sources.extend(p for p in (ROOT / folder).rglob('*') if p.is_file() and p.suffix != '.md')
    hashes = {str(p.relative_to(ROOT)): digest(p) for p in sorted(set(sources))}
    summary = {'schema': 1, 'passed': False, 'machine': platform.platform(), 'source_sha256': hashes, 'checks': [], 'variants': {},
               'limitations': ['Native-Go/SBF semantics check, not fresh-validator verification',
                               'One SBF entry per scalar kind; measured frames are not bounds for every user function',
                               'Freestanding memory loops are correctness helpers, not tuned CU optimizations']}

    def run(label, command, input_data=None):
        command = [str(x) for x in command]
        with (output / (label + '.log')).open('wb') as log:
            result = subprocess.run(command, cwd=ROOT, env=env, input=input_data, stdout=log,
                                    stderr=subprocess.STDOUT, timeout=180)
        summary['checks'].append({'check': label, 'command': command, 'exit_code': result.returncode})
        require(result.returncode == 0, f'{label}: see {output / (label + ".log")}')
        print(f'{label}: PASS', flush=True)

    try:
        cli = stage / 'gosvm'
        run('cli-build', ['bash', 'scripts/build-cli.sh', cli])
        run('runner-install', [cli, 'runner', 'install', '-archive', ROOT / 'results/svm/2026-10-05-runner-install-complete/gosvm-svm-runner-0.5.0-darwin-arm64.tar.gz'])
        driver = (ROOT / 'scripts/array_native_driver.go.txt').read_text()
        driver = driver.replace('arrays "gosvm/examples/array-values"', 'arrays "gosvm/examples/array-capacity"').replace('arrays.Process(s,i)', 'arrays.Check(s,i)')
        (output / 'native-driver.go.txt').write_text(driver)
        (stage / 'native-driver.go').write_text(driver)
        native = stage / 'native'
        run('native-build', ['go', 'build', '-ldflags=-linkmode=external', '-o', native, stage / 'native-driver.go'])
        run('native-sign', ['codesign', '--force', '--sign', '-', native])
        rng = random.Random(102432)
        total = 0
        for mode, (kind, count, handler) in enumerate((('byte', 1024, 'bytes'), ('uint64', 128, 'words'), ('uint32', 256, 'smallWords'), ('bool', 1024, 'flags'))):
            folder = output / kind
            folder.mkdir()
            contract = stage / kind
            contract.mkdir()
            shutil.copyfile(ROOT / 'examples/array-capacity/program.go', contract / 'program.go')
            entry = f'package arraycapacity\nfunc Process(s,i []byte)uint64{{s[3]=37;{handler}(s,i);return 0}}\n'
            (contract / 'entry.go').write_text(entry)
            (folder / 'entry.go.txt').write_text(entry)
            rows = []
            for index, probe in ((0, 0), (count - 1, count - 1), (0, count - 1), (count - 1, 0), (count // 2, count // 2), (count, 0), (0, count)):
                for value in (0, 255):
                    ix = bytearray(16)
                    ix[0], ix[3], ix[4] = mode, value, 17
                    ix[1:3], ix[5:7] = index.to_bytes(2, 'little'), probe.to_bytes(2, 'little')
                    ix[8:16] = ((1 << 64) - 1 if value else 0).to_bytes(8, 'little')
                    rows.append({'Name': f'index-{index}-probe-{probe}-value-{value}',
                                 'State': bytes(rng.randrange(256) for _ in range(24)).hex(), 'Instruction': ix.hex()})
            save(folder / 'native-input.json', rows)
            run(kind + '-native', [native], json.dumps(rows).encode())
            expected = load(output / (kind + '-native.log'))
            save(folder / 'native-expectations.json', expected)
            elf = folder / 'program.so'
            run(kind + '-build', [cli, '-no-cache', '-arch', 'v3', '-llvm', llvm, '-o', elf, contract])
            run(kind + '-emit-c', [cli, '-arch', 'v3', '-emit-c', '-o', folder / 'program.c', contract])
            obj = contract / 'stack.o'
            run(kind + '-stack', [llvm / 'bin/clang', '-target', 'sbf', '-mcpu=v3', '-O2', '-fno-builtin', '-fPIC',
                '-fno-stack-protector', '-std=c11', '-Werror', '-fstack-usage', '-c', folder / 'program.c', '-o', obj])
            run(kind + '-stack-link', [llvm / 'bin/ld.lld', '-z', 'notext', '-shared', '--Bdynamic', '--strip-all', '--entry',
                'entrypoint', '--script', ROOT / 'internal/compiler/sbf-v3.ld', '--no-undefined', '-o', contract / 'stack.so', obj])
            require(digest(elf) == digest(contract / 'stack.so'), kind + ': instrumented ELF differs')
            shutil.copyfile(contract / 'stack.su', folder / 'stack-usage.tsv')
            stack = []
            for line in (folder / 'stack-usage.tsv').read_text().splitlines():
                function, size, shape = line.split('\t')
                stack.append({'function': function.rsplit(':', 1)[-1], 'bytes': int(size), 'kind': shape})
            require(stack and all(r['kind'] == 'static' and r['bytes'] <= 4096 for r in stack), kind + ': frame budget exceeded')
            program = b58(hashlib.sha256(('array capacity ' + kind).encode()).digest())
            address = b58(hashlib.sha256(('array capacity state ' + kind).encode()).digest())

            def state(data):
                return {'data': data, 'owner': program, 'lamports': 10000000, 'executable': False, 'rent_epoch': 0}

            def instruction(data):
                return {'program': 'program', 'accounts': [{'account': 'state', 'signer': False, 'writable': True}], 'data': data}

            cases = []
            require(len(rows) == len(expected), 'native result count changed')
            for row, result in zip(rows, expected):
                ix = bytes.fromhex(row['Instruction'])
                failed = int.from_bytes(ix[1:3], 'little') >= count or int.from_bytes(ix[5:7], 'little') >= count
                require(row['Name'] == result['Name'] and result['Panicked'] == failed and result['Code'] == 0, 'native boundary outcome changed')
                cases.append({'name': row['Name'], 'overrides': [{'name': 'state', 'state': state(row['State'])}],
                    'steps': [{'instructions': [instruction(row['Instruction'])], 'expect': {
                        'error': {'InstructionError': [0, 'ProgramFailedToComplete']} if failed else None,
                        'accounts': [{'account': 'state', 'state': state(row['State'] if failed else result['Data'])}]}}]})
            cases.append({'name': 'copy-then-failing-index-rollback', 'steps': [{'instructions': [instruction(rows[0]['Instruction']), instruction(rows[-1]['Instruction'])],
                'expect': {'error': {'InstructionError': [1, 'ProgramFailedToComplete']}, 'accounts': [{'account': 'state', 'state': state(bytes(24).hex())}]}}]})
            fixture = folder / 'fixtures.json'
            save(fixture, {'format': 'gosvm-svm-fixtures-v1', 'program_id': program,
                'payer_seed': hashlib.sha256(('capacity local payer ' + kind).encode()).hexdigest(),
                'accounts': [{'name': 'state', 'address': address, 'initial': state(bytes(24).hex())}], 'cases': cases})
            for sample in range(3):
                report = folder / f'svm-{sample}.json'
                run(f'{kind}-svm-{sample}', [cli, 'svm-test', '-elf', elf, '-fixtures', fixture, '-report', report])
                actual = load(report)
                require(actual['elf_sha256'] == digest(elf), 'runtime artifact changed')
                if sample:
                    require(actual['cases'] == load(folder / 'svm-0.json')['cases'], 'repeated cases/CU/logs changed')
            summary['variants'][kind] = {'elements': count, 'value_bytes': 1024, 'native_vectors': len(rows),
                'scenarios': len(cases), 'failures': 5, 'samples': 3, 'elf_bytes': elf.stat().st_size,
                'elf_sha256': digest(elf), 'stack_frames': stack, 'runner_sha256': actual['runner_sha256'], 'runtime_version': actual['runtime_version']}
            total += len(cases)
        require(hashes == {str(p.relative_to(ROOT)): digest(p) for p in sorted(set(sources))}, 'sources changed during verification')
        summary.update(passed=True, scenarios=total, samples=3)
        print(f'PASS: {total} capacity scenarios across four scalar kinds, three real-SBF repetitions. {output}', flush=True)
    except Exception as error:
        summary['failure'] = str(error)
        raise
    finally:
        save(output / 'summary.json', summary)


if __name__ == '__main__':
    main()
