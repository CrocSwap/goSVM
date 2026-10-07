#!/usr/bin/env python3
"""Rebuild and verify the optimized owner-side Whirlpools snapshot.

Replays unchanged scoped/full Rust handlers and a pinned expanded component
corpus. All writes are confined to goSVM; benchmark sources/pins remain frozen.
"""
import argparse
import copy
import hashlib
import json
import os
from pathlib import Path
import random
import re
import shutil
import statistics
import struct
import subprocess
import tarfile
import time

ROOT=Path(__file__).resolve().parents[1]
BENCH=ROOT.parent/'goSVM-benchmarks'
FRONTEND=ROOT/'build/packed-store/2026-10-06-packed-store-canonical-final'
REFERENCE=ROOT/'results/compiler/2026-10-06-packed-store-canonical-final'
HANDLER_FIXTURE=ROOT/'results/compiler/2026-10-06-compact-signer-canonical-final/handler.json'
CANDIDATE=ROOT/'build/whirlpool-math/2026-10-06-whirlpool-validation-minimal/reuse-validation-full-scan'
CANDIDATE_PROOF=ROOT/'results/compiler/2026-10-06-whirlpool-validation-minimal'


def sha(p):
    return hashlib.sha256(p.read_bytes()).hexdigest()


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output',required=True,type=Path)
    args=parser.parse_args()
    out=args.output.resolve();stage=ROOT/'build/whirlpool-optimized'/out.name
    assert out.is_relative_to(ROOT/'results') and not out.exists() and not stage.exists()
    out.mkdir(parents=True);stage.mkdir(parents=True)
    cli=FRONTEND/'gosvm';llvm=Path.home()/'.cache/solana/v1.51/platform-tools/llvm'
    candidate_proof=json.loads((CANDIDATE_PROOF/'summary.json').read_text())
    assert candidate_proof['passed']
    candidate=candidate_proof['variants']['reuse-validation-full-scan']
    summary=dict(passed=False,scope='O2, fixed-fee classic-token Whirlpools handler, events omitted; new owner-side application snapshot',checks=[],handler={},components={})
    component_dir=BENCH/'build/profile/2026-10-06-wide-division-expanded'
    component_proof_path=BENCH/'results/whirlpool/2026-10-06-wide-division-expanded/summary.json'
    component_proof=json.loads(component_proof_path.read_text());assert component_proof['passed']
    rust_reference=BENCH/'results/whirlpool/2026-10-06-compact-signer-v1-verified'
    rust_proof=json.loads((rust_reference/'summary.json').read_text());assert rust_proof['passed']
    verified=BENCH/'build/verification/2026-10-06-fee-growth-handler'
    runner=next((verified/'cache').rglob('gosvm-svm-runner'));maker=verified/'transaction-maker'
    rust_elf=verified/'rust/whirlpool_handler_probe.so';upstream_elf=verified/'upstream/whirlpool.so'
    component_rust=component_dir/'rust/whirlpool_component_probe.so'
    assert sha(rust_elf)==rust_proof['elf']['rust']['sha256']
    assert sha(upstream_elf)==rust_proof['elf']['upstream']['sha256']
    assert sha(component_rust)==component_proof['elf']['rust']['sha256']
    assert sha(component_dir/'components.json')==component_proof['fixture_sha256']
    inputs=[Path(__file__),cli,CANDIDATE_PROOF/'summary.json',HANDLER_FIXTURE,REFERENCE/'canonical-0.json',BENCH/'scripts/run_budget_suite.py',ROOT/'internal/testvm/profiles/validator-3.0.15.json',rust_elf,upstream_elf,component_rust,component_proof_path,component_dir/'components.json',BENCH/'programs/whirlpool/go-handler/profile/program.go']
    for folder in (CANDIDATE/'handler',CANDIDATE/'go'):
        inputs += [p for p in folder.rglob('*') if p.is_file() and 'build' not in p.relative_to(folder).parts]
    for language in ('rust','upstream'):
        inputs += [rust_reference/(language+'-'+str(i)+'.json') for i in range(3)]
    summary['input_sha256']={str(p):sha(p) for p in inputs}
    env=dict(os.environ,GOCACHE=str(ROOT/'build/lifecycle-go-cache'),GOWORK='off',GOTOOLCHAIN='local',GOPROXY='off',GOSUMDB='off',PYTHONDONTWRITEBYTECODE='1')

    def save():
        (out/'summary.json').write_text(json.dumps(summary,indent=2)+'\n')

    def run(name,command,cwd=ROOT):
        start=time.perf_counter()
        with (out/(name+'.log')).open('wb') as log:
            p=subprocess.run([str(v) for v in command],cwd=cwd,env=env,stdout=log,stderr=subprocess.STDOUT)
        summary['checks'].append(dict(name=name,command=[str(v) for v in command],cwd=str(cwd),exit_code=p.returncode,seconds=time.perf_counter()-start));save()
        assert p.returncode==0,name+': see log'
        print(name+': PASS',flush=True)

    def suite(name,elf,fixture):
        report=out/(name+'.json')
        run(name,['python3',BENCH/'scripts/run_budget_suite.py','--runner',runner,'--maker',maker,'--elf',elf,'--features',ROOT/'internal/testvm/profiles/validator-3.0.15.json','--fixtures',fixture,'--report',report])
        data=json.loads(report.read_text());assert data['passed'];return data

    try:
        for module in ('handler','go'):
            shutil.copytree(CANDIDATE/module,stage/module,ignore=shutil.ignore_patterns('build'))
        # Apply's public checked entry must remain; the private core has only
        # the checked wrapper and fully validating handler as its callers.
        swap=(stage/'go/swap/swap.go').read_text()
        handler=(stage/'go/swap/handler.go').read_text()
        assert 'func Apply(' in swap and 'wire.CheckTickArray(Array(a0, a1, a2, j), poolKey)' in swap
        assert swap.count('applyValidated(')==2 and handler.count('applyValidated(')==1
        assert 'wire.CheckTickArray(b, key)' in handler and 'func ApplyValidated' not in swap
        assert 'ReadTickFlag' not in (stage/'go/wire/wire.go').read_text()
        for module in ('go','handler'):
            run('native-'+module,['go','test','-ldflags=-linkmode=external','./...','-count=3','-v'],stage/module)
        elf=out/'handler.so';c=out/'handler.c';obj=out/'stack.o'
        run('handler-build',[cli,'-no-cache','-llvm',llvm,'-o',elf,stage/'handler'])
        assert sha(elf)==candidate['elf_sha256'], 'fresh CLI rebuild differs'
        run('emit-c',[cli,'-emit-c','-o',c,stage/'handler'])
        run('stack-compile',[llvm/'bin/clang','-target','sbf','-mcpu=v3','-O2','-fno-builtin','-fPIC','-fno-stack-protector','-std=c11','-Werror','-fstack-usage','-c',c,'-o',obj])
        run('stack-link',[llvm/'bin/ld.lld','-z','notext','-shared','--Bdynamic','--strip-all','--entry','entrypoint','--script',ROOT/'internal/compiler/sbf-v3.ld','--no-undefined','-o',out/'stack.so',obj])
        assert sha(elf)==sha(out/'stack.so')
        reports={}
        for language,path in [('go',elf),('rust',rust_elf),('upstream',upstream_elf)]:
            repeats=[]
            for i in range(3):
                actual=suite(language+'-'+str(i),path,HANDLER_FIXTURE)
                assert len(actual['cases'])==166
                if repeats:assert actual['cases']==repeats[0]['cases']
                if language!='go':
                    old=json.loads((rust_reference/(language+'-'+str(i)+'.json')).read_text())
                    assert actual['cases']==old['cases'], 'Rust reference changed'
                repeats.append(actual)
            reports[language]=repeats[0]
        baseline=json.loads((REFERENCE/'canonical-0.json').read_text())
        prefix='Program '+json.loads(HANDLER_FIXTURE.read_text())['program_id']+' consumed '
        def logs(xs):
            return [re.sub(r' of \d+ compute units$',' of <remaining> compute units',x) for x in xs if not x.startswith(prefix)]
        rows=[]
        for a,b,r,u in zip(reports['go']['cases'],baseline['cases'],reports['rust']['cases'],reports['upstream']['cases']):
            assert a['name']==b['name']==r['name']==u['name'] and len(a['steps'])==len(b['steps'])==len(r['steps'])==len(u['steps'])
            for s,t,v,w in zip(a['steps'],b['steps'],r['steps'],u['steps']):
                assert s['error']==t['error']==v['error']==w['error'] and s['committed']==t['committed']
                assert logs(s['logs'])==logs(t['logs']), 'Token consumed CU or behavior changed'
                rows.append(dict(name=a['name'],success=s['error'] is None,baseline_cu=t['cu'],go_cu=s['cu'],rust_cu=v['cu'],upstream_cu=w['cu'],saving=t['cu']-s['cu'],paired_ratio=s['cu']/v['cu']))
        for report in reports.values():
            for key in ('fixtures_sha256','runner_sha256','transaction_maker_sha256','features','runtime_version','compute_unit_limit'):
                assert report[key]==baseline[key],key
        successes=[r for r in rows if r['success']];failures=[r for r in rows if not r['success']]
        assert len(successes)==103 and len(failures)==63
        ratio=statistics.median(r['paired_ratio'] for r in successes)
        assert ratio<=1.33,'target not met'
        summary['handler']=dict(scenarios=166,successful=103,failed=63,repetitions=3,median_paired_ratio=ratio,elf_bytes=elf.stat().st_size,elf_sha256=sha(elf),max_static_frame=max(int(x.split('\t')[1]) for x in obj.with_suffix('.su').read_text().splitlines()),median_success_saving=statistics.median(r['saving'] for r in successes),median_success_percent=statistics.median(r['saving']/r['baseline_cu']*100 for r in successes),success_improve=sum(r['saving']>0 for r in successes),success_regress=sum(r['saving']<0 for r in successes),failure_improve=sum(r['saving']>0 for r in failures),failure_regress=sum(r['saving']<0 for r in failures),ratio_min=min(r['paired_ratio'] for r in successes),ratio_max=max(r['paired_ratio'] for r in successes))
        (out/'per-case-cu.json').write_text(json.dumps(rows,indent=2)+'\n');save()
        print('Handler target verified: '+str(ratio),flush=True)
        profile=stage/'handler/profile';profile.mkdir()
        shutil.copyfile(BENCH/'programs/whirlpool/go-handler/profile/program.go',profile/'program.go')
        profile_elf=out/'components.so'
        run('components-build',[cli,'-no-cache','-llvm',llvm,'-o',profile_elf,profile])
        fixture=json.loads((component_dir/'components.json').read_text())
        assert len(fixture['cases'])==component_proof['cases']==24713
        rng=random.Random(20261008)
        edges=[0,1,2,(1<<32)-1,1<<32,(1<<64)-1,1<<64,(1<<65)-1,1<<127,(1<<128)-1]
        triples=[(a,b,d) for a in edges for b in edges for d in edges]
        for i in range(1500):
            a,b,d=rng.getrandbits(128),rng.getrandbits(128),rng.getrandbits(128)
            if i%5==0:a&=(1<<64)-1;b&=(1<<64)-1;d&=(1<<64)-1
            if i%5==1:b=i%10000
            if i%5==2:a&=(1<<64)-1;b&=(1<<64)-1
            if i%5==3:a=i%10000;d&=(1<<64)-1
            triples.append((a,b,d))
        template=fixture['cases'][0]
        for index,(a,b,d) in enumerate(triples):
            for up in (False,True):
                code,value=0,0
                if not d:code=6006
                elif a*b>=(1<<128):code=6031
                else:
                    value,rem=divmod(a*b,d)
                    if up and rem:value+=1
                    if value>=(1<<128):code=6008;value=0
                data=bytes([7])+b''.join(v.to_bytes(16,'little') for v in (a,b,d,0))+struct.pack('<QII',0,0,0)+bytes([up,False])
                case=copy.deepcopy(template);case['name']='mul_div/owner-expanded-'+str(index)+'-'+str(int(up))
                step=case['steps'][0];step['instructions'][0]['data']=data.hex()
                step['expect']['error']=None if not code else {'InstructionError':[0,{'Custom':code}]}
                step['expect']['accounts'][0]['state']['data']=(value.to_bytes(16,'little')+bytes(24)).hex()
                fixture['cases'].append(case)
        fixtures=out/'components.json';fixtures.write_text(json.dumps(fixture,separators=(',',':'))+'\n')
        component_reports={}
        for language,path in [('go',profile_elf),('rust',component_rust)]:
            repeats=[]
            for i in range(3):
                actual=suite('components-'+language+'-'+str(i),path,fixtures)
                assert len(actual['cases'])==len(fixture['cases'])
                if repeats:assert actual['cases']==repeats[0]['cases']
                repeats.append(actual)
            component_reports[language]=repeats[0]
        for a,b in zip(component_reports['go']['cases'],component_reports['rust']['cases']):
            assert a['name']==b['name'] and len(a['steps'])==len(b['steps'])
            for s,t in zip(a['steps'],b['steps']):assert s['error']==t['error']
        summary['components']=dict(cases=len(fixture['cases']),preserved_cases=24713,new_muldiv_cases=2*len(triples),repetitions=3,fixture_sha256=sha(fixtures),go_elf_sha256=sha(profile_elf),rust_elf_sha256=sha(component_rust))
        assert all(sha(Path(p))==h for p,h in summary['input_sha256'].items()),'input drift'
        # Portable source modules plus the exact local frontend/SDK pin and
        # artifact: a separate experimental snapshot, not a default-pin update.
        snapshot=stage/'snapshot';snapshot.mkdir()
        for module in ('handler','go'):
            shutil.copytree(stage/module,snapshot/module,ignore=shutil.ignore_patterns('build','profile'))
        shutil.copyfile(cli,snapshot/'gosvm');shutil.copyfile(elf,snapshot/'handler.so')
        manifest=dict(scope=summary['scope'],handler=summary['handler'],components=summary['components'],files={str(p.relative_to(snapshot)):sha(p) for p in sorted(snapshot.rglob('*')) if p.is_file()},frontend_proof_sha256=sha(REFERENCE/'summary.json'))
        (snapshot/'manifest.json').write_text(json.dumps(manifest,indent=2)+'\n')
        archive=out/'optimized-snapshot.tar.gz'
        with tarfile.open(archive,'w:gz') as tar:tar.add(snapshot,arcname='whirlpool-optimized')
        with tarfile.open(archive,'r:gz') as tar:
            for name,want in manifest['files'].items():
                assert hashlib.sha256(tar.extractfile('whirlpool-optimized/'+name).read()).hexdigest()==want
        summary['snapshot_sha256']=sha(archive);summary['cli_sha256']=sha(cli)
        shutil.copyfile(__file__,out/'verify.py.txt');summary['passed']=True
        print('PASS: target, matched handlers, expanded SBF arithmetic, and snapshot archive verified.',flush=True)
    finally:save()


if __name__=='__main__':main()
