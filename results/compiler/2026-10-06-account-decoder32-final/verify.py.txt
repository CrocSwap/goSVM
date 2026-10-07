#!/usr/bin/env python3
"""Verify SDK-2's 32-account loader and preserved SDK-1/Phoenix baselines."""
import argparse
import copy
import gzip
import hashlib
import json
import os
from pathlib import Path
import shutil
import statistics
import subprocess
import tarfile
import time

ROOT=Path(__file__).resolve().parents[1]
BENCH=ROOT.parent/'goSVM-benchmarks'
BASE=BENCH/'results/phoenix-v1/2026-10-06-word-keys'
OLDCLI=ROOT/'build/packed-store/2026-10-06-packed-store-canonical-final/gosvm'

PROBE='''package probe
import "gosvm/solana"
func read(b []byte,o uint64)uint64{v:=uint64(0);for j:=uint64(0);j<8;j++{v=v|uint64(b[o+j])<<(8*j)};return v}
func put(b []byte,o,v uint64){for j:=uint64(0);j<8;j++{b[o+j]=byte(v>>(8*j))}}
func Process(c solana.Context)uint64{
 d:=solana.Instruction(c);if len(d)!=12{return 7300}
 n:=solana.Count(c);if n!=uint64(d[2]){return 7301}
 if d[0]==0{return 0}
 if d[0]==6 && n!=4{return 7310}
 if d[0]==3 || d[0]==4{
  if n<4{return 7303};src,dst,program:=n-3,n-2,n-1
  var metas [18]byte;put(metas[:],0,src);metas[8]=3;put(metas[:],9,dst);metas[17]=1
  var data [12]byte;data[0]=2;put(data[:],4,read(d,4))
  code:=solana.Invoke(c,program,metas[:],data[:],nil);if code!=0{return code}
  if d[0]==4{return 7337};return 0
 }
 index:=uint64(d[1]);if index>=n{return 7302}
 if !solana.Writable(c,index)||solana.Executable(c,index){return 7311}
 b:=solana.Data(c,index);if len(b)!=1{return 7312}
 if d[0]==2 || d[0]==5{
  other:=uint64(d[3]);if other>=n{return 7302}
  for j:=uint64(0);j<32;j++{if solana.Key(c,index)[j]!=solana.Key(c,other)[j]||solana.Owner(c,index)[j]!=solana.Owner(c,other)[j]{return 7313}}
  if solana.Signer(c,index)!=solana.Signer(c,other)||solana.Writable(c,index)!=solana.Writable(c,other){return 7313}
  before:=b[0];b[0]=before+1
  if solana.Data(c,other)[0]!=before+1{return 7314}
  if d[0]==5{return 7337};return 0
 }
 b[0]=99;return 0
}
'''


def sha(p):return hashlib.sha256(p.read_bytes()).hexdigest()
def b58(data):
    alphabet='123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz';n=int.from_bytes(data,'big');v=''
    while n:n,r=divmod(n,58);v=alphabet[r]+v
    return '1'*(len(data)-len(data.lstrip(b'\0')))+v


