#!/usr/bin/env python3
"""Verify SDK-2 checked token/PDA/generic CPI with real compiled-SBF CPIs."""
import argparse
import copy
import hashlib
import json
import os
import platform
from pathlib import Path
import random
import shutil
import struct
import subprocess
import time

ROOT = Path(__file__).resolve().parents[1]
ALPHABET = '123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz'
TOKEN = bytes([6,221,246,225,215,101,161,147,217,203,225,70,206,235,121,172,28,180,133,237,95,91,55,145,58,140,245,133,126,255,0,169])
MAX = (1 << 64) - 1


def digest(p): return hashlib.sha256(p.read_bytes()).hexdigest()
def save(p, v): p.write_text(json.dumps(v, indent=2) + '\n')
def load(p): return json.loads(p.read_text())
def require(ok, message):
    if not ok: raise RuntimeError(message)


def b58(data):
    n, result = int.from_bytes(data, 'big'), ''
    while n:
        n, r = divmod(n, 58)
        result = ALPHABET[r] + result
    return '1' * (len(data) - len(data.lstrip(b'\0'))) + result


def on_curve(data):
    p = (1 << 255) - 19
    y = int.from_bytes(data, 'little') & ((1 << 255) - 1)
    d = -121665 * pow(121666, -1, p) % p
    x2 = (y*y-1) * pow((d*y*y+1) % p, -1, p) % p
    return x2 == 0 or pow(x2, (p-1)//2, p) == 1


def derive(parts, program):
    for bump in range(255, -1, -1):
        h = hashlib.sha256(b''.join(parts) + bytes([bump]) + program + b'ProgramDerivedAddress').digest()
        if not on_curve(h): return h, bump
    raise RuntimeError('no fixture bump')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', required=True, type=Path)
    parser.add_argument('--llvm', type=Path, default=Path.home()/'.cache/solana/v1.51/platform-tools/llvm')
    args = parser.parse_args()
    output = args.output.resolve(); stage = ROOT/'build/checked-token'/output.name
    require(not output.exists() and not stage.exists(), 'choose a new evidence/staging directory')
    output.mkdir(parents=True); stage.mkdir(parents=True)
    env = dict(os.environ, GOCACHE=str(ROOT/'build/lifecycle-go-cache'), GOSVM_CACHE=str(stage/'cache'), GOPROXY='off', GOSUMDB='off', GOWORK='off')
    for name in ('GOFLAGS','GOSVM_TEST_RUNNER','GOSVM_TEST_FEATURES','SBF_LLVM'): env.pop(name,None)
    paths = [Path(__file__).resolve(), ROOT/'scripts/checked_token_native.go.txt']
    for folder in ('internal/compiler','internal/project','internal/testvm','internal/sbftest','internal/runner','cmd/gosvm','sdk','solana','examples/checked-token'):
        paths.extend(p for p in (ROOT/folder).rglob('*') if p.is_file() and p.suffix!='.md')
    hashes = {str(p.relative_to(ROOT)):digest(p) for p in sorted(set(paths))}
    summary = {'schema':1,'passed':False,'machine':platform.platform(),'source_sha256':hashes,'checks':[],
               'limitations':['Preloaded SDK-2 compiler fixture; not full swap migration, initialization, escrow or external developer trial',
                              'Native-Go/C boundary comparisons and real LiteSVM CPIs; no fresh validator comparison',
                              'Measurements apply only to checked-transfer/generic-CPI fixture']}

    def run(label, command, input_data=None):
        command=[str(x) for x in command];start=time.perf_counter()
        with (output/(label+'.log')).open('wb') as log:
            done=subprocess.run(command,cwd=ROOT,env=env,input=input_data,stdout=log,stderr=subprocess.STDOUT,timeout=180)
        summary['checks'].append({'check':label,'command':command,'exit_code':done.returncode,'wall_seconds':time.perf_counter()-start,'log':label+'.log'})
        require(done.returncode==0,f'{label}: see {output/(label+".log")}');print(label+': PASS',flush=True)

    try:
        run('helper-native-tests',['go','test','./sdk/...','./internal/project','-run','TestExplicitSDK|TestSDK1|TestBounded|TestInvoke|TestChecked|TestToken|TestAddress|TestExplicitSeed','-count=1','-v'])
        run('boundary-differential',['go','test','./internal/compiler','-run','TestSDK2Invoke','-count=1','-v'])
        cli=stage/'gosvm';run('cli-build',['bash','scripts/build-cli.sh',cli])
        run('runner-install',[cli,'runner','install','-archive',ROOT/'results/svm/2026-10-05-runner-install-complete/gosvm-svm-runner-0.5.0-darwin-arm64.tar.gz'])
        run('sdk2-scaffold',[cli,'new','-schema','2','-sdk','2',stage/'sdk-two'])
        run('sdk2-check',[cli,'check','-dir',stage/'sdk-two'])
        run('sdk2-native',[cli,'test','-dir',stage/'sdk-two'])
        native_src=stage/'native.go';shutil.copyfile(ROOT/'scripts/checked_token_native.go.txt',native_src)
        shutil.copyfile(native_src,output/'native-driver.go.txt')
        native=stage/'native';run('native-build',['go','build','-ldflags=-linkmode=external','-o',native,native_src]);run('native-sign',['codesign','--force','--sign','-',native])
        seed=hashlib.sha256(b'SDK2 fixture authority; never fund').hexdigest();run('authority-key',[native,'--key',seed]);authority=bytes.fromhex((output/'authority-key.log').read_text().strip())
        program_bytes=hashlib.sha256(b'SDK2 fixture program; never deploy').digest();program=b58(program_bytes)
        names=['result','authority','source','destination','pda','token','frozen']
        addresses={n:hashlib.sha256(('SDK2 '+n).encode()).digest() for n in names};addresses['authority']=authority;addresses['token']=TOKEN
        pda,bump=derive([addresses['result'],struct.pack('<I',0x12345678),struct.pack('<Q',0x1122334455667788)],program_bytes);addresses['pda']=pda
        _,other_bump=derive([bytes([13])],program_bytes)
        mint=hashlib.sha256(b'SDK2 mint').digest()

        def token_data(amount,owner,state=1):
            b=bytearray(165);b[:32]=mint;b[32:64]=owner;struct.pack_into('<Q',b,64,amount);b[108]=state;return b.hex()
        def state(data='',owner=program,lamports=10000000): return {'data':data,'owner':owner,'lamports':lamports,'executable':False,'rent_epoch':0}
        def initial(x=100,y=5,mode=0):
            auth=pda if mode in (1,3) else authority
            return {'result':state(bytes(24).hex()),'authority':state('',b58(bytes(32))),
                    'source':state(token_data(x,auth),b58(TOKEN)),'destination':state(token_data(y,auth),b58(TOKEN)),
                    'pda':state('',b58(bytes(32))),'frozen':state(token_data(0,authority,2),b58(TOKEN))}
        def data(amount,mode,b=bump): return (struct.pack('<QBBB',amount,mode,b,other_bump)+bytes(5)).hex()
        def refs(mode=0,signer=True):
            return [{'account':n,'signer':n=='authority' and signer,'writable':n in ('result','source','destination','frozen') or mode==4 and n=='authority'} for n in (names if mode!=4 else names[:5]+['system','frozen'])]
        def instruction(ix,mode=0,signer=True,meta=None): return {'program':'program','accounts':refs(mode,signer) if meta is None else meta,'data':ix}
        def assertions(states): return [{'account':n,'state':s} for n,s in states.items()]
        rows,cases,expectations=[],[],[]
        # Native direct-call effects and runtime transaction rollback have separate
        # expectations: a failed second CPI leaves earlier native callback writes.
        def add(name,before,amount,mode=0,code=0,signer=True,seed_bump=bump,changes=None,calls=None,meta_changes=None):
            after=copy.deepcopy(before)
            if mode==4 and code==0:
                after['authority']['lamports']-=amount;after['destination']['lamports']+=amount
                after['result']['data']=struct.pack('<QQQ',before['authority']['lamports'],after['authority']['lamports'],after['destination']['lamports']).hex()
            elif code==0 or mode==2 and code==17:
                x=struct.unpack_from('<Q',bytes.fromhex(before['source']['data']),64)[0];y=struct.unpack_from('<Q',bytes.fromhex(before['destination']['data']),64)[0]
                auth=pda if mode in (1,3) else authority
                after['source']['data']=token_data(x-amount,auth);after['destination']['data']=token_data(y+amount,auth)
                after['result']['data']=struct.pack('<QQQ',x,x-amount,y+amount).hex()
            else:
                # Process writes the pre-CPI source balance after successful loads.
                if code not in (3010,3014):
                    x=struct.unpack_from('<Q',bytes.fromhex(before['source']['data']),64)[0]
                    after['result']['data']=struct.pack('<QQQ',x,0,0).hex()
            if changes is not None: after=changes
            ix=data(amount,mode,seed_bump);metas=refs(mode,signer)
            if meta_changes is not None: meta_changes(metas)
            accounts=[]
            for meta in metas:
                n=meta['account']
                if n in ('token','system'):
                    accounts.append({'Key':(TOKEN if n=='token' else bytes(32)).hex(),'Owner':bytes(32).hex(),'Data':'','Executable':True,'Signer':False,'Writable':False,'Lamports':10000000})
                else:
                    s=before[n];owner=TOKEN if s['owner']==b58(TOKEN) else program_bytes if s['owner']==program else bytes(32)
                    accounts.append({'Key':addresses[n].hex(),'Owner':owner.hex(),'Data':s['data'],'Executable':s['executable'],'Signer':meta['signer'],'Writable':meta['writable'],'Lamports':s['lamports']})
            rows.append({'Name':name,'ID':program_bytes.hex(),'Instruction':ix,'Accounts':accounts})
            expected_data=[after[n]['data'] if n in after else '' for n in [m['account'] for m in metas]]
            expected_lamports=[after[n]['lamports'] if n in after else 10000000 for n in [m['account'] for m in metas]]
            expectations.append({'Name':name,'Code':code,'Data':expected_data,'Lamports':expected_lamports,'Calls':calls if calls is not None else 2 if mode==2 else 1 if code in (0,1,14,17) and not (mode in (1,3) and code==1) else 0})
            committed=before if code else after
            cases.append({'name':name,'overrides':[{'name':n,'state':s} for n,s in before.items()],
                          'steps':[{'instructions':[instruction(ix,mode,signer,metas)],'expect':{'error':None if code==0 else {'InstructionError':[0,{'Custom':code}]},'accounts':assertions(committed)}}]})

        rng=random.Random(232026)
        for mode in (0,1,3):
            for sample in range(32):
                amount=rng.randrange(1,1<<63);x=rng.randrange(amount,1<<64);y=rng.randrange(MAX-amount+1)
                add(f'mode-{mode}-wide-{sample:02}',initial(x,y,mode),amount,mode)
        for mode in (0,1,3): add(f'mode-{mode}-zero',initial(mode=mode),0,mode)
        add('failed-second-cpi-rollback',initial(),10,2,17)
        add('insufficient-real-cpi',initial(x=9),10,0,1)
        add('overflow-real-cpi',initial(y=MAX),10,0,14)
        add('missing-signature-helper',initial(),10,0,3013,False)
        before=initial();b=bytearray.fromhex(before['source']['data']);b[108]=2;before['source']['data']=b.hex();add('frozen-first-cpi',before,10,0,17)
        for offset,code in ((108,3010),(109,3010),(0,3012),(32,3013)):
            before=initial();b=bytearray.fromhex(before['source']['data']);b[offset]^=1;before['source']['data']=b.hex();add(f'bad-source-{offset}',before,10,0,code)
        before=initial();before['source']['data']=before['source']['data'][:-2];add('short-token',before,10,0,3010)
        before=initial();before['source']['owner']=b58(bytes(32));add('wrong-token-owner',before,10,0,3010)
        add('readonly-token',initial(),10,0,3014,meta_changes=lambda metas:metas[2].update(writable=False))
        add('wrong-token-program',initial(),10,0,2001,meta_changes=lambda metas:metas[5].update(account='system'))
        bad_bump=(bump-1)%256
        bad_address=hashlib.sha256(addresses['result']+struct.pack('<I',0x12345678)+struct.pack('<Q',0x1122334455667788)+bytes([bad_bump])+program_bytes+b'ProgramDerivedAddress').digest()
        add('bad-PDA-bump',initial(mode=1),10,1,1 if on_curve(bad_address) else 4002,seed_bump=bad_bump,calls=0)
        for amount in (1,1000,9000000): add(f'generic-System-transfer-{amount}',initial(),amount,4)
        save(output/'native-input.json',rows);save(output/'independent-expectations.json',expectations)
        run('native-expectations',[native],json.dumps(rows).encode());observed=load(output/'native-expectations.log')
        require(observed==expectations,'native direct-call effects differ from independent wire/math/error expectations');save(output/'native-expectations.json',observed)
        before=initial();ix=data(10,0)
        cases.append({'name':'successful-transfer-then-invalid-dispatch-rollback','steps':[{'instructions':[instruction(ix),instruction(data(1,9))],'expect':{'error':{'InstructionError':[1,{'Custom':4003}]},'accounts':assertions(before)}}]})
        suite={'format':'gosvm-svm-fixtures-v1','program_id':program,'payer_seed':hashlib.sha256(b'SDK2 payer; never fund').hexdigest(),
               'signers':[{'name':'authority','seed':seed}], 'native_programs':[{'name':'system','address':b58(bytes(32))}],
               'accounts':[{'name':n,'address':b58(addresses[n]),'initial':s} for n,s in initial().items()]+[{'name':'token','address':b58(TOKEN)}], 'cases':cases}
        fixture=output/'fixtures.json';save(fixture,suite)
        llvm=args.llvm.resolve();elf=output/'program.so'
        run('sbf-build',[cli,'-arch','v3','-no-cache','-llvm',llvm,'-o',elf,ROOT/'examples/checked-token'])
        run('emit-c',[cli,'-arch','v3','-emit-c','-o',output/'program.c',ROOT/'examples/checked-token'])
        run('stack-compile',[llvm/'bin/clang','-target','sbf','-mcpu=v3','-O2','-fno-builtin','-fPIC','-fno-stack-protector','-std=c11','-Werror','-DGOSVM_SBF_V3=1','-fstack-usage','-c',output/'program.c','-o',stage/'stack.o'])
        run('stack-link',[llvm/'bin/ld.lld','-z','notext','-shared','--Bdynamic','--strip-all','--entry','entrypoint','--script',ROOT/'internal/compiler/sbf-v3.ld','--no-undefined','-o',stage/'stack.so',stage/'stack.o'])
        require(digest(elf)==digest(stage/'stack.so'),'stack instrumentation changed the tested ELF');shutil.copyfile(stage/'stack.su',output/'stack-usage.tsv')
        frames=[]
        for line in (output/'stack-usage.tsv').read_text().splitlines():
            name,size,kind=line.split('\t');frames.append({'function':name.rsplit(':',1)[-1],'bytes':int(size),'kind':kind})
        require(frames and all(f['kind']=='static' and f['bytes']<=4096 for f in frames),'fixture exceeds SBF frame budget')
        for sample in range(3):
            report=output/f'svm-{sample}.json';run(f'svm-{sample}',[cli,'svm-test','-elf',elf,'-fixtures',fixture,'-report',report]);actual=load(report)
            require(actual['elf_sha256']==digest(elf),'runner executed a different ELF')
            if sample: require(actual['cases']==load(output/'svm-0.json')['cases'],'case/CU/log results changed between repetitions')
        require(hashes=={str(p.relative_to(ROOT)):digest(p) for p in sorted(set(paths))},'sources changed during verification')
        summary.update(passed=True,native_vectors=len(rows),native_c_boundary_vectors=1011,scenarios=len(cases),samples=3,
                       expected_failures=sum(s['expect']['error'] is not None for c in cases for s in c['steps']),elf_bytes=elf.stat().st_size,elf_sha256=digest(elf),stack_frames=frames,
                       runner_sha256=actual['runner_sha256'],runtime_version=actual['runtime_version'],fixture_sha256=digest(fixture))
        print(f'PASS: {len(cases)} SDK-2 SBF scenarios in three repetitions. {output}',flush=True)
    except Exception as error:
        summary['failure']=str(error);raise
    finally: save(output/'summary.json',summary)


if __name__=='__main__': main()
