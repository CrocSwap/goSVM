#!/usr/bin/env python3
"""Check canonical packed stores, aborts and transaction rollback in real SBF."""
import argparse
import copy
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess

ROOT = Path(__file__).resolve().parents[1]
BENCH = ROOT.parent / 'goSVM-benchmarks'
PIN = ROOT / 'results/compiler/2026-10-06-packed-store-canonical-final'
TOOLS = ROOT / 'build/packed-store/2026-10-06-packed-store-canonical-final'
SOURCE = '''package p
import "gosvm/solana"
func Store64(b []byte,o,v uint64){for j:=uint64(0);j<8;j++{b[o+j]=byte(v>>(8*j))}}
func Store32(b []byte,o uint64,v uint32){for j:=uint64(0);j<4;j++{b[o+j]=byte(v>>(8*j))}}
func read(b []byte,o uint64)uint64{v:=uint64(0);for j:=uint64(0);j<8;j++{v=v|uint64(b[o+j])<<(8*j)};return v}
func Process(c solana.Context)uint64{
 d:=solana.Instruction(c);if solana.Count(c)!=1||len(d)!=17{return 7330}
 b:=solana.Data(c,0);o,v:=read(d,1),read(d,9)
 if d[0]&1==0{Store64(b,o,v)}else{Store32(b,o,uint32(v))}
 if d[0]>1{return 7331};return 0
}
'''


def sha(p):
    return hashlib.sha256(p.read_bytes()).hexdigest()


def b58(data):
    alphabet, n, result = '123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz', int.from_bytes(data, 'big'), ''
    while n:
        n, r = divmod(n, 58)
        result = alphabet[r] + result
    return '1' * (len(data) - len(data.lstrip(b'\0'))) + result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', required=True, type=Path)
    args = parser.parse_args()
    out = args.output.resolve()
    stage = ROOT / 'build/packed-store-probe' / out.name
    assert out.is_relative_to(ROOT / 'results') and not out.exists() and not stage.exists()
    out.mkdir(parents=True)
    stage.mkdir(parents=True)
    cli = TOOLS / 'gosvm'
    pin = json.loads((PIN / 'summary.json').read_text())
    assert pin['passed'] and sha(cli) == pin['cli_sha256']
    summary = dict(passed=False, checks=[], cli_sha256=sha(cli), script_sha256=sha(Path(__file__)))
    env = dict(os.environ, GOCACHE=str(ROOT / 'build/lifecycle-go-cache'), GOWORK='off', GOPROXY='off', GOTOOLCHAIN='local', PYTHONDONTWRITEBYTECODE='1')

    def run(name, command):
        with (out / (name + '.log')).open('wb') as log:
            p = subprocess.run([str(x) for x in command], cwd=ROOT, env=env, stdout=log, stderr=subprocess.STDOUT)
        summary['checks'].append(dict(name=name, command=[str(x) for x in command], exit_code=p.returncode))
        assert p.returncode == 0, name
        print(name + ': PASS', flush=True)

    try:
        shutil.copytree(TOOLS / 'scaffold/.gosvm', stage / '.gosvm')
        (stage / 'go.mod').write_text('module example.org/packed-store-probe\n\ngo 1.22\n\nrequire gosvm v0.0.0\nreplace gosvm => ./.gosvm/sdk\n')
        (stage / 'program.go').write_text(SOURCE)
        (out / 'program.go.txt').write_text(SOURCE)
        elf = out / 'program.so'
        run('sbf-build', [cli, '-no-cache', '-llvm', Path.home() / '.cache/solana/v1.51/platform-tools/llvm', '-o', elf, stage])
        run('emit-c', [cli, '-emit-c', '-o', out / 'program.c', stage])
        assert (out / 'program.c').read_text().count('__builtin_memcpy(') == 2
        program, address = b58(bytes([73]) * 32), b58(bytes([74]) * 32)
        def state(data):
            return dict(data=data.hex(), owner=program, lamports=1000000000, executable=False, rent_epoch=0)
        cases = []
        for op in range(4):
            width = 8 if op % 2 == 0 else 4
            for length in (0, 1, 3, 4, 7, 8, 15, 16, 65):
                for offset in sorted(set(list(range(8)) + [max(0, length-width), length, (1 << 64)-1, (1 << 64)-7, 1 << 63])):
                    data = bytearray((j*37+11) & 255 for j in range(length))
                    initial = state(data)
                    value = 0xfedcba9876543210
                    valid = offset <= length and width <= length-offset
                    error = None if valid and op < 2 else {'InstructionError': [0, {'Custom': 7331} if valid else 'ProgramFailedToComplete']}
                    if error is None:
                        data[offset:offset+width] = value.to_bytes(8, 'little')[:width]
                    cases.append(dict(name=f'op{op}-len{length}-offset{offset}', overrides=[dict(name='state', state=initial)], steps=[dict(instructions=[dict(program='program', data=(bytes([op])+offset.to_bytes(8, 'little')+value.to_bytes(8, 'little')).hex(), accounts=[dict(account='state', writable=True, signer=False)])], expect=dict(error=error, accounts=[dict(account='state', state=state(data))]))]))
        fixture = out / 'fixtures.json'
        fixture.write_text(json.dumps(dict(format='gosvm-svm-fixtures-v1', program_id=program, payer_seed='11'*32, accounts=[dict(name='state', address=address, initial=state(bytearray(16)))], cases=cases), indent=2) + '\n')
        verified = BENCH / 'build/verification/2026-10-06-fee-growth-handler'
        runner = next((verified / 'cache').rglob('gosvm-svm-runner'))
        first = None
        for i in range(3):
            report = out / ('sbf-' + str(i) + '.json')
            run('sbf-' + str(i), ['python3', BENCH / 'scripts/run_budget_suite.py', '--runner', runner, '--maker', verified / 'transaction-maker', '--elf', elf, '--features', ROOT / 'internal/testvm/profiles/validator-3.0.15.json', '--fixtures', fixture, '--report', report])
            actual = json.loads(report.read_text())
            assert actual['passed']
            if first is not None:
                assert actual['cases'] == first['cases']
            first = actual
        summary.update(passed=True, scenarios=len(cases), repetitions=3, elf_sha256=sha(elf), elf_bytes=elf.stat().st_size, fixture_sha256=sha(fixture), runtime_version=first['runtime_version'], runner_sha256=first['runner_sha256'], successful=sum(c['steps'][0]['expect']['error'] is None for c in cases), failures=sum(c['steps'][0]['expect']['error'] is not None for c in cases))
    finally:
        (out / 'summary.json').write_text(json.dumps(summary, indent=2) + '\n')


if __name__ == '__main__':
    main()