def main():
    parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--output',required=True,type=Path)
    args=parser.parse_args();out=args.output.resolve();stage=ROOT/'build/account-decoder'/out.name
    assert out.is_relative_to(ROOT/'results') and not out.exists() and not stage.exists()
    out.mkdir(parents=True);stage.mkdir(parents=True)
    reference=json.loads((BASE/'summary.json').read_text());assert reference['passed']
    source=BENCH/'programs/phoenix-v1/go'
    # Freeze the application exactly as measured; fail on agent/source drift.
    for name,want in reference['source_sha256'].items():
        if name.startswith('programs/phoenix-v1/go/'):
            assert sha(BENCH/name)==want,name
    inputs=[Path(__file__),OLDCLI,BASE/'summary.json',BASE/'fixtures.json.gz',BENCH/'scripts/run_phoenix_suite.py',BENCH/'results/phoenix-v1/2026-10-06-extra-metas-v2/fixture-rust.json',BENCH/'tools/phoenix-v1/runtime-features.json',Path(reference['runner_path']),Path(reference['maker_path'])]
    for folder in ('cmd','internal','sdk','solana'):
        inputs += [p for p in (ROOT/folder).rglob('*') if p.is_file()]
    inputs += [p for p in source.rglob('*') if p.is_file()]
    inputs += [BENCH/reference['elf'][name]['path'] for name in ('rust','upstream')]
    for name in ('rust','upstream'):
        assert sha(BENCH/reference['elf'][name]['path'])==reference['elf'][name]['sha256']
    summary=dict(passed=False,checks=[],source_sha256={str(p):sha(p) for p in inputs},scope='SDK-2 incoming accounts up to 32; SDK-1 16 and outgoing CPI meta/schema declared-account bounds preserved',artifacts={})
    env=dict(os.environ,GOCACHE=str(ROOT/'build/lifecycle-go-cache'),GOWORK='off',GOTOOLCHAIN='local',GOPROXY='off',GOSUMDB='off',PYTHONDONTWRITEBYTECODE='1')
    cli=stage/'gosvm';llvm=Path.home()/'.cache/solana/v1.51/platform-tools/llvm'

    def save():(out/'summary.json').write_text(json.dumps(summary,indent=2)+'\n')
    def run(name,command,cwd=ROOT):
        started=time.perf_counter()
        with (out/(name+'.log')).open('wb') as log:p=subprocess.run([str(x) for x in command],cwd=cwd,env=env,stdout=log,stderr=subprocess.STDOUT)
        summary['checks'].append(dict(name=name,command=[str(x) for x in command],cwd=str(cwd),exit_code=p.returncode,seconds=time.perf_counter()-started));save()
        assert p.returncode==0,name+': see log'
        print(name+': PASS',flush=True)
    def build(name,package,frontend=cli):
        c,obj,elf=out/(name+'.c'),out/(name+'.o'),out/(name+'.so')
        run(name+'-emit',[frontend,'-emit-c','-o',c,package])
        run(name+'-compile',[llvm/'bin/clang','-target','sbf','-mcpu=v3','-O2','-fno-builtin','-fPIC','-fno-stack-protector','-std=c11','-Werror','-fstack-usage','-c',c,'-o',obj])
        run(name+'-link',[llvm/'bin/ld.lld','-z','notext','-shared','--Bdynamic','--strip-all','--entry','entrypoint','--script',ROOT/'internal/compiler/sbf-v3.ld','--no-undefined','-o',elf,obj])
        frames=[dict(function=x.split('\t')[0],bytes=int(x.split('\t')[1]),kind=x.split('\t')[2]) for x in obj.with_suffix('.su').read_text().splitlines()]
        assert frames and all(x['bytes']<=4096 and x['kind']=='static' for x in frames),name+': frame limit'
        summary['artifacts'][name]=dict(bytes=elf.stat().st_size,sha256=sha(elf),max_static_frame=max(x['bytes'] for x in frames),frames=frames);save();return elf
    def suite(name,elf,fixture,repeats=3):
        rows=[]
        for i in range(repeats):
            report=out/(name+'-'+str(i)+'.json')
            run(name+'-'+str(i),['python3',BENCH/'scripts/run_phoenix_suite.py','--runner',reference['runner_path'],'--maker',reference['maker_path'],'--elf',elf,'--features',BENCH/'tools/phoenix-v1/runtime-features.json','--fixtures',fixture,'--report',report])
            actual=json.loads(report.read_text());assert actual['passed']
            if rows:assert actual['cases']==rows[0]['cases']
            rows.append(actual)
        return rows[0]
    try:
        run('native-decoder',['go','test','-ldflags=-linkmode=external','./internal/compiler','-run','TestSDKAccountDecoderCapacityAndAliases','-count=1','-v'])
        run('cli-build',['bash',ROOT/'scripts/build-cli.sh',cli])
        for sdk in (1,2):
            run('scaffold-sdk'+str(sdk),[cli,'new','-schema','2','-sdk',str(sdk),stage/('sdk'+str(sdk))])
            noop=stage/('sdk'+str(sdk))/'noop';noop.mkdir();(noop/'program.go').write_text('package noop\nimport "gosvm/solana"\nfunc Process(c solana.Context)uint64{return 0}\n')
            build('noop-sdk'+str(sdk),noop)
        old_noop=build('noop-sdk1-old',stage/'sdk1/noop',OLDCLI)
        assert sha(old_noop)==summary['artifacts']['noop-sdk1']['sha256'],'SDK-1 ELF changed'
        probe=stage/'sdk2/probe';probe.mkdir();(probe/'program.go').write_text(PROBE);probe_elf=build('probe',probe)
        # Use an independent signer: the runner funds/overwrites the fee payer.
        main_fixture=json.loads(gzip.decompress((BASE/'fixtures.json.gz').read_bytes()))
        maker=subprocess.Popen([reference['maker_path']],stdin=subprocess.PIPE,stdout=subprocess.PIPE,text=True)
        try:
            maker.stdin.write(json.dumps(dict(payer_seed=main_fixture['signer_seeds'][0],blockhash='11111111111111111111111111111111',limit=0,instructions=[]))+'\n');maker.stdin.flush()
            trader=json.loads(maker.stdout.readline())['payer']
        finally:maker.terminate();maker.wait()
        program_bytes=hashlib.sha256(b'goSVM 32-account decoder proof').digest();program=b58(program_bytes)
        def state(data=b'\x07',owner=program,lamports=1_000_000_000):return dict(data=data.hex(),owner=owner,lamports=lamports,executable=False,rent_epoch=0)
        accounts=[dict(name='a'+str(i),address=b58(hashlib.sha256(('Account decoder '+str(i)).encode()).digest()),initial=state(bytes([i+7]))) for i in range(33)]
        accounts += [dict(name='authority',address=trader,initial=state(b'','11111111111111111111111111111111')),dict(name='recipient',address=b58(hashlib.sha256(b'Account decoder recipient').digest()),initial=state(b'','11111111111111111111111111111111',100_000_000)),dict(name='system',address='11111111111111111111111111111111')]
        base=dict(format='gosvm-svm-fixtures-v1',program_id=program,payer_seed=main_fixture['payer_seed'],signer_seeds=main_fixture['signer_seeds'],accounts=accounts,cases=[])
        states={a['name']:a['initial'] for a in accounts if 'initial' in a}
        def case(name,n,mode=0,index=0,other=0,code=None,duplicate=False,readonly=False,amount=1):
            # Legacy transactions with 31+ unique pubkeys exceed the packet
            # limit. Exercise the descriptor boundary with valid duplicates;
            # native serialized tests independently cover 32 unique accounts.
            names=['a'+str(i if n<=20 else i%20) for i in range(n)]
            if duplicate:names[index]=names[other]
            if mode in (3,4):names[-3:]=['authority','recipient','system']
            metas=[dict(account=a,writable=a!='system',signer=a=='authority') for a in names]
            if readonly:
                for meta in metas:
                    if meta['account']==names[index]:meta['writable']=False
            ix=bytes([mode,index,n,other])+amount.to_bytes(8,'little')
            after={a:copy.deepcopy(states[a]) for a in dict.fromkeys(names) if a in states}
            if not after:after={'a0':copy.deepcopy(states['a0'])}
            if code is None:
                if mode in (1,6):after[names[index]]['data']='63'
                if mode==2:after[names[index]]['data']=bytes([(int(after[names[index]]['data'],16)+1)&255]).hex()
                if mode==3:after['authority']['lamports']-=amount;after['recipient']['lamports']+=amount
            expected=None if code is None else {'InstructionError':[0,{'Custom':code}]}
            return dict(name=name,steps=[dict(instructions=[dict(program='program',accounts=metas,data=ix.hex())],expect=dict(error=expected,accounts=[dict(account=a,state=s) for a,s in after.items()]))])
        noop=copy.deepcopy(base)
        for n in (0,1,4,16,17,31,32,33):noop['cases'].append(case('noop-'+str(n),n,code=1001 if n>32 else None))
        (out/'noop-sdk2.json').write_text(json.dumps(noop,separators=(',',':'))+'\n')
        legacy=copy.deepcopy(noop)
        for c in legacy['cases']:
            n=int(c['name'].split('-')[-1]);c['steps'][0]['expect']['error']=None if n<=16 else {'InstructionError':[0,{'Custom':1001}]}
        (out/'noop-sdk1.json').write_text(json.dumps(legacy,separators=(',',':'))+'\n')
        suite('noop-sdk2',out/'noop-sdk2.so',out/'noop-sdk2.json');suite('noop-sdk1',out/'noop-sdk1.so',out/'noop-sdk1.json')
        checked=copy.deepcopy(base)
        for n in (4,16,17,31,32,33):
            checked['cases'].append(case('last-account-'+str(n),n,1,n-1,code=1001 if n>32 else None))
            if n<=32:
                checked['cases'].append(case('readonly-last-'+str(n),n,1,n-1,code=7311,readonly=True))
                checked['cases'].append(case('strict-handler-'+str(n),n,6,0,code=None if n==4 else 7310))
                checked['cases'].append(case('alias-last-'+str(n),n,2,n-1,n-2,duplicate=True))
                checked['cases'].append(case('alias-rollback-'+str(n),n,5,n-1,n-2,code=7337,duplicate=True))
                for mode in (3,4):checked['cases'].append(case('signed-cpi-'+str(n)+'-'+str(mode),n,mode,code=7337 if mode==4 else None,amount=17))
                checked['cases'].append(case('failed-cpi-'+str(n),n,3,code=1,amount=2_000_000_000))
        (out/'probe-fixtures.json').write_text(json.dumps(checked,separators=(',',':'))+'\n')
        suite('probe',probe_elf,out/'probe-fixtures.json')
        phoenix=stage/'phoenix';shutil.copytree(source,phoenix,ignore=shutil.ignore_patterns('build','.gosvm'));shutil.copytree(stage/'sdk2/.gosvm/sdk',phoenix/'.gosvm/sdk')
        run('phoenix-native',['go','test','-ldflags=-linkmode=external','./...','-count=1'],phoenix)
        old=build('phoenix-old',phoenix,OLDCLI);assert sha(old)==reference['elf']['go']['sha256'],'Phoenix baseline differs'
        new=build('phoenix-new',phoenix)
        fixtures=out/'phoenix-fixtures.json';fixtures.write_text(json.dumps(main_fixture,separators=(',',':'))+'\n')
        extra=out/'phoenix-extra.json';shutil.copyfile(BENCH/'results/phoenix-v1/2026-10-06-extra-metas-v2/fixture-rust.json',extra)
        go=suite('phoenix-go',new,fixtures)
        refs={name:suite('phoenix-'+name,BENCH/reference['elf'][name]['path'],fixtures) for name in ('rust','upstream')}
        for name in ('rust','upstream'):suite('extra-'+name,BENCH/reference['elf'][name]['path'],extra)
        extra_go=suite('extra-go',new,extra)
        # The 16/17 reproduction has real expected header changes, not no-op outcomes.
        assert len(extra_go['cases'])==2 and all(x['steps'][0]['error'] is None for x in extra_go['cases'])
        baseline=json.loads((BASE/'go-0.json').read_text());deltas=[];ratios=[]
        for a,b,r,u in zip(go['cases'],baseline['cases'],refs['rust']['cases'],refs['upstream']['cases']):
            assert a['name']==b['name']==r['name']==u['name']
            for s,t,v,w in zip(a['steps'],b['steps'],r['steps'],u['steps']):
                assert s['error']==t['error']==v['error']==w['error'] and s['committed']
                deltas.append(t['cu']-s['cu'])
                if s['error'] is None:ratios.append(s['cu']/v['cu'])
        assert len(go['cases'])==430
        summary.update(incoming_sdk2_limit=32,incoming_sdk1_limit=16,probe_cases=len(checked['cases']),noop_cases=len(noop['cases']),phoenix_cases=430,repetitions=3,phoenix_median_paired_ratio=statistics.median(ratios),phoenix_cu_change_min=min(deltas),phoenix_cu_change_max=max(deltas),cli_sha256=sha(cli))
        assert all(sha(Path(p))==h for p,h in summary['source_sha256'].items()),'source drift'
        # Freeze the complete frontend/SDK for an independently owned new pin.
        frontend=stage/'frontend';frontend.mkdir();shutil.copy2(cli,frontend/'gosvm');shutil.copytree(stage/'sdk2/.gosvm/sdk',frontend/'sdk')
        frontend_source=frontend/'source';frontend_source.mkdir()
        for folder in ('cmd','internal','sdk','solana'):shutil.copytree(ROOT/folder,frontend_source/folder,ignore=shutil.ignore_patterns('__pycache__'))
        shutil.copy2(ROOT/'go.mod',frontend_source/'go.mod');(frontend_source/'scripts').mkdir();shutil.copy2(ROOT/'scripts/build-cli.sh',frontend_source/'scripts/build-cli.sh')
        manifest=dict(cli_sha256=sha(cli),incoming_sdk2_limit=32,incoming_sdk1_limit=16,files={str(p.relative_to(frontend)):sha(p) for p in sorted(frontend.rglob('*')) if p.is_file()})
        (frontend/'manifest.json').write_text(json.dumps(manifest,indent=2)+'\n')
        with tarfile.open(out/'frontend.tar.gz','w:gz') as tar:tar.add(frontend,arcname='frontend')
        with tarfile.open(out/'phoenix-source.tar.gz','w:gz') as tar:tar.add(phoenix,arcname='phoenix')
        summary['frontend_archive_sha256']=sha(out/'frontend.tar.gz');summary['phoenix_source_archive_sha256']=sha(out/'phoenix-source.tar.gz')
        shutil.copyfile(__file__,out/'verify.py.txt');summary['passed']=True
    finally:save()


if __name__=='__main__':main()
