#!/usr/bin/env python3
"""Compare a compact signer prototype using copied, frozen benchmark inputs."""
import argparse
import copy
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import time

from checked_token_verify import b58, derive, on_curve

ROOT = Path(__file__).resolve().parents[1]
BENCH = ROOT.parent / 'goSVM-benchmarks'


def sha(p): return hashlib.sha256(p.read_bytes()).hexdigest()
def load(p): return json.loads(p.read_text())
def save(p, v): p.write_text(json.dumps(v, indent=2) + '\n')
def require(ok, message):
    if not ok: raise RuntimeError(message)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', required=True, type=Path)
    args = parser.parse_args()
    out = args.output.resolve()
    stage = ROOT / 'build/single-signer' / out.name
    require(not out.exists() and not stage.exists(), 'choose new directories')
    out.mkdir(parents=True); stage.mkdir(parents=True)
    lock = load(BENCH / 'tools/handler-clock-v1/gosvm.lock.json')
    pin = BENCH / lock['snapshot']
    cli = pin / 'gosvm'
    require(sha(cli) == lock['cli_sha256'], 'frontend pin differs')
    runner = ROOT / 'build/milestone2-bench/2026-10-06-milestone2-bench-supported/cache/runners/0.5.0/darwin-arm64/bin/gosvm-svm-runner'
    runner_pin = load(ROOT / 'internal/runner/pins/darwin-arm64.json')
    require(sha(runner) == runner_pin['files']['bin/gosvm-svm-runner']['sha256'], 'runner pin differs')
    source = BENCH / 'docs/repro/sdk_seed_tick.go.txt'
    sdk = BENCH / 'programs/whirlpool/go-handler/.gosvm/sdk'
    tracked = [source, cli, ROOT / 'scripts/single_signer_prototype.go.txt', ROOT / 'scripts/single_signer_native_test.go.txt', Path(__file__).resolve()]
    tracked += [p for p in sdk.rglob('*') if p.is_file()]
    for name in ('wire', 'math'):
        tracked += [p for p in (BENCH / 'programs/whirlpool/go' / name).glob('*.go') if not p.name.endswith('_test.go')]
    guards = {str(p): sha(p) for p in tracked}
    summary = {'passed': False, 'frontend_source_pin': lock['source_tree_sha256'], 'cli_sha256': sha(cli),
               'runner_sha256': sha(runner), 'input_sha256': guards, 'checks': [],
               'limitations': ['Isolated experimental helper; canonical SDK and benchmark are untouched',
                               'Probe CU differences include dispatch/codegen effects, not full-handler attribution',
                               'Default 200000-CU fixture workflow; the benchmark probe also included a 150-CU budget instruction',
                               'Actual System CPI is checked; full Whirlpools Token/CPI corpus is still required before adoption']}
    env = dict(os.environ, GOCACHE=str(ROOT / 'build/lifecycle-go-cache'), GOSVM_CACHE=str(stage / 'cache'),
               GOWORK='off', GOPROXY='off', GOSUMDB='off', GOTOOLCHAIN='local')
    for name in ('GOFLAGS', 'GOSVM_TEST_RUNNER', 'GOSVM_TEST_FEATURES', 'SBF_LLVM'): env.pop(name, None)

    def run(label, command, cwd=ROOT):
        command = [str(v) for v in command]; start = time.perf_counter()
        with (out / (label + '.log')).open('wb') as log:
            result = subprocess.run(command, cwd=cwd, env=env, stdout=log, stderr=subprocess.STDOUT, timeout=240)
        summary['checks'].append({'label': label, 'command': command, 'cwd': str(cwd),
                                  'exit_code': result.returncode, 'seconds': time.perf_counter()-start})
        require(result.returncode == 0, 'failed ' + label); print(label + ': PASS', flush=True)

    try:
        shared = stage / 'go'; shared.mkdir()
        (shared / 'go.mod').write_text('module example.org/gosvm-benchmarks/whirlpool\n\ngo 1.22\n')
        for name in ('wire', 'math'):
            (shared / name).mkdir()
            for p in (BENCH / 'programs/whirlpool/go' / name).glob('*.go'):
                if not p.name.endswith('_test.go'): shutil.copyfile(p, shared / name / p.name)
        project = stage / 'handler'; project.mkdir()
        (project / 'go.mod').write_text('module example.org/gosvm-benchmarks/whirlpool-handler\n\ngo 1.22\n\nrequire (\n gosvm v0.0.0\n example.org/gosvm-benchmarks/whirlpool v0.0.0\n)\nreplace gosvm => ./.gosvm/sdk\nreplace example.org/gosvm-benchmarks/whirlpool => ../go\n')
        shutil.copytree(sdk, project / '.gosvm/sdk')
        probe = project / 'isolation-profile'; probe.mkdir()
        original = source.read_text(); (probe / 'program.go').write_text(original)
        baseline = out / 'baseline.so'
        llvm = Path.home() / '.cache/solana/v1.51/platform-tools/llvm'
        run('baseline-build', [cli, '-llvm', llvm, '-o', baseline, probe])
        shutil.copyfile(ROOT / 'scripts/single_signer_prototype.go.txt', project / '.gosvm/sdk/sdk/cpi/single.go')
        shutil.copyfile(ROOT / 'scripts/single_signer_native_test.go.txt', project / '.gosvm/sdk/sdk/cpi/single_test.go')
        run('native-builder', ['go', 'test', './sdk/cpi', '-count=1', '-v'], project / '.gosvm/sdk')
        # Keep the original 20-case program body, adding a compact path and CPI branch.
        compact_build = original[original.index('func build('):original.index('func Process(')]
        compact_build = compact_build.replace('func build(', 'func buildCompact(').replace('*pda.Seeds', '*cpi.SingleSigner')
        compact = original.replace('func Process(', compact_build + 'func Process(', 1)
        compact = compact.replace('\td := solana.Instruction(c)\n', '''\td := solana.Instruction(c)
\tif len(d) == 1 && (d[0] == 20 || d[0] == 21 || d[0] == 22) {
\t\tif solana.Count(c) != 4 || len(solana.Data(c, 2)) != 653 { return 7310 }
\t\tvar metas cpi.Metas
\t\tmetas.Add(0, true, true)
\t\tmetas.Add(1, true, false)
\t\tvar data [12]byte
\t\tdata[0] = 2
\t\tdata[4] = 7
\t\tif d[0] == 20 {
\t\t\tvar seeds pda.Seeds
\t\t\tbuild(solana.Data(c, 2), &seeds)
\t\t\tvar signers cpi.Signers
\t\t\tif signers.Add(&seeds) != 0 { return 7311 }
\t\t\treturn cpi.Invoke(c, 3, &metas, data[:], &signers)
\t\t}
\t\tvar signer cpi.SingleSigner
\t\tbuildCompact(solana.Data(c, 2), &signer)
\t\tcode := cpi.InvokeSingle(c, 3, &metas, data[:], &signer)
\t\tif code != 0 { return code }
\t\tif d[0] == 22 { return 7312 }
\t\treturn 0
\t}
''', 1)
        compact = compact.replace('\tif d[0] == 0 {', '''\tif d[0] == 10 {
\t\tvar signer cpi.SingleSigner
\t\tbuildCompact(pool, &signer)
\t\tb := signer.Bytes()
\t\tfor i := uint64(0); i < uint64(len(b)); i++ { out[i] = b[i] }
\t\treturn 0
\t}
\tif d[0] == 11 { var s cpi.SingleSigner; return s.AddBytes(pool[:33]) }
\tif d[0] == 12 {
\t\tvar s cpi.SingleSigner
\t\tfor i := uint64(0); i < 16; i++ { if s.AddBytes(pool[:0]) != 0 { return 7311 } }
\t\treturn s.AddBytes(pool[:0])
\t}
\tif d[0] == 13 { var m cpi.Metas; return cpi.InvokeSingle(c, 0, &m, nil, nil) }
\tif d[0] == 0 {''', 1)
        (probe / 'program.go').write_text(compact)
        shutil.copyfile(probe / 'program.go', out / 'prototype-program.go.txt')
        shutil.copyfile(project / '.gosvm/sdk/sdk/cpi/single.go', out / 'single.go.txt')
        candidate = out / 'prototype.so'
        run('prototype-build', [cli, '-llvm', llvm, '-o', candidate, probe])
        run('prototype-emit', [cli, '-emit-c', '-o', out / 'prototype.c', probe])
        run('stack-compile', [llvm / 'bin/clang', '-target', 'sbf', '-mcpu=v3', '-O2', '-fno-builtin', '-fPIC', '-fno-stack-protector', '-std=c11', '-Werror', '-DGOSVM_SBF_V3=1', '-fstack-usage', '-c', out / 'prototype.c', '-o', stage / 'stack.o'])
        run('stack-link', [llvm / 'bin/ld.lld', '-z', 'notext', '-shared', '--Bdynamic', '--strip-all', '--entry', 'entrypoint', '--script', pin / 'source/internal/compiler/sbf-v3.ld', '--no-undefined', '-o', stage / 'stack.so', stage / 'stack.o'])
        require(sha(candidate) == sha(stage / 'stack.so'), 'stack instrumentation changed ELF')
        shutil.copyfile(stage / 'stack.su', out / 'stack-usage.tsv')
        frames = [line.split('\t') for line in (out / 'stack-usage.tsv').read_text().splitlines()]
        require(frames and all(int(f[1]) <= 4096 and f[2] == 'static' for f in frames), 'frame limit exceeded')
        # Independent bytes/metadata expectations; preserve the benchmark's full old suite.
        pid = bytes([3])*32; program = b58(pid); poolkey = bytes([22])*32
        pool = bytearray((i*29+7)%256 for i in range(653)); pool[:8] = bytes([63,149,209,12,225,128,99,9])
        array = bytearray(9988); array[:8] = bytes([69,97,189,190,110,7,66,187]); array[9956:] = poolkey
        for i in range(88): array[12+i*113] = i%2
        seeds = [b'whirlpool',pool[8:40],pool[101:133],pool[181:213],pool[43:45],pool[40:41]]
        encoded = bytes([1,len(seeds)]) + b''.join(bytes([len(x)])+x for x in seeds)
        require(len(encoded)==116, 'independent encoding length')
        def state(data, owner=program, lamports=1000000000): return {'data': bytes(data).hex(), 'owner': owner, 'lamports': lamports, 'executable': False, 'rent_epoch': 0}
        accounts = [{'name':'output','address':b58(bytes([10])*32),'initial':state(bytes(116))},
                    {'name':'pool','address':b58(poolkey),'initial':state(pool)},
                    {'name':'array','address':b58(bytes([23])*32),'initial':state(array)}]
        cases = []
        def add(name, op, code=0, result=None, mutated=None):
            watch = [{'account':'output','state':state(result if result is not None else bytes(116))}, {'account':'pool','state':state(pool)}, {'account':'array','state':state(array if mutated is None else mutated)}]
            ix = {'program':'program','accounts':[{'account':n,'writable':n=='output','signer':False} for n in ('output','pool','array')], 'data':bytes([op]).hex()}
            case = {'name':name,'steps':[{'instructions':[ix],'expect':{'error':None if code==0 else {'InstructionError':[0,{'Custom':code}]},'accounts':watch}}]}
            if mutated is not None: case['overrides']=[{'name':'array','state':state(mutated)}]
            cases.append(case)
        add('noop',0); add('seed-construction',1,result=encoded); add('seed-plus-signer-copy',2,result=encoded)
        add('validate-once',3,result=bytes([1])+bytes(115)); add('validate-twice',4,result=bytes([1])+bytes(115))
        for name,op,code in [('seed-too-long',5,3001),('seed-count-limit',6,3001),('nil-signer',7,3003),('signer-count-limit',8,3003),('signer-byte-capacity',9,3003)]: add(name,op,code)
        for name,offset,value,code in [('wrong-discriminator',0,0,7000),('wrong-pool',9956,0,6056),('invalid-first-flag',12,2,7000),('invalid-last-flag',12+87*113,2,7000)]:
            bad=bytearray(array); bad[offset]=value
            for op in (3,4): add(name+'-'+str(op),op,code,mutated=bad)
        for op in (3,4): add('short-array-'+str(op),op,7000,mutated=array[:-1])
        baseline_suite={'format':'gosvm-svm-fixtures-v1','program_id':program,'payer_seed':'11'*32,'accounts':accounts,'cases':copy.deepcopy(cases)}
        save(out/'baseline-fixtures.json',baseline_suite)
        add('compact-signer',10,result=encoded); add('compact-long-seed',11,3001); add('compact-seed-limit',12,3001); add('compact-nil-signer',13,3003)
        save(out/'prototype-fixtures.json',{**baseline_suite,'cases':cases})
        # Real PDA-signed System transfers, runtime seed rejection, and rollback.
        seeds_without_bump=[bytes(x) for x in seeds[:-1]]
        address,bump=derive(seeds_without_bump,pid); pool[40]=bump
        system='11111111111111111111111111111111'
        cpi_accounts=[{'name':'source','address':b58(address),'initial':state(b'',system)},
                      {'name':'dest','address':b58(bytes([44])*32),'initial':state(b'',system)},
                      {'name':'pool','address':b58(poolkey),'initial':state(pool)}]
        cpi_cases=[]
        for name,op,code,mutation in [('general-transfer',20,0,None),('compact-transfer',21,0,None),('compact-rollback',22,7312,None),('general-bad-bump',20,'PrivilegeEscalation','bump'),('compact-bad-bump',21,'PrivilegeEscalation','bump'),('general-readonly-source',20,'PrivilegeEscalation','readonly'),('compact-readonly-source',21,'PrivilegeEscalation','readonly')]:
            badpool=bytearray(pool)
            if mutation=='bump':
                badpool[40]=next(b for b in range(256) if b!=bump and not on_curve(hashlib.sha256(b''.join(seeds_without_bump)+bytes([b])+pid+b'ProgramDerivedAddress').digest()))
            watch=[{'account':'source','state':state(b'',system,1000000000-(7 if code==0 else 0))}, {'account':'dest','state':state(b'',system,1000000000+(7 if code==0 else 0))}, {'account':'pool','state':state(badpool)}]
            ix={'program':'program','data':bytes([op]).hex(),'accounts':[{'account':n,'writable':n in ('source','dest') and not (mutation=='readonly' and n=='source'),'signer':False} for n in ('source','dest','pool','system')]}
            error=None if code==0 else {'InstructionError':[0,code if isinstance(code,str) else {'Custom':code}]}
            case={'name':name,'steps':[{'instructions':[ix],'expect':{'error':error,'accounts':watch}}]}
            if mutation=='bump': case['overrides']=[{'name':'pool','state':state(badpool)}]
            cpi_cases.append(case)
        save(out/'cpi-fixtures.json',{'format':'gosvm-svm-fixtures-v1','program_id':program,'payer_seed':'11'*32,'native_programs':[{'name':'system','address':system}],'accounts':cpi_accounts,'cases':cpi_cases})
        for label,elf,fixture in [('baseline',baseline,'baseline-fixtures.json'),('prototype',candidate,'prototype-fixtures.json'),('cpi',candidate,'cpi-fixtures.json')]:
            for sample in range(3):
                report=out/f'{label}-{sample}.json'
                run(f'{label}-{sample}',[cli,'svm-test','-elf',elf,'-fixtures',out/fixture,'-runner',runner,'-report',report])
                actual=load(report)
                require(actual['elf_sha256']==sha(elf) and actual['runner_sha256']==sha(runner),'artifact pin drift')
                if sample: require(actual['cases']==load(out/f'{label}-0.json')['cases'],'unstable cases/CU/logs')
        summary['cu']={label:{c['name']:c['steps'][0]['cu'] for c in load(out/f'{label}-0.json')['cases']} for label in ('baseline','prototype','cpi')}
        summary['elf']={label:{'bytes':p.stat().st_size,'sha256':sha(p)} for label,p in [('baseline',baseline),('prototype',candidate)]}
        summary['max_static_frame']=max(int(f[1]) for f in frames)
        require(guards=={p:sha(Path(p)) for p in guards},'authoritative inputs changed')
        summary['passed']=True; print('PASS: compact-signer investigation '+str(out),flush=True)
    except Exception as error: summary['failure']=str(error); raise
    finally: save(out/'summary.json',summary)


if __name__=='__main__': main()
