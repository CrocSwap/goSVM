#!/usr/bin/env python3
"""Prove generated full-swap creation, deposits, swap, drain/close/reuse in SBF."""
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

ROOT=Path(__file__).resolve().parents[1]
TOKEN='TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA'
SYSTEM=b58(bytes(32))
MAX=(1<<64)-1

def digest(p): return hashlib.sha256(p.read_bytes()).hexdigest()
def key(label): return hashlib.sha256(label.encode()).digest()
def save(p,value): p.write_text(json.dumps(value,indent=2)+'\n')
def load(p): return json.loads(p.read_text())
def require(value,message):
    if not value: raise RuntimeError(message)

def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output',required=True,type=Path)
    parser.add_argument('--llvm',type=Path,default=Path.home()/'.cache/solana/v1.51/platform-tools/llvm')
    args=parser.parse_args();out=args.output.resolve();stage=ROOT/'build/managed-swap'/out.name
    require(not out.exists() and not stage.exists(),'choose new evidence/staging directories');out.mkdir(parents=True);stage.mkdir(parents=True)
    example=ROOT/'examples/full-swap'
    paths=[Path(__file__).resolve(),ROOT/'scripts/checked_token_verify.py',ROOT/'scripts/lifecycle2_key.go.txt']
    for folder in ('internal/compiler','internal/project','internal/testvm','internal/sbftest','cmd/gosvm','sdk','solana','examples/full-swap','examples/full-swap-service'):
        paths.extend(p for p in (ROOT/folder).rglob('*') if p.is_file() and p.suffix!='.md' and 'build' not in p.relative_to(ROOT/folder).parts)
    paths=sorted(set(paths));hashes={str(p.relative_to(ROOT)):digest(p) for p in paths}
    env=dict(os.environ,GOCACHE=str(ROOT/'build/lifecycle-go-cache'),GOSVM_CACHE=str(stage/'cache'),GOPROXY='off',GOSUMDB='off',GOWORK='off',GOTOOLCHAIN='local')
    for name in ('GOFLAGS','GOSVM_TEST_RUNNER','GOSVM_TEST_FEATURES','SBF_LLVM'):env.pop(name,None)
    summary={'schema':1,'passed':False,'machine':platform.platform(),'source_sha256':hashes,'checks':[],
             'limitations':['Creator-owned liquidity without LP tokens; creator may drain and close the pool',
                            'Classic non-native tokens; Token-2022, wrapped SOL, delegate and multisig excluded',
                            'External mints/user token holdings are preexisting fixtures; pool and vaults start unallocated',
                            'Actual SBF proves atomicity; native callbacks do not',
                            'Pinned local LiteSVM; no fresh-validator or independent developer-trial claim',
                            'Individual build observations are not repeated comparative benchmarks']}
    def run(label,command,cwd=ROOT):
        command=[str(v) for v in command];start=time.perf_counter()
        with (out/(label+'.log')).open('wb') as log:done=subprocess.run(command,cwd=cwd,env=env,stdout=log,stderr=subprocess.STDOUT,timeout=240)
        summary['checks'].append({'check':label,'command':command,'cwd':str(cwd),'exit_code':done.returncode,'wall_seconds':time.perf_counter()-start,'log':label+'.log'})
        require(done.returncode==0,f'{label}: see {out/(label+".log")}');print(label+': PASS',flush=True)
    try:
        run('token-native',['go','test','./sdk/token','-count=1','-v'])
        cli=stage/'gosvm';run('cli-build',['bash','scripts/build-cli.sh',cli])
        run('runner-install',[cli,'runner','install','-archive',ROOT/'results/svm/2026-10-05-runner-install-complete/gosvm-svm-runner-0.5.0-darwin-arm64.tar.gz'])
        project=stage/'full-swap';run('scaffold',[cli,'new','-schema','2','-sdk','2','-module','example.org/gosvm/full-swap',project])
        for rel in ('zz_gosvm.go','client/zz_gosvm.go','layouts.json','program_test.go'):(project/rel).unlink()
        for rel in ('gosvm.json','program.go','lifecycle.go','lifecycle_test.go','model/model.go','model/quote.go','model/quote_test.go'):
            (project/rel).parent.mkdir(parents=True,exist_ok=True);shutil.copyfile(example/rel,project/rel)
        run('check',[cli,'check','-dir',project]);run('native',[cli,'test','-dir',project,'--','-count=1'])
        for rel in ('zz_gosvm.go','client/zz_gosvm.go','idl.json','layouts.json'):require((project/rel).read_bytes()==(example/rel).read_bytes(),'generated drift '+rel)
        for file in (example/'.gosvm/sdk').rglob('*'):
            if file.is_file():require(file.read_bytes()==(project/'.gosvm/sdk'/file.relative_to(example/'.gosvm/sdk')).read_bytes(),'SDK drift')
        src=stage/'keys.go';shutil.copyfile(ROOT/'scripts/lifecycle2_key.go.txt',src);native=stage/'keys'
        run('keys-build',['go','build','-o',native,src])
        seeds={n:key('managed-swap '+n+'; never fund').hex() for n in ('creator','user','pool','intruder')}
        run('keys',[native,*seeds.values()]);addresses=dict(zip(seeds,[bytes.fromhex(s) for s in (out/'keys.log').read_text().splitlines()]))
        pid=key('managed-swap program; never deploy');program=b58(pid)
        authority,bump=derive([addresses['pool']],pid);vx,xbump=derive([b'vaultx',addresses['pool']],pid);vy,ybump=derive([b'vaulty',addresses['pool']],pid)
        addresses.update(authority=authority,vaultx=vx,vaulty=vy)
        for n in ('creatorx','creatory','userx','usery','mintx','minty'):addresses[n]=key('managed-swap '+n+'; never fund')
        wrong_bump=next(b for b in range(255,-1,-1) if b!=xbump and not on_curve(hashlib.sha256(b'vaultx'+addresses['pool']+bytes([b])+pid+b'ProgramDerivedAddress').digest()))
        rent={'lamports_per_byte_year':3480,'exemption_threshold':2.0,'burn_percent':50}
        def minimum(size,r=rent):return max(1,int(float((128+size)*r['lamports_per_byte_year'])*r['exemption_threshold']))
        def state(data=b'',owner=SYSTEM,lamports=100000000):return {'data':data.hex(),'owner':owner,'lamports':lamports,'executable':False,'rent_epoch':0}
        def token_data(mint,owner,amount=0,frozen=False,close=None):
            data=bytearray(165);data[:32]=addresses[mint];data[32:64]=addresses[owner];struct.pack_into('<Q',data,64,amount);data[108]=2 if frozen else 1
            if close is not None:data[129]=1;data[133:165]=addresses[close]
            return bytes(data)
        def mint_data(supply):
            data=bytearray(82);data[0]=1;data[4:36]=addresses['creator'];struct.pack_into('<Q',data,36,supply);data[44]=6;data[45]=1
            data[46]=1;data[50:82]=addresses['creator'];return bytes(data)
        initial={n:state() for n in ('creator','user','intruder')};initial.update(pool=None,vaultx=None,vaulty=None,authority=None)
        for n,mint,owner,amount in [('creatorx','mintx','creator',100000000),('creatory','minty','creator',100000000),('userx','mintx','user',100000),('usery','minty','user',11)]:
            initial[n]=state(token_data(mint,owner,amount),TOKEN,minimum(165))
        initial['mintx']=state(mint_data(100100000),TOKEN,minimum(82));initial['minty']=state(mint_data(100000011),TOKEN,minimum(82))
        def disc(kind,name):return hashlib.sha256(f'gosvm:{kind}:{name}:v1'.encode()).digest()[:8]
        def record(swaps=0):return disc('account','ManagedPool')+addresses['vaultx']+addresses['vaulty']+addresses['mintx']+addresses['minty']+struct.pack('<BQ',bump,swaps)+addresses['creator']
        def instruction(op,x=1000000,y=2000000,amount=10000,minout=1,xb=xbump):
            if op=='CreatePool':
                names=['pool','creator','creatorx','vaultx','vaulty','creatory','authority','token','system','mintx','minty'];signers={'pool','creator'};readonly={'authority','token','system','mintx','minty'}
                data=disc('instruction',op)+struct.pack('<QQBBB',x,y,bump,xb,ybump)
            elif op=='ManagedSwap':
                names=['pool','user','userx','vaultx','vaulty','usery','authority','token'];signers={'user'};readonly={'user','authority','token'}
                data=disc('instruction',op)+struct.pack('<QQ',amount,minout)
            else:
                names=['pool','creator','creatorx','vaultx','vaulty','creatory','authority','token'];signers={'creator'};readonly={'authority','token'};data=disc('instruction',op)
            return {'program':'program','accounts':[{'account':n,'writable':n not in readonly,'signer':n in signers} for n in names],'data':data.hex()}
        watched=['pool','vaultx','vaulty','creator','intruder','creatorx','creatory','userx','usery','mintx','minty']
        def assertions(states):return [{'account':n,'state':states[n]} if states[n] is not None else {'account':n,'absent':True} for n in watched]
        def amount(states,n):return struct.unpack_from('<Q',bytes.fromhex(states[n]['data']),64)[0]
        def set_amount(states,n,value):
            data=bytearray.fromhex(states[n]['data']);struct.pack_into('<Q',data,64,value);states[n]['data']=data.hex()
        def step(before,op,*,code=0,controls=None,fresh=False,edit=None,r=rent,**options):
            after=copy.deepcopy(before);ix=instruction(op,**options)
            if edit:edit(ix['accounts'])
            if code==0:
                if op=='CreatePool':
                    x,y=options.get('x',1000000),options.get('y',2000000)
                    after['creator']['lamports']-=minimum(177,r)+2*minimum(165,r)
                    after['pool']=state(record(),program,minimum(177,r))
                    after['vaultx']=state(token_data('mintx','authority',x),TOKEN,minimum(165,r));after['vaulty']=state(token_data('minty','authority',y),TOKEN,minimum(165,r))
                    set_amount(after,'creatorx',amount(before,'creatorx')-x);set_amount(after,'creatory',amount(before,'creatory')-y)
                elif op=='ManagedSwap':
                    a=options.get('amount',10000);x,y=amount(before,'vaultx'),amount(before,'vaulty');net=a-(a*30+9999)//10000;quoted=y*net//(x+net)
                    for n,v in [('userx',amount(before,'userx')-a),('vaultx',x+a),('vaulty',y-quoted),('usery',amount(before,'usery')+quoted)]:set_amount(after,n,v)
                    old=bytes.fromhex(before['pool']['data']);counter=struct.unpack_from('<Q',old,137)[0];after['pool']['data']=record(counter+1).hex()
                else:
                    for v,d in [('vaultx','creatorx'),('vaulty','creatory')]:set_amount(after,d,amount(before,d)+amount(before,v))
                    after['creator']['lamports']+=sum(before[n]['lamports'] for n in ('pool','vaultx','vaulty'))
                    for n in ('pool','vaultx','vaulty'):after[n]=None
            error=None if code==0 else code if isinstance(code,dict) else {'InstructionError':[0,{'Custom':code}]}
            result={'instructions':[ix],'expect':{'error':error,'accounts':assertions(after)}}
            if controls is not None:result['sysvars']=controls
            if fresh:result['fresh_blockhash']=True
            if code==0:
                if op=='CreatePool':result['expect']['log_counts']={SYSTEM+' invoke':3,SYSTEM+' success':3,TOKEN+' invoke':4,TOKEN+' success':4}
                elif op=='ManagedSwap':result['expect']['log_counts']={TOKEN+' invoke':2,TOKEN+' success':2}
                else:
                    count=2+int(amount(before,'vaultx')!=0)+int(amount(before,'vaulty')!=0)
                    result['expect']['log_counts']={TOKEN+' invoke':count,TOKEN+' success':count}
                    result['expect']['simulation_accounts']=[{'account':n,'state':state(b'',SYSTEM,0)} for n in ('pool','vaultx','vaulty')]
            return result,after
        cases=[]
        def single(name,op,*,opened=False,overrides=None,**options):
            before=copy.deepcopy(initial)
            if opened:_,before=step(before,'CreatePool')
            if overrides:before.update(overrides)
            s,_=step(before,op,**options);case={'name':name,'steps':[s]}
            changed=[{'name':n,'state':v} for n,v in before.items() if v is not None and v!=initial[n]]
            if changed:case['overrides']=changed
            cases.append(case)
        before=copy.deepcopy(initial);steps=[]
        for j,op in enumerate(('CreatePool','ManagedSwap','ClosePool','CreatePool','ManagedSwap','ClosePool')):
            s,before=step(before,op,fresh=j>=3);steps.append(s)
        cases.append({'name':'create-deposit-swap-drain-close-reuse','steps':steps})
        single('create-zero-reserve','CreatePool',x=0,code=115)
        single('create-insufficient-tokens','CreatePool',y=100000001,code=115)
        single('create-mint-layout','CreatePool',overrides={'mintx':state(b'bad',TOKEN,minimum(82))},code=3012)
        single('create-mint-uninitialized','CreatePool',overrides={'mintx':state(bytes(82),TOKEN,minimum(82))},code=3012)
        single('create-wallet-owner','CreatePool',code=6007,edit=lambda m:m[1].update(account='intruder'))
        single('create-pool-unsigned','CreatePool',code=6001,edit=lambda m:m[0].update(signer=False))
        single('create-payer-unsigned','CreatePool',code=6001,edit=lambda m:m[1].update(signer=False))
        single('create-target-readonly','CreatePool',code=6001,edit=lambda m:m[3].update(writable=False))
        single('create-vault-alias','CreatePool',code=6006,edit=lambda m:m[4].update(account='vaultx'))
        single('create-wrong-vault-bump','CreatePool',xb=wrong_bump,code=6008)
        single('create-vault-already-funded','CreatePool',overrides={'vaultx':state(lamports=1)},code=3022)
        single('create-pool-already-funded','CreatePool',overrides={'pool':state(lamports=1)},code=6009)
        single('create-insufficient-lamports-after-pool','CreatePool',overrides={'creator':state(lamports=minimum(177)+100)},code=1)
        single('create-second-deposit-frozen-rolls-back','CreatePool',overrides={'creatory':state(token_data('minty','creator',100000000,True),TOKEN,minimum(165))},code=17)
        single('create-token-program-substitution','CreatePool',code=6005,edit=lambda m:m[7].update(account='program'))
        single('close-wrong-creator','ClosePool',opened=True,code=6007,edit=lambda m:m[1].update(account='intruder'))
        single('close-unsigned-creator','ClosePool',opened=True,code=6001,edit=lambda m:m[1].update(signer=False))
        single('close-vault-substitution','ClosePool',opened=True,code=6007,edit=lambda m:m[3].update(account='userx'))
        single('close-refund-token-authority','ClosePool',opened=True,code=6007,overrides={'creatorx':state(token_data('mintx','intruder',99000000),TOKEN,minimum(165))})
        single('close-second-refund-frozen','ClosePool',opened=True,code=17,overrides={'creatory':state(token_data('minty','creator',98000000,True),TOKEN,minimum(165))})
        single('close-second-vault-close-authority','ClosePool',opened=True,code=3013,overrides={'vaulty':state(token_data('minty','authority',2000000,close='intruder'),TOKEN,minimum(165))})
        single('close-lamport-overflow','ClosePool',opened=True,code=14,overrides={'creator':state(lamports=MAX)})
        single('swap-slippage-after-create','ManagedSwap',opened=True,minout=2000000,code=113)
        # Later instruction failures restore initial creation or fully closed state.
        before=copy.deepcopy(initial);s,_=step(before,'CreatePool');s['instructions'].append(instruction('ManagedSwap',minout=2000000));s['expect']={'error':{'InstructionError':[1,{'Custom':113}]},'accounts':assertions(before)}
        cases.append({'name':'atomic-create-bad-swap-rollback','steps':[s]})
        _,before=step(copy.deepcopy(initial),'CreatePool');s,_=step(before,'ClosePool');s['instructions'].append(instruction('CreatePool',x=0));s['expect']={'error':{'InstructionError':[1,{'Custom':115}]},'accounts':assertions(before)}
        cases.append({'name':'atomic-close-bad-recreate-rollback','overrides':[{'name':n,'state':v} for n,v in before.items() if v is not None and v!=initial[n]],'steps':[s]})
        # All-zero frozen vaults are closable without attempting a token transfer.
        overrides={'vaultx':state(token_data('mintx','authority',0,True),TOKEN,minimum(165)),'vaulty':state(token_data('minty','authority',0,True),TOKEN,minimum(165))}
        single('close-empty-frozen-vaults','ClosePool',opened=True,overrides=overrides)
        for i,r in enumerate(({'lamports_per_byte_year':777,'exemption_threshold':1.5,'burn_percent':17},{'lamports_per_byte_year':0,'exemption_threshold':0.0,'burn_percent':0})):
            before=copy.deepcopy(initial);s,before=step(before,'CreatePool',r=r,controls={'rent':r});end,_=step(before,'ClosePool')
            cases.append({'name':'current-rent-lifecycle-'+str(i),'steps':[s,end]})
        oracle=ROOT/'results/svm/2026-10-05-general-complete/token-fixtures.json';token_sha=load(oracle)['programs'][0]['sha256'];token_file=oracle.parent/'spl-token.so'
        require(digest(token_file)==token_sha,'token program drift');shutil.copyfile(token_file,out/'spl-token.so')
        suite={'format':'gosvm-svm-fixtures-v1','program_id':program,'payer_seed':key('managed-swap fee payer; never fund').hex(),'sysvars':{'rent':rent},
               'signers':[{'name':n,'seed':s} for n,s in seeds.items()],
               'programs':[{'name':'token','address':TOKEN,'elf':'spl-token.so','sha256':token_sha}],
               'native_programs':[{'name':'system','address':SYSTEM}],
               'accounts':[dict({'name':n,'address':b58(addresses[n])},**({'initial':v} if v is not None else {})) for n,v in initial.items()], 'cases':cases}
        fixtures=out/'fixtures.json';save(fixtures,suite)
        packaged=example/'testdata/lifecycle.json'
        if packaged.exists():require(load(packaged)==suite,'packaged lifecycle fixture drift')
        run('project-svm',[cli,'test','--svm','-dir',project,'-llvm',args.llvm,'-svm-fixtures',fixtures,'--','-count=1'])
        elf=out/'program.so';shutil.copyfile(project/'build/program.so',elf);shutil.copyfile(project/'build/svm-results.json',out/'project-svm.json');first=load(out/'project-svm.json')
        run('emit-c',[cli,'-arch','v3','-emit-c','-o',out/'program.c',project])
        run('stack-compile',[args.llvm/'bin/clang','-target','sbf','-mcpu=v3','-O2','-fno-builtin','-fPIC','-fno-stack-protector','-std=c11','-Werror','-DGOSVM_SBF_V3=1','-fstack-usage','-c',out/'program.c','-o',stage/'stack.o'])
        run('stack-link',[args.llvm/'bin/ld.lld','-z','notext','-shared','--Bdynamic','--strip-all','--entry','entrypoint','--script',ROOT/'internal/compiler/sbf-v3.ld','--no-undefined','-o',stage/'stack.so',stage/'stack.o'])
        require(digest(elf)==digest(stage/'stack.so'),'instrumented link differs');shutil.copyfile(stage/'stack.su',out/'stack-usage.tsv');frames=[]
        for line in (out/'stack-usage.tsv').read_text().splitlines():
            name,size,kind=line.split('\t');frames.append({'function':name.rsplit(':',1)[-1],'bytes':int(size),'kind':kind})
        require(frames and all(f['kind']=='static' and f['bytes']<=4096 for f in frames),'static frame exceeds SBF budget')
        for i in range(3):
            report=out/f'svm-{i}.json';run(f'svm-{i}',[cli,'svm-test','-elf',elf,'-fixtures',fixtures,'-report',report]);actual=load(report)
            require(actual['cases']==first['cases'],'case/CU/log drift');require(actual['elf_sha256']==digest(elf),'another ELF ran')
        require(hashes=={p:digest(ROOT/p) for p in hashes},'sources changed during proof')
        summary.update(passed=True,scenarios=len(cases),transactions=sum(len(c['steps']) for c in cases),samples=3,
                       expected_failures=sum(s['expect']['error'] is not None for c in cases for s in c['steps']),
                       elf_bytes=elf.stat().st_size,elf_sha256=digest(elf),stack_frames=frames,fixture_sha256=digest(fixtures),runner_sha256=actual['runner_sha256'],runtime_version=actual['runtime_version'])
        print(f'PASS: {len(cases)} managed-swap lifecycle scenarios × three. {out}',flush=True)
    except Exception as error:summary['failure']=str(error);raise
    finally:save(out/'summary.json',summary)

if __name__=='__main__':main()
