#!/usr/bin/env python3
"""Verify generated SDK-2 escrow initialization, authorization, closing and rollback."""
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
SYSTEM = b58(bytes(32))


def digest(p): return hashlib.sha256(p.read_bytes()).hexdigest()
def key(label): return hashlib.sha256(label.encode()).digest()
def save(p, value): p.write_text(json.dumps(value, indent=2)+'\n')
def load(p): return json.loads(p.read_text())
def require(ok, message):
    if not ok: raise RuntimeError(message)


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output',required=True,type=Path)
    parser.add_argument('--llvm',type=Path,default=Path.home()/'.cache/solana/v1.51/platform-tools/llvm')
    args=parser.parse_args();out=args.output.resolve();stage=ROOT/'build/escrow'/out.name
    require(not out.exists() and not stage.exists(),'choose a new evidence/staging directory')
    out.mkdir(parents=True);stage.mkdir(parents=True)
    example=ROOT/'examples/escrow'
    paths=[Path(__file__).resolve(),ROOT/'scripts/checked_token_verify.py',ROOT/'scripts/lifecycle2_key.go.txt']
    for folder in ('internal/compiler','internal/project','internal/runner','internal/sbftest','internal/testvm','cmd/gosvm','sdk','solana','examples/escrow'):
        paths.extend(p for p in (ROOT/folder).rglob('*') if p.is_file() and p.suffix!='.md' and 'build' not in p.relative_to(ROOT/folder).parts)
    paths=sorted(set(paths));hashes={str(p.relative_to(ROOT)):digest(p) for p in paths}
    env=dict(os.environ,GOCACHE=str(ROOT/'build/lifecycle-go-cache'),GOSVM_CACHE=str(stage/'cache'),
             GOPROXY='off',GOSUMDB='off',GOWORK='off',GOTOOLCHAIN='local')
    for name in ('GOFLAGS','GOSVM_TEST_RUNNER','GOSVM_TEST_FEATURES','SBF_LLVM'):env.pop(name,None)
    summary={'schema':1,'passed':False,'machine':platform.platform(),'source_sha256':hashes,'checks':[],
             'limitations':['Cancellable SOL hashlock escrow; not a token exchange, timelock or audited production application',
                            'Recipient receives the entire escrow balance including rent/donations; cancellation refunds the creator',
                            'Native callbacks do not establish transaction atomicity; actual-SBF fixtures do',
                            'Pinned local LiteSVM; no fresh-validator or external developer-trial claim',
                            'Individual build observations are not repeated comparative build benchmarks']}

    def run(label, command, cwd=ROOT):
        command=[str(x) for x in command];start=time.perf_counter()
        with (out/(label+'.log')).open('wb') as log:
            done=subprocess.run(command,cwd=cwd,env=env,stdout=log,stderr=subprocess.STDOUT,timeout=240)
        summary['checks'].append({'check':label,'command':command,'cwd':str(cwd),'exit_code':done.returncode,
                                  'wall_seconds':time.perf_counter()-start,'log':label+'.log'})
        require(done.returncode==0,f'{label}: see {out/(label+".log")}');print(label+': PASS',flush=True)

    try:
        run('generator-diagnostics',['go','test','./internal/project','-run','TestSchema2Lifecycle|TestSchema2AllInit','-count=1','-v'])
        cli=stage/'gosvm';run('cli-build',['bash','scripts/build-cli.sh',cli])
        run('runner-install',[cli,'runner','install','-archive',ROOT/'results/svm/2026-10-05-runner-install-complete/gosvm-svm-runner-0.5.0-darwin-arm64.tar.gz'])
        project=stage/'escrow'
        run('scaffold',[cli,'new','-schema','2','-sdk','2','-module','example.org/gosvm/escrow',project])
        # SDK stays exactly as scaffolded. Copy only the application/config/history;
        # generation must reproduce the committed adapter/client/IDL byte-for-byte.
        for rel in ('program.go','program_test.go','model/model.go','gosvm.json','layouts.json'):
            shutil.copyfile(example/rel,project/rel)
        for p in (example/'.gosvm/sdk').rglob('*'):
            if p.is_file():require(p.read_bytes()==(project/'.gosvm/sdk'/p.relative_to(example/'.gosvm/sdk')).read_bytes(),'SDK snapshot differs from scaffold')
        run('check',[cli,'check','-dir',project])
        for rel in ('zz_gosvm.go','client/zz_gosvm.go','idl.json','layouts.json'):
            require((project/rel).read_bytes()==(example/rel).read_bytes(),'generated artifact differs: '+rel)
        run('native-tests',[cli,'test','-dir',project,'--','-count=1'])
        source=stage/'keys.go';native=stage/'keys'
        shutil.copyfile(ROOT/'scripts/lifecycle2_key.go.txt',source)
        run('keys-build',['go','build','-o',native,source])
        seeds={n:key('escrow fixture '+n+'; never fund').hex() for n in ('creator','beneficiary','intruder')}
        run('keys',[native,*seeds.values()])
        addresses=dict(zip(seeds,(bytes.fromhex(v) for v in (out/'keys.log').read_text().splitlines())))
        program_bytes=key('generated escrow program; never deploy');program=b58(program_bytes)
        nonce=123;preimage=bytes([19,23,31])+bytes(29);commitment=hashlib.sha256(preimage).digest()
        parts=[b'escrow',addresses['creator'],struct.pack('<Q',nonce)]
        escrow,bump=derive(parts,program_bytes);addresses['escrow']=escrow
        wrong_bump=next(b for b in range(255,-1,-1) if b!=bump and not on_curve(
            hashlib.sha256(b''.join(parts)+bytes([b])+program_bytes+b'ProgramDerivedAddress').digest()))
        rent_default={'lamports_per_byte_year':3480,'exemption_threshold':2.0,'burn_percent':50}
        amount=1000000

        def discriminator(kind,name):return hashlib.sha256(f'gosvm:{kind}:{name}:v1'.encode()).digest()[:8]
        def state(data=b'',owner=SYSTEM,lamports=100000000):
            return {'data':data.hex(),'owner':owner,'lamports':lamports,'executable':False,'rent_epoch':0}
        initial={n:state() for n in seeds};initial['escrow']=None
        def rent_min(rent):return max(1,int(float((128+121)*rent['lamports_per_byte_year'])*rent['exemption_threshold']))
        def record(creator=addresses['creator'],beneficiary=addresses['beneficiary'],hash=commitment,n=nonce,b=bump,a=amount):
            return discriminator('account','Escrow')+creator+beneficiary+hash+struct.pack('<QQB',a,n,b)
        def data(op,a=amount,n=nonce,b=bump,receiver=addresses['beneficiary'],secret=preimage):
            if op=='Open':return (discriminator('instruction',op)+receiver+commitment+struct.pack('<QQB',a,n,b)).hex()
            if op=='Claim':return (discriminator('instruction',op)+secret+struct.pack('<Q',n)).hex()
            return discriminator('instruction',op).hex()
        def instruction(op,**options):
            meta=[{'account':'escrow','writable':True,'signer':False},
                  {'account':'creator','writable':op!='Claim','signer':op!='Claim'}]
            if op!='Cancel':meta.append({'account':'beneficiary','writable':op=='Claim','signer':op=='Claim'})
            if op=='Open':meta.append({'account':'system','writable':False,'signer':False})
            return {'program':'program','accounts':meta,'data':data(op,**options)}
        def assertions(after):
            return [{'account':n,'state':v} if v is not None else {'account':n,'absent':True} for n,v in after.items()]
        cases=[]
        def step(before,op,*,code=0,rent=rent_default,controls=None,fresh=False,meta_edit=None,**options):
            after=copy.deepcopy(before);ix=instruction(op,**options)
            if meta_edit:meta_edit(ix['accounts'])
            if code==0:
                if op=='Open':
                    balance=rent_min(rent)+options.get('a',amount)
                    after['creator']['lamports']-=balance
                    after['escrow']=state(record(a=options.get('a',amount)),program,balance)
                else:
                    to='beneficiary' if op=='Claim' else 'creator'
                    after[to]['lamports']+=after['escrow']['lamports'];after['escrow']=None
            error=None if code==0 else code if isinstance(code,dict) else {'InstructionError':[0,{'Custom':code}]}
            expect={'error':error,'accounts':assertions(after)}
            if code==0 and op=='Open':expect['log_counts']={SYSTEM+' invoke':2,SYSTEM+' success':2}
            if code!=0 and op=='Open' and code in (4100,1):
                expect['log_counts']={SYSTEM+' invoke':1 if code==4100 else 2,SYSTEM+' success':1}
            if code==0 and op!='Open':expect['simulation_accounts']=[{'account':'escrow','state':state(b'',SYSTEM,0)}]
            value={'instructions':[ix],'expect':expect}
            if controls is not None:value['sysvars']=controls
            if fresh:value['fresh_blockhash']=True
            return value,after
        def single(name,op,*,opened=False,overrides=None,**options):
            before=copy.deepcopy(initial)
            if opened:
                before['creator']['lamports']-=rent_min(rent_default)+amount
                before['escrow']=state(record(),program,rent_min(rent_default)+amount)
            if overrides:before.update(overrides)
            s,_=step(before,op,**options)
            case={'name':name,'steps':[s]}
            if opened or overrides:
                case['overrides']=[{'name':n,'state':v} for n,v in before.items() if v is not None and v!=initial[n]]
            cases.append(case)
        # Full genesis-unallocated lifecycle paths; close and reuse the same PDA.
        for name,commands in [('open-claim-reuse',['Open','Claim','Open','Cancel']),('open-cancel-reuse',['Open','Cancel','Open','Claim'])]:
            before=copy.deepcopy(initial);steps=[]
            for j,op in enumerate(commands):
                s,before=step(before,op,fresh=j>=2);steps.append(s)
            cases.append({'name':name,'steps':steps})
        single('zero-deposit-rollback','Open',a=0,code=4100)
        single('deposit-cpi-failure-rollback','Open',a=100000000,code=1)
        single('target-already-funded','Open',overrides={'escrow':state(lamports=1)},code=6009)
        single('target-already-owned','Open',overrides={'escrow':state(record(),program,rent_min(rent_default)+amount)},code=6009)
        single('target-has-data','Open',overrides={'escrow':state(b'\x01',lamports=10000000)},code=6009)
        single('bad-pda-bump','Open',b=wrong_bump,code=6008)
        single('bad-nonce-pda','Open',n=124,code=6008)
        single('bad-beneficiary-args','Open',receiver=addresses['intruder'],code=6007)
        single('beneficiary-substitution','Open',code=6007,meta_edit=lambda m:m[2].update(account='intruder'))
        single('creator-unsigned','Open',code=6001,meta_edit=lambda m:m[1].update(signer=False))
        single('creator-readonly','Open',code=6001,meta_edit=lambda m:m[1].update(writable=False))
        single('target-readonly','Open',code=6001,meta_edit=lambda m:m[0].update(writable=False))
        single('System-substitution','Open',code=6005,meta_edit=lambda m:m[3].update(account='program'))
        single('duplicate-recipient','Open',code=6006,meta_edit=lambda m:m[2].update(account='creator'))
        single('claim-wrong-secret','Claim',opened=True,secret=bytes([99])+bytes(31),code=4102)
        single('claim-wrong-nonce','Claim',opened=True,n=124,code=4103)
        single('claim-beneficiary-unsigned','Claim',opened=True,code=6001,meta_edit=lambda m:m[2].update(signer=False))
        single('claim-beneficiary-substitute','Claim',opened=True,code=6007,meta_edit=lambda m:m[2].update(account='intruder'))
        single('claim-creator-substitute','Claim',opened=True,code=6007,meta_edit=lambda m:m[1].update(account='intruder'))
        single('cancel-creator-unsigned','Cancel',opened=True,code=6001,meta_edit=lambda m:m[1].update(signer=False))
        single('cancel-creator-substitute','Cancel',opened=True,code=6007,meta_edit=lambda m:m[1].update(account='intruder'))
        single('close-refund-overflow','Claim',opened=True,overrides={'beneficiary':state(lamports=(1<<64)-1)},code=2014)
        single('close-owner-mismatch','Cancel',opened=True,overrides={'escrow':state(record(),SYSTEM,rent_min(rent_default)+amount)},code=6002)
        single('close-wrong-layout','Cancel',opened=True,overrides={'escrow':state(record()[:-1],program,rent_min(rent_default)+amount)},code=6003)
        single('close-unallocated','Cancel',code=6002)
        # A bad second instruction must roll back creation, deposit and both fees
        # apart from the transaction fee paid by the separate fee payer.
        before=copy.deepcopy(initial);s,_=step(before,'Open')
        s['instructions'].append(instruction('Claim',secret=bytes(32)))
        s['expect']={'error':{'InstructionError':[1,{'Custom':4102}]},'accounts':assertions(before)}
        cases.append({'name':'atomic-open-bad-claim-rollback','steps':[s]})
        # Closing followed by a failing reinitialization must restore the original
        # owned layout and balances, not retain a System owner or overwritten data.
        before=copy.deepcopy(initial);before['creator']['lamports']-=rent_min(rent_default)+amount
        before['escrow']=state(record(),program,rent_min(rent_default)+amount)
        s,_=step(before,'Cancel');s.pop('simulation_accounts',None)
        s['instructions'].append(instruction('Open',a=0))
        s['expect']={'error':{'InstructionError':[1,{'Custom':4100}]},'accounts':assertions(before)}
        cases.append({'name':'atomic-close-bad-open-rollback','overrides':[{'name':n,'state':v} for n,v in before.items() if v is not None and v!=initial[n]],'steps':[s]})
        # Custom/free Rent still funds and retains created state; cancellation
        # refunds exactly the actual deposit/reserve, even if rent changes later.
        for j,rent in enumerate([{'lamports_per_byte_year':777,'exemption_threshold':1.5,'burn_percent':17},
                                 {'lamports_per_byte_year':0,'exemption_threshold':0.0,'burn_percent':0}]):
            before=copy.deepcopy(initial);s,before=step(before,'Open',rent=rent,controls={'rent':rent})
            end,_=step(before,'Cancel');cases.append({'name':'current-rent-lifecycle-'+str(j),'steps':[s,end]})
        # Retrying a claim after a rejected secret is a fresh transaction; failures
        # must keep the funded record available for the next valid claimant.
        before=copy.deepcopy(initial);s,before=step(before,'Open')
        bad,_=step(before,'Claim',secret=bytes(32),code=4102)
        good,_=step(before,'Claim');cases.append({'name':'failed-claim-then-success','steps':[s,bad,good]})

        suite={'format':'gosvm-svm-fixtures-v1','program_id':program,'payer_seed':key('escrow fee payer; never fund').hex(),
               'sysvars':{'rent':rent_default},'signers':[{'name':n,'seed':s} for n,s in seeds.items()],
               'native_programs':[{'name':'system','address':SYSTEM}],
               'accounts':[dict({'name':n,'address':b58(addresses[n])},**({'initial':v} if v is not None else {})) for n,v in initial.items()],
               'cases':cases}
        fixture=out/'fixtures.json';save(fixture,suite)
        packaged=example/'testdata/svm.json'
        if packaged.exists():require(load(packaged)==suite,'packaged fixtures drifted from independent expectations')
        run('project-svm',[cli,'test','--svm','-dir',project,'-llvm',args.llvm,'-svm-fixtures',fixture,'--','-count=1'])
        elf=out/'program.so';shutil.copyfile(project/'build/program.so',elf)
        first=load(project/'build/svm-results.json');shutil.copyfile(project/'build/svm-results.json',out/'project-svm.json')
        require(first['elf_sha256']==digest(elf),'project ran another ELF')
        run('emit-c',[cli,'-arch','v3','-emit-c','-o',out/'program.c',project])
        llvm=args.llvm.resolve()
        run('stack-compile',[llvm/'bin/clang','-target','sbf','-mcpu=v3','-O2','-fno-builtin','-fPIC','-fno-stack-protector','-std=c11','-Werror',
            '-DGOSVM_SBF_V3=1','-fstack-usage','-c',out/'program.c','-o',stage/'stack.o'])
        run('stack-link',[llvm/'bin/ld.lld','-z','notext','-shared','--Bdynamic','--strip-all','--entry','entrypoint',
            '--script',ROOT/'internal/compiler/sbf-v3.ld','--no-undefined','-o',stage/'stack.so',stage/'stack.o'])
        require(digest(elf)==digest(stage/'stack.so'),'stack instrumentation changed actual ELF')
        shutil.copyfile(stage/'stack.su',out/'stack-usage.tsv')
        frames=[]
        for line in (out/'stack-usage.tsv').read_text().splitlines():
            name,size,kind=line.split('\t');frames.append({'function':name.rsplit(':',1)[-1],'bytes':int(size),'kind':kind})
        require(frames and all(f['kind']=='static' and f['bytes']<=4096 for f in frames),'escrow exceeds static SBF frame bound')
        for sample in range(3):
            report=out/f'svm-{sample}.json';run(f'svm-{sample}',[cli,'svm-test','-elf',elf,'-fixtures',fixture,'-report',report])
            actual=load(report);require(actual['cases']==first['cases'],'case/CU/log outputs changed between repetitions')
            require(actual['elf_sha256']==digest(elf),'standalone ran another ELF')
        require(hashes=={p:digest(ROOT/p) for p in hashes},'sources changed during proof')
        summary.update(passed=True,scenarios=len(cases),transactions=sum(len(c['steps']) for c in cases),samples=3,
                       expected_failures=sum(s['expect']['error'] is not None for c in cases for s in c['steps']),
                       elf_bytes=elf.stat().st_size,elf_sha256=digest(elf),stack_frames=frames,
                       fixture_sha256=digest(fixture),runner_sha256=actual['runner_sha256'],runtime_version=actual['runtime_version'])
        print(f'PASS: {len(cases)} generated escrow scenarios, three SBF repetitions. {out}',flush=True)
    except Exception as error:summary['failure']=str(error);raise
    finally:save(out/'summary.json',summary)


if __name__=='__main__':main()
