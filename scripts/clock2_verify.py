#!/usr/bin/env python3
"""Prove the SDK-2 Clock accessor through native/C and real SBF fixtures."""
import argparse
import copy
import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import struct
import subprocess
import time
from checked_token_verify import b58

ROOT = Path(__file__).resolve().parents[1]


def digest(path): return hashlib.sha256(path.read_bytes()).hexdigest()
def save(path, value): path.write_text(json.dumps(value, indent=2)+'\n')
def load(path): return json.loads(path.read_text())
def require(value, message):
    if not value: raise RuntimeError(message)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', required=True, type=Path)
    parser.add_argument('--llvm', type=Path, default=Path.home()/'.cache/solana/v1.51/platform-tools/llvm')
    args = parser.parse_args(); out = args.output.resolve(); stage = ROOT/'build/clock2'/out.name
    require(not out.exists() and not stage.exists(), 'choose a new evidence/staging directory')
    out.mkdir(parents=True); stage.mkdir(parents=True)
    paths = [Path(__file__).resolve(), ROOT/'scripts/checked_token_verify.py']
    for folder in ('internal/compiler', 'internal/project', 'cmd/gosvm', 'sdk', 'solana', 'internal/testvm', 'internal/sbftest', 'examples/clock2'):
        paths.extend(p for p in (ROOT/folder).rglob('*') if p.is_file() and p.suffix != '.md' and 'build' not in p.relative_to(ROOT/folder).parts)
    hashes = {str(p.relative_to(ROOT)): digest(p) for p in sorted(set(paths))}
    env = dict(os.environ, GOCACHE=str(ROOT/'build/lifecycle-go-cache'), GOSVM_CACHE=str(stage/'cache'),
               GOPROXY='off', GOSUMDB='off', GOWORK='off', GOTOOLCHAIN='local')
    for name in ('GOFLAGS', 'GOSVM_TEST_RUNNER', 'GOSVM_TEST_FEATURES', 'SBF_LLVM'): env.pop(name, None)
    summary = {'schema': 1, 'passed': False, 'machine': platform.platform(), 'source_sha256': hashes, 'checks': [],
               'limitations': ['SDK Clock boundary and small timestamp-consumer probe, not a Whirlpools handler',
                               'Probe reward-overflow check is uint64; no claim to test Orca U128 reward math',
                               'Pinned local LiteSVM, no new validator or external developer trial',
                               'Signed timestamps cross as two\'s-complement bits; signed Go arithmetic remains unsupported']}

    def run(label, command, cwd=ROOT):
        command = [str(x) for x in command]; start = time.perf_counter()
        with (out/(label+'.log')).open('wb') as log:
            result = subprocess.run(command, cwd=cwd, env=env, stdout=log, stderr=subprocess.STDOUT, timeout=240)
        summary['checks'].append({'check': label, 'command': command, 'exit_code': result.returncode,
                                  'wall_seconds': time.perf_counter()-start, 'log': label+'.log'})
        require(result.returncode == 0, f'{label}: see {out/(label+".log")}')
        print(label+': PASS', flush=True)

    try:
        run('native-c-boundaries', ['go', 'test', './solana', './internal/compiler', '-run', 'TestClock|TestSDK2Clock', '-count=1', '-v'])
        cli = stage/'gosvm'; run('cli-build', ['bash', 'scripts/build-cli.sh', cli])
        run('runner-install', [cli, 'runner', 'install', '-archive', ROOT/'results/svm/2026-10-05-runner-install-complete/gosvm-svm-runner-0.5.0-darwin-arm64.tar.gz'])
        project = stage/'sdk-two'
        run('sdk2-scaffold', [cli, 'new', '-schema', '2', '-sdk', '2', project])
        run('sdk2-check', [cli, 'check', '-dir', project]); run('sdk2-native', [cli, 'test', '-dir', project])
        probe = project/'probe'; probe.mkdir(); shutil.copyfile(ROOT/'examples/clock2/program.go', probe/'program.go')
        elf = out/'program.so'
        run('sbf-build', [cli, '-arch', 'v3', '-llvm', args.llvm, '-o', elf, probe])
        run('emit-c', [cli, '-arch', 'v3', '-emit-c', '-o', out/'program.c', probe])
        run('stack-compile', [args.llvm/'bin/clang', '-target', 'sbf', '-mcpu=v3', '-O2', '-fno-builtin', '-fPIC',
                             '-fno-stack-protector', '-std=c11', '-Werror', '-DGOSVM_SBF_V3=1', '-fstack-usage',
                             '-c', out/'program.c', '-o', stage/'stack.o'])
        run('stack-link', [args.llvm/'bin/ld.lld', '-z', 'notext', '-shared', '--Bdynamic', '--strip-all', '--entry', 'entrypoint',
                          '--script', ROOT/'internal/compiler/sbf-v3.ld', '--no-undefined', '-o', stage/'stack.so', stage/'stack.o'])
        require(digest(elf) == digest(stage/'stack.so'), 'instrumented link changed ELF')
        shutil.copyfile(stage/'stack.su', out/'stack-usage.tsv'); frames = []
        for line in (out/'stack-usage.tsv').read_text().splitlines():
            name, size, kind = line.split('\t'); frames.append({'function': name.rsplit(':', 1)[-1], 'bytes': int(size), 'kind': kind})
        require(frames and all(f['kind'] == 'static' and f['bytes'] <= 4096 for f in frames), 'unsafe frame')
        pid = b58(hashlib.sha256(b'Clock SDK fixture; never deploy').digest())
        address = b58(hashlib.sha256(b'Clock SDK state; never fund').digest())
        clock = {'slot': 9007199254740993, 'epoch_start_timestamp': -23, 'epoch': 7, 'leader_schedule_epoch': 9, 'unix_timestamp': 1000}
        def state(data): return {'data': data.hex(), 'owner': pid, 'lamports': 10000000, 'executable': False, 'rent_epoch': 0}
        original = bytes(56); cases = []
        def step(before, current, mode=0, rate=7, code=0):
            after = before
            if code == 0:
                previous = struct.unpack_from('<Q', before, 40)[0]; delta = current['unix_timestamp']-previous
                growth = delta*rate; growth = 0 if growth > (1<<64)-1 else growth
                after = struct.pack('<QqQQqQQ', current['slot'], current['epoch_start_timestamp'], current['epoch'],
                                    current['leader_schedule_epoch'], current['unix_timestamp'], current['unix_timestamp'], growth)
            ix = {'program': 'program', 'accounts': [{'account': 'state', 'writable': True, 'signer': False}],
                  'data': struct.pack('<BQ', mode, rate).hex()}
            return {'sysvars': {'clock': current}, 'instructions': [ix], 'expect': {
                'error': None if code == 0 else {'InstructionError': [0, {'Custom': code}]},
                'accounts': [{'account': 'state', 'state': state(after)}]}}, after
        def single(name, current, mode=0, rate=7, code=0, before=original):
            s, _ = step(before, current, mode, rate, code); case = {'name': name, 'steps': [s]}
            if before != original: case['overrides'] = [{'name': 'state', 'state': state(before)}]
            cases.append(case)
        before = original; steps = []
        for timestamp in (1000, 1010, 1010, 1020):
            current = dict(clock, unix_timestamp=timestamp); s, before = step(before, current); steps.append(s)
            if len(steps) > 1: s['fresh_blockhash'] = True
        cases.append({'name': 'actual-clock-reward-sequence', 'steps': steps})
        single('negative-timestamp-minus-one', dict(clock, unix_timestamp=-1), code=6021)
        single('negative-timestamp-min-int64', dict(clock, unix_timestamp=-(1<<63)), code=6021)
        single('maximum-positive-timestamp', dict(clock, unix_timestamp=(1<<63)-1), rate=1)
        single('zero-timestamp', dict(clock, unix_timestamp=0))
        single('word-extremes', dict(clock, slot=(1<<64)-1, epoch_start_timestamp=-(1<<63), epoch=(1<<64)-1,
                                   leader_schedule_epoch=(1<<64)-1, unix_timestamp=1000))
        previous = bytes(40)+struct.pack('<QQ', 1010, 98765)
        single('time-regression', dict(clock, unix_timestamp=1000), code=6022, before=previous)
        single('uint64-reward-multiply-overflow', clock, rate=(1<<64)-1)
        single('read-then-fail-rolls-back', clock, mode=1, code=6099)
        single('output-length-39', clock, mode=2, code=2016)
        single('output-length-41', clock, mode=3, code=2016)
        first, _ = step(original, clock)
        second, _ = step(original, dict(clock, unix_timestamp=1001), mode=1, code=6099)
        first['instructions'] += second['instructions']
        first['expect'] = {'error': {'InstructionError': [1, {'Custom': 6099}]}, 'accounts': [{'account': 'state', 'state': state(original)}]}
        cases.append({'name': 'atomic-later-instruction-rollback', 'steps': [first]})
        suite = {'format': 'gosvm-svm-fixtures-v1', 'program_id': pid,
                 'payer_seed': hashlib.sha256(b'Clock SDK fee payer; never fund').hexdigest(), 'sysvars': {'clock': clock},
                 'accounts': [{'name': 'state', 'address': address, 'initial': state(original)}], 'cases': cases}
        fixtures = out/'fixtures.json'; save(fixtures, suite)
        reports = []
        for i in range(3):
            report = out/f'svm-{i}.json'; run(f'svm-{i}', [cli, 'svm-test', '-elf', elf, '-fixtures', fixtures, '-report', report])
            actual = load(report); require(actual['elf_sha256'] == digest(elf), 'another ELF executed'); reports.append(actual)
        require(all(r['cases'] == reports[0]['cases'] for r in reports), 'case/CU/log drift')
        require(hashes == {p: digest(ROOT/p) for p in hashes}, 'sources changed during proof')
        summary.update(passed=True, scenarios=len(cases), transactions=sum(len(c['steps']) for c in cases), samples=3,
                       native_c_vectors=1010, elf_bytes=elf.stat().st_size, elf_sha256=digest(elf), stack_frames=frames,
                       runner_sha256=reports[0]['runner_sha256'], runtime_version=reports[0]['runtime_version'], fixture_sha256=digest(fixtures))
        print(f'PASS: Clock accessor, {len(cases)} SBF scenarios × three. {out}', flush=True)
    except Exception as error:
        summary['failure'] = str(error); raise
    finally: save(out/'summary.json', summary)


if __name__ == '__main__': main()
