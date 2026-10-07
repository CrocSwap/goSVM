#!/usr/bin/env python3
"""Prove SDK-2 current rent, creation, owned resizing/close/reuse in real SBF."""
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

from checked_token_verify import b58, derive, on_curve

ROOT = Path(__file__).resolve().parents[1]
MAX = (1 << 64) - 1
SYSTEM = b58(bytes(32))


def digest(p): return hashlib.sha256(p.read_bytes()).hexdigest()
def key(label): return hashlib.sha256(label.encode()).digest()
def save(p, value): p.write_text(json.dumps(value, indent=2) + '\n')
def load(p): return json.loads(p.read_text())
def require(ok, message):
    if not ok: raise RuntimeError(message)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', required=True, type=Path)
    parser.add_argument('--llvm', type=Path, default=Path.home()/'.cache/solana/v1.51/platform-tools/llvm')
    args = parser.parse_args()
    output = args.output.resolve()
    stage = ROOT/'build/lifecycle2'/output.name
    require(not output.exists() and not stage.exists(), 'choose a new evidence/staging directory')
    output.mkdir(parents=True); stage.mkdir(parents=True)
    env = dict(os.environ, GOCACHE=str(ROOT/'build/lifecycle-go-cache'), GOSVM_CACHE=str(stage/'cache'),
               GOPROXY='off', GOSUMDB='off', GOWORK='off')
    for name in ('GOFLAGS','SBF_LLVM','GOSVM_TEST_RUNNER','GOSVM_TEST_FEATURES'): env.pop(name, None)
    paths = [Path(__file__).resolve(), ROOT/'scripts/lifecycle2_key.go.txt', ROOT/'scripts/checked_token_verify.py']
    for folder in ('internal/compiler','internal/project','internal/runner','internal/testvm','internal/sbftest','cmd/gosvm','sdk','solana','examples/lifecycle2'):
        paths.extend(p for p in (ROOT/folder).rglob('*') if p.is_file() and p.suffix != '.md')
    hashes = {str(p.relative_to(ROOT)):digest(p) for p in sorted(set(paths))}
    summary = {'schema':1, 'passed':False, 'machine':platform.platform(), 'source_sha256':hashes, 'checks':[],
               'limitations':['SDK boundary fixture; generated initialization/close policy and full applications remain open',
                              'Native/C differential tests and LiteSVM SBF; no new validator oracle',
                              'Rent arithmetic rejects overflowing integer product, negative/nonfinite thresholds',
                              'Not a Whirlpool port, performance comparison or external developer trial']}

    def run(label, command):
        command = [str(x) for x in command]; start = time.perf_counter()
        with (output/(label+'.log')).open('wb') as log:
            result = subprocess.run(command, cwd=ROOT, env=env, stdout=log, stderr=subprocess.STDOUT, timeout=240)
        summary['checks'].append({'check':label, 'command':command, 'exit_code':result.returncode,
                                  'wall_seconds':time.perf_counter()-start, 'log':label+'.log'})
        require(result.returncode == 0, f'{label}: see {output/(label+".log")}')
        print(label+': PASS', flush=True)

    try:
        run('native-boundaries', ['go','test','./solana','./sdk/system','-run','TestOwned|TestRent|TestSystem','-count=1','-v'])
        run('c-boundaries', ['go','test','./internal/compiler','-run','TestSDK2OwnedLifecycle|TestSDK2RentCalculation','-count=1','-v'])
        cli = stage/'gosvm'
        run('cli-build',['bash','scripts/build-cli.sh',cli])
        run('runner-install',[cli,'runner','install','-archive',ROOT/'results/svm/2026-10-05-runner-install-complete/gosvm-svm-runner-0.5.0-darwin-arm64.tar.gz'])
        project = stage/'sdk-two'
        run('sdk2-scaffold',[cli,'new','-schema','2','-sdk','2',project])
        run('sdk2-check',[cli,'check','-dir',project])
        run('sdk2-native',[cli,'test','-dir',project])
        probe = project/'probe'; probe.mkdir()
        shutil.copyfile(ROOT/'examples/lifecycle2/program.go',probe/'program.go')
        native = stage/'keys'; source = stage/'keys.go'
        shutil.copyfile(ROOT/'scripts/lifecycle2_key.go.txt',source)
        run('fixture-keys-build',['go','build','-o',native,source])
        wallet_seed, signed_seed = key('lifecycle2 wallet; never fund').hex(), key('lifecycle2 target; never fund').hex()
        run('fixture-keys',[native,wallet_seed,signed_seed])
        wallet, signed = [bytes.fromhex(x) for x in (output/'fixture-keys.log').read_text().splitlines()]
        program_bytes = key('SDK2 lifecycle probe; never deploy'); program = b58(program_bytes)
        pda, bump = derive([b'',bytes([17])],program_bytes)
        wrong_bump = next(b for b in range(255,-1,-1) if b != bump and not on_curve(
            hashlib.sha256(bytes([17,b])+program_bytes+b'ProgramDerivedAddress').digest()))
        names = {'result':key('SDK2 lifecycle result'), 'wallet':wallet, 'signed':signed, 'pda':pda,
                 'owned':key('SDK2 lifecycle owned'), 'system':bytes(32)}
        rent_default = {'lamports_per_byte_year':3480, 'exemption_threshold':2.0, 'burn_percent':50}

        def minimum(size, rent=rent_default):
            n = (128+size)*rent['lamports_per_byte_year']
            if size > 10485760 or n > MAX: return 0,3020
            return min(MAX,int(float(n)*rent['exemption_threshold'])),0

        def state(data=b'', owner=SYSTEM, lamports=100000000):
            return {'data':data.hex(), 'owner':owner, 'lamports':lamports, 'executable':False, 'rent_epoch':0}

        initial = {'result':state(bytes(56),program,10000000), 'wallet':state(),
                   'owned':state(bytes(range(1,33)),program), 'signed':None, 'pda':None}
        cases = []

        def instruction(op,target,length=0,amount=0,failed=False,sign=True,readonly=False,b=bump):
            return {'program':'program', 'data':struct.pack('<BQQBB',op,length,amount,b,1 if failed else 0).hex(),
                    'accounts':[{'account':'result','writable':True,'signer':False},
                                {'account':target,'writable':not readonly,'signer':target=='signed' and op in (1,8)},
                                {'account':'wallet','writable':True,'signer':sign},
                                {'account':'system','writable':False,'signer':False},
                                {'account':target,'writable':not readonly,'signer':False}]}

        def step(before,op,target='owned',length=0,amount=0,*,code=0,failed=False,sign=True,readonly=False,
                 rent=rent_default,controls=None,b=bump,fresh=False):
            after = copy.deepcopy(before); target_before = before[target]
            minimum_amount, minimum_code = minimum(length,rent)
            if code == 0:
                if op in (1,2,8):
                    if op == 8:
                        after['wallet']['lamports'] += target_before['lamports']
                    after[target] = state(bytes(length),program,max(1,minimum_amount))
                    after['wallet']['lamports'] -= max(1,minimum_amount)
                elif op in (3,4):
                    data = bytes.fromhex(target_before['data'])
                    if op == 4: data = data[:amount] + bytes(max(0,amount-len(data)))
                    after[target]['data'] = (data[:length]+bytes(max(0,length-len(data)))).hex()
                elif op == 5:
                    after['wallet']['lamports'] += target_before['lamports'];after[target] = None
                elif op == 7:
                    after[target]['lamports'] += amount;after['wallet']['lamports'] -= amount
                t = after[target]
                data = struct.pack('<7Q',minimum_amount,target_before['lamports'] if target_before else 0,
                                   t['lamports'] if t else 0,len(bytes.fromhex(t['data'])) if t else 0,
                                   len(bytes.fromhex(t['data'])) if t else 0,after['wallet']['lamports'],amount)
                after['result']['data'] = data.hex()
            expectations = []
            for n in ['result','wallet',target]:
                expectations.append({'account':n,'state':after[n]} if after[n] is not None else {'account':n,'absent':True})
            value = {'instructions':[instruction(op,target,length,amount,failed,sign,readonly,b)],
                     'expect':{'error':None if code==0 else code if isinstance(code,dict) else {'InstructionError':[0,{'Custom':code}]},'accounts':expectations}}
            if op in (1,2,8) or op==7:
                # Rejections before CPI and rollback cases have separate cases below.
                if code==0: value['expect']['log_counts']={SYSTEM+' invoke':1,SYSTEM+' success':1}
            if after[target] is None and code==0:
                value['expect']['simulation_accounts']=[{'account':target,'state':state(b'',SYSTEM,0)}]
            if controls is not None: value['sysvars']=controls
            if fresh: value['fresh_blockhash']=True
            return value, after

        def single(name,op,target='owned',length=0,amount=0,**options):
            before = copy.deepcopy(initial)
            overrides = options.pop('overrides',{})
            before.update(overrides)
            s,_ = step(before,op,target,length,amount,**options)
            case = {'name':name,'steps':[s]}
            if overrides: case['overrides']=[{'name':n,'state':v} for n,v in overrides.items()]
            cases.append(case)

        # Real create/close/recreate across transactions plus within one invocation.
        for name,target,op in [('signed-lifecycle','signed',1),('pda-lifecycle','pda',2)]:
            before = copy.deepcopy(initial); steps=[]
            top_up = minimum(200)[0] - minimum(145)[0]
            for command,length,amount in [(op,145,0),(7,0,top_up),(4,200,40),(5,0,0),(op,145,0)]:
                s,before = step(before,command,target,length,amount,fresh=command==op and len(steps)>0)
                steps.append(s)
            cases.append({'name':name,'steps':steps})
        single('close-and-recreate-same-invocation',8,'signed',145,
               overrides={'signed':state(bytes(range(1,33)),program,100000000)})
        single('resize-grow',3,length=160)
        single('resize-shrink',3,length=1)
        single('shrink-and-regrow-zeroes',4,length=64,amount=2)
        single('exact-growth-boundary',3,length=10272)
        single('growth-over-boundary',3,length=10273,code=2010)
        single('wide-resize-limit',3,length=10485761,code=3020)
        single('readonly-resize',3,length=40,readonly=True,code=2011)
        single('wrong-owner-resize',3,length=40,code=2012,overrides={'owned':state(bytes(range(1,33)),SYSTEM)})
        single('duplicate-refund',6,code=2013)
        single('readonly-close',5,readonly=True,code=2011)
        single('wrong-owner-close',5,code=2012,overrides={'owned':state(bytes(range(1,33)),SYSTEM)})
        single('refund-overflow',5,code=2014,overrides={'wallet':state(lamports=MAX)})
        before=copy.deepcopy(initial)
        s,_=step(before,5,code=2011)
        s['instructions'][0]['accounts'][2]['writable']=False
        cases.append({'name':'readonly-refund-destination','steps':[s]})
        single('already-funded-target',1,'signed',145,code=3022,overrides={'signed':state()})
        single('missing-payer-signature',1,'signed',145,sign=False,code=3023)
        single('insufficient-create-funds',1,'signed',145,code=1,overrides={'wallet':state(lamports=1000)})
        single('incorrect-pda-bump',2,'pda',145,b=wrong_bump,code=3023)
        single('create-size-over-boundary',1,'signed',10241,code=3024)
        single('create-exact-boundary',1,'signed',10240)
        free_rent={'lamports_per_byte_year':0,'exemption_threshold':0.0,'burn_percent':0}
        single('free-rent-create-retains-account',1,'signed',145,rent=free_rent,controls={'rent':free_rent})
        single('create-then-return-error',1,'signed',145,failed=True,code=92)
        single('resize-then-return-error',3,length=64,failed=True,code=92)
        single('close-then-return-error',5,failed=True,code=92)
        single('system-transfer-current-lamports',7,amount=123456)
        single('system-transfer-then-return-error',7,amount=123456,failed=True,code=92)
        before=copy.deepcopy(initial)
        create_step,before=step(before,1,'signed',145)
        resize_step,_=step(before,3,'signed',200,code={'InsufficientFundsForRent':{'account_index':3}})
        cases.append({'name':'unfunded-resize-rolls-back','steps':[create_step,resize_step]})

        # Changes after creation and a failing second instruction must roll back
        # both the CPI's creation and the first instruction's result write.
        first,_ = step(copy.deepcopy(initial),1,'signed',145)
        second = instruction(3,'signed',64,failed=True)
        first['instructions'].append(second)
        first['expect']={'error':{'InstructionError':[1,{'Custom':92}]},'accounts':[
            {'account':'result','state':initial['result']},{'account':'wallet','state':initial['wallet']},
            {'account':'signed','absent':True}]}
        cases.append({'name':'atomic-create-resize-rollback','steps':[first]})

        # Current nondefault Rent is checked before and after ordered changes.
        a={'lamports_per_byte_year':777,'exemption_threshold':1.5,'burn_percent':17}
        b={'lamports_per_byte_year':999,'exemption_threshold':2.5,'burn_percent':19}
        before=copy.deepcopy(initial); steps=[]
        for rent,op,target,size in [(a,0,'owned',145),(b,1,'signed',145),(a,5,'signed',0)]:
            s,before=step(before,op,target,size,rent=rent,controls={'rent':rent});steps.append(s)
        cases.append({'name':'ordered-current-rent','steps':steps})
        for j,r in enumerate([{'lamports_per_byte_year':0,'exemption_threshold':0.0,'burn_percent':0},
                              {'lamports_per_byte_year':123456789012345,'exemption_threshold':0.625,'burn_percent':17},
                              {'lamports_per_byte_year':MAX//128,'exemption_threshold':1.0000000000000002,'burn_percent':50},
                              {'lamports_per_byte_year':MAX,'exemption_threshold':2.0,'burn_percent':50}]):
            n,code=minimum(0,r)
            single('rent-boundary-'+str(j),0,rent=r,controls={'rent':r},code=code)

        suite={'format':'gosvm-svm-fixtures-v1','program_id':program,'payer_seed':key('lifecycle2 fee payer; never fund').hex(),
               'sysvars':{'rent':rent_default},'signers':[{'name':'wallet','seed':wallet_seed},{'name':'signed','seed':signed_seed}],
               'native_programs':[{'name':'system','address':SYSTEM}],
               'accounts':[dict({'name':n,'address':b58(names[n])},**({'initial':v} if v is not None else {})) for n,v in initial.items()],
               'cases':cases}
        fixture=output/'fixtures.json';save(fixture,suite)
        llvm=args.llvm.resolve();elf=output/'program.so'
        run('sbf-build',[cli,'-arch','v3','-no-cache','-llvm',llvm,'-o',elf,probe])
        run('emit-c',[cli,'-arch','v3','-emit-c','-o',output/'program.c',probe])
        run('stack-compile',[llvm/'bin/clang','-target','sbf','-mcpu=v3','-O2','-fno-builtin','-fPIC','-fno-stack-protector',
            '-std=c11','-Werror','-DGOSVM_SBF_V3=1','-fstack-usage','-c',output/'program.c','-o',stage/'stack.o'])
        run('stack-link',[llvm/'bin/ld.lld','-z','notext','-shared','--Bdynamic','--strip-all','--entry','entrypoint',
            '--script',ROOT/'internal/compiler/sbf-v3.ld','--no-undefined','-o',stage/'stack.so',stage/'stack.o'])
        require(digest(elf)==digest(stage/'stack.so'),'instrumented linking changed actual ELF')
        shutil.copyfile(stage/'stack.su',output/'stack-usage.tsv')
        frames=[]
        for line in (output/'stack-usage.tsv').read_text().splitlines():
            name,size,kind=line.split('\t');frames.append({'function':name.rsplit(':',1)[-1],'bytes':int(size),'kind':kind})
        require(frames and all(f['kind']=='static' and f['bytes']<=4096 for f in frames),'fixture exceeds SBF frame bound')
        reference=None
        for sample in range(3):
            report=output/f'svm-{sample}.json'
            run(f'svm-{sample}',[cli,'svm-test','-elf',elf,'-fixtures',fixture,'-report',report])
            actual=load(report)
            require(actual['elf_sha256']==digest(elf),'executed different ELF')
            if reference is None: reference=actual['cases']
            else: require(actual['cases']==reference,'case/CU/log outputs changed')
        # Preserve the old manual artifact identity when current unpinned sources
        # do not call lifecycle APIs; SDK-1 snapshots remain unchanged too.
        baseline=output/'manual-token.so'
        run('manual-token-rebuild',[cli,'-arch','v3','-no-cache','-llvm',llvm,'-o',baseline,ROOT/'benchmarks/anchor/go-token'])
        require(digest(baseline)==digest(ROOT/'results/compiler/2026-10-05-framework-swap-current/manual-baseline.so')
                if (ROOT/'results/compiler/2026-10-05-framework-swap-current/manual-baseline.so').exists()
                else digest(baseline)==digest(ROOT/'build/anchor/go-token.so'),'manual baseline ELF changed')
        require(hashes=={p:digest(ROOT/p) for p in hashes},'sources changed during experiment')
        summary.update(passed=True, scenarios=len(cases),transactions=sum(len(c['steps']) for c in cases),samples=3,
                       native_rent_random_vectors=100000,c_rent_vectors=1000,c_lifecycle_vectors=1035,
                       elf_bytes=elf.stat().st_size,elf_sha256=digest(elf),stack_frames=frames,
                       runner_sha256=actual['runner_sha256'],runtime_version=actual['runtime_version'],fixture_sha256=digest(fixture))
        print(f'PASS: {len(cases)} lifecycle scenarios, three SBF repetitions. {output}',flush=True)
    except Exception as error:
        summary['failure']=str(error);raise
    finally:save(output/'summary.json',summary)


if __name__=='__main__':main()
