#!/usr/bin/env python3
"""Controlled lowered-C experiments; does not modify the compiler or benchmark.

Requires the preserved canonical compact-handler rebuild and sibling fixture tools.
The fixed emitted symbols/signatures deliberately bind this experiment to that pin.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import random
import re
import statistics
import subprocess
import time

ROOT = Path(__file__).resolve().parents[1]
BENCH = ROOT.parent / 'goSVM-benchmarks'
REFERENCE = ROOT / 'results/compiler/2026-10-06-compact-signer-canonical-final'
STAGE = ROOT / 'build/compact-signer-canonical/2026-10-06-compact-signer-canonical-final'
SIGNATURES = {'v222': 'static u64 v222(slice v220,u64 v221)',
              'v225': 'static u32 v225(slice v223,u64 v224)',
              'v238': 'static void v238(slice v235,u64 v236,u64 v237)',
              'v242': 'static void v242(slice v239,u64 v240,u32 v241)'}


def sha(p):
    return hashlib.sha256(p.read_bytes()).hexdigest()


def body(source, name):
    start = source.index(SIGNATURES[name] + '{')
    end, depth = start + len(SIGNATURES[name]), 0
    for i in range(end, len(source)):
        depth += (source[i] == '{') - (source[i] == '}')
        if depth == 0:
            return source[start:i + 1]
    raise ValueError(name)


def transform(source, reads=False, writes=False, comparisons=False):
    for name, b, o, value, width in [('v222', 'v220', 'v221', None, 8), ('v225', 'v223', 'v224', None, 4),
                                    ('v238', 'v235', 'v236', 'v237', 8), ('v242', 'v239', 'v240', 'v241', 4)]:
        if not (reads if value is None else writes):
            continue
        old = body(source, name)
        # Valid fast path; fall back to the exact original body for partial-write
        # panics and offset wraparound. Builtin memcpy permits unaligned access.
        operation = f'u{width * 8} word; __builtin_memcpy(&word,{b}.ptr+{o},{width}); return word;' if value is None else f'__builtin_memcpy({b}.ptr+{o},&{value},{width}); return;'
        fast = f'\n#if __BYTE_ORDER__ == __ORDER_LITTLE_ENDIAN__\nif ({o}<=(u64){b}.len && {width}ULL<=(u64){b}.len-{o}) {{ {operation} }}\n#endif\n'
        source = source.replace(old, SIGNATURES[name] + '{' + fast + old[len(SIGNATURES[name]) + 1:])
    if comparisons:
        old = 'static inline boolean array4_equal(array4 a,array4 b) {\nfor (u64 i=0;i<32ULL;i++) { if (a.items[i] != b.items[i]) return 0; }\nreturn 1;\n}'
        assert old in source
        source = source.replace(old, old.replace('array4 a,array4 b', 'const array4 *a,const array4 *b').replace('a.items', 'a->items').replace('b.items', 'b->items'))
        source = re.sub(r'array4_equal\((t\d+),(t\d+)\)', r'array4_equal(&\1,&\2)', source)
    return source


def native_vectors():
    rng = random.Random(20261006)
    lines, expected = [], []
    for trial in range(12000):
        op, alignment, length = trial % 4, trial % 8, rng.randrange(65)
        offset = rng.choice([rng.randrange(80), (1 << 64) - 1, (1 << 64) - 7, (1 << 64) - 8, 1 << 63, 0, max(0, length - 8)])
        value = rng.getrandbits(64)
        data = bytearray(rng.getrandbits(8) for _ in range(80))
        lines.append(f'{op} {alignment} {length} {offset} {value} {data.hex()}\n')
        width = 8 if op in (0, 2) else 4
        result, panic = 0, False
        for j in range(width):
            index = (offset + j) % (1 << 64)
            if index >= length:
                panic = True
                break
            if op < 2:
                result |= data[alignment + index] << (8 * j)
            else:
                data[alignment + index] = (value >> (8 * j)) & 255
        expected.append(f'{"P" if panic else "R" if op < 2 else "W"}:{result if not panic and op < 2 else 0}:{data.hex()}\n')
    return ''.join(lines), ''.join(expected)


NATIVE_DRIVER = r'''
#include <stdio.h>
#include <stdlib.h>
#include <setjmp.h>
typedef unsigned char u8;typedef unsigned int u32;typedef unsigned long long u64;
typedef long long i64;typedef _Bool boolean;typedef struct {u8 *ptr;i64 len,cap;} slice;
static jmp_buf jump;static u8 data[80];static int panic;static u64 result;
void abort(void){panic=1;longjmp(jump,1);}
static u8 *at(slice s,u64 i){if(i>=(u64)s.len)abort();return s.ptr+i;}
FUNCTIONS
int main(void){unsigned op,alignment;u64 length,offset,value;char hex[161];
while(scanf("%u %u %llu %llu %llu %160s",&op,&alignment,&length,&offset,&value,hex)==6){
for(int j=0;j<80;j++){unsigned x;sscanf(hex+2*j,"%2x",&x);data[j]=(u8)x;}
panic=0;result=0;
if(!setjmp(jump)){slice b={data+alignment,(i64)length,(i64)length};
switch(op){case 0:result=v222(b,offset);break;case 1:result=v225(b,offset);break;case 2:v238(b,offset,value);break;case 3:v242(b,offset,(u32)value);break;}}
printf("%c:%llu:",panic?'P':op<2?'R':'W',panic?0:result);
for(int j=0;j<80;j++)printf("%02x",data[j]);puts("");}return 0;}
'''


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', required=True, type=Path)
    args = parser.parse_args()
    out = args.output.resolve()
    assert out.is_relative_to(ROOT / 'results') and not out.exists()
    out.mkdir(parents=True)
    summary = dict(passed=False, scope='Lowered-C diagnostics on compact handler; compiler/SDK/benchmark unchanged', checks=[], variants={})
    env = dict(os.environ, GOCACHE=str(ROOT / 'build/lifecycle-go-cache'), GOWORK='off', GOTOOLCHAIN='local', PYTHONDONTWRITEBYTECODE='1')

    def run(name, command, input=None):
        start = time.perf_counter()
        p = subprocess.run([str(v) for v in command], cwd=ROOT, env=env, input=input, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        (out / (name + '.log')).write_text(p.stdout)
        summary['checks'].append(dict(name=name, command=[str(v) for v in command], exit_code=p.returncode, seconds=time.perf_counter() - start))
        (out / 'summary.json').write_text(json.dumps(summary, indent=2) + '\n')
        assert p.returncode == 0, name + ': see log'
        print(name + ': PASS', flush=True)
        return p.stdout

    try:
        cli, llvm = STAGE / 'gosvm', Path.home() / '.cache/solana/v1.51/platform-tools/llvm/bin'
        proof = json.loads((REFERENCE / 'summary.json').read_text())
        assert proof['passed'] and sha(cli) == proof['cli_sha256']
        run('emit-c', [cli, '-emit-c', '-o', out / 'baseline.c', STAGE / 'handler'])
        original = (out / 'baseline.c').read_text()
        summary['generated_c_sha256'] = sha(out / 'baseline.c')
        summary['input_sha256'] = {str(p): sha(p) for p in [cli, Path(__file__), STAGE / 'handler/program.go', STAGE / 'go/wire/wire.go', REFERENCE / 'handler.json', REFERENCE / 'canonical-0.json', BENCH / 'scripts/run_budget_suite.py', ROOT / 'internal/testvm/profiles/validator-3.0.15.json']}
        summary['native_vectors'] = 12000
        vectors, want = native_vectors()
        (out / 'native-input.txt').write_text(vectors)
        (out / 'native-expected.txt').write_text(want)
        reference = json.loads((REFERENCE / 'canonical-0.json').read_text())
        rust = json.loads((BENCH / 'results/whirlpool/2026-10-06-compact-signer-v1-verified/rust-0.json').read_text())
        fixture = json.loads((REFERENCE / 'handler.json').read_text())
        verified = BENCH / 'build/verification/2026-10-06-fee-growth-handler'
        runner = next((verified / 'cache').rglob('gosvm-svm-runner'))
        program_prefix = 'Program ' + fixture['program_id'] + ' consumed '
        def normalized_logs(logs):
            # Caller optimizations change both its consumed CU and the remaining
            # budget handed to Token. Preserve Token's actual consumed CU.
            return [re.sub(r' of \d+ compute units$', ' of <remaining> compute units', x)
                    for x in logs if not x.startswith(program_prefix)]
        for name, flags in [('baseline', {}), ('read-words', dict(reads=True)), ('write-words', dict(writes=True)), ('compare-pointers', dict(comparisons=True)), ('combined', dict(reads=True, writes=True, comparisons=True))]:
            c = transform(original, **flags)
            file, obj, elf = out / (name + '.c'), out / (name + '.o'), out / (name + '.so')
            file.write_text(c)
            functions = '\n'.join(body(c, f) for f in SIGNATURES)
            host_source, host = out / (name + '-native.c'), out / (name + '-native')
            host_source.write_text(NATIVE_DRIVER.replace('FUNCTIONS', functions))
            run(name + '-native-build', ['clang', '-std=c11', '-O2', '-fsanitize=undefined', '-fno-sanitize-recover=all', host_source, '-o', host])
            assert run(name + '-native', [host], vectors) == want, 'native output/panic/partial-write mismatch'
            run(name + '-compile', [llvm / 'clang', '-target', 'sbf', '-mcpu=v3', '-O2', '-fno-builtin', '-fPIC', '-fno-stack-protector', '-std=c11', '-Werror', '-fstack-usage', '-c', file, '-o', obj])
            run(name + '-link', [llvm / 'ld.lld', '-z', 'notext', '-shared', '--Bdynamic', '--strip-all', '--entry', 'entrypoint', '--script', ROOT / 'internal/compiler/sbf-v3.ld', '--no-undefined', '-o', elf, obj])
            if name == 'baseline':
                assert sha(elf) == proof['elf_sha256'], 'baseline ELF differs'
            reports = []
            for i in range(3):
                report = out / (name + '-' + str(i) + '.json')
                run(name + '-sbf-' + str(i), ['python3', BENCH / 'scripts/run_budget_suite.py', '--runner', runner, '--maker', verified / 'transaction-maker', '--elf', elf, '--features', ROOT / 'internal/testvm/profiles/validator-3.0.15.json', '--fixtures', REFERENCE / 'handler.json', '--report', report])
                actual = json.loads(report.read_text())
                assert actual['passed'] and len(actual['cases']) == len(reference['cases'])
                for key in ('fixtures_sha256', 'runner_sha256', 'transaction_maker_sha256', 'features', 'runtime_version', 'compute_unit_limit'):
                    assert actual[key] == reference[key], key
                for a, b in zip(actual['cases'], reference['cases']):
                    assert a['name'] == b['name'] and len(a['steps']) == len(b['steps'])
                    for s, t in zip(a['steps'], b['steps']):
                        assert s['error'] == t['error'] and s['committed'] == t['committed']
                        assert normalized_logs(s['logs']) == normalized_logs(t['logs'])
                if reports:
                    assert actual['cases'] == reports[0]['cases']
                reports.append(actual)
            ratios, deltas, failures = [], [], []
            for a, b, r in zip(reports[0]['cases'], reference['cases'], rust['cases']):
                for s, t, u in zip(a['steps'], b['steps'], r['steps']):
                    if s['error'] is None:
                        ratios.append(s['cu'] / u['cu']); deltas.append(t['cu'] - s['cu'])
                    else:
                        failures.append(t['cu'] - s['cu'])
            frames = [int(line.split('\t')[1]) for line in file.with_suffix('.su').read_text().splitlines()]
            summary['variants'][name] = dict(elf_bytes=elf.stat().st_size, elf_sha256=sha(elf), max_static_frame=max(frames), median_paired_ratio=statistics.median(ratios), median_success_saving=statistics.median(deltas), success_improve=sum(x>0 for x in deltas), success_regress=sum(x<0 for x in deltas), failure_improve=sum(x>0 for x in failures), failure_regress=sum(x<0 for x in failures))
        assert all(sha(Path(p)) == h for p, h in summary['input_sha256'].items()), 'input drift'
        summary['passed'] = True
    finally:
        (out / 'summary.json').write_text(json.dumps(summary, indent=2) + '\n')


if __name__ == '__main__':
    main()
