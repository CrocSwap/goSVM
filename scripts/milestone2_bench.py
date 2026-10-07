#!/usr/bin/env python3
"""Measure milestone-2 applications, preserving distinct swap-only denominators."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import random
import resource
import shutil
import statistics
import subprocess
import tarfile
import time

ROOT=Path(__file__).resolve().parents[1]

def load(p):return json.loads(p.read_text())
def save(p,v):p.write_text(json.dumps(v,indent=2)+'\n')
def sha(p):return hashlib.sha256(p.read_bytes()).hexdigest()
def require(v,m):
    if not v:raise RuntimeError(m)
def footprint(path):
    files=[path] if path.is_file() else [p for p in path.rglob('*') if p.is_file() and not p.is_symlink()]
    return {'files':len(files),'logical_bytes':sum(p.stat().st_size for p in files),'allocated_bytes':sum(p.stat().st_blocks*512 for p in files)}
def tree(path):return {str(p.relative_to(path)):sha(p) for p in sorted(path.rglob('*')) if p.is_file() and 'build' not in p.relative_to(path).parts}
def stats(values):return {'median_seconds':statistics.median(values),'min_seconds':min(values),'max_seconds':max(values),'samples_seconds':values}


def main():
    parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--output',required=True,type=Path)
    parser.add_argument('--samples',type=int,default=5);parser.add_argument('--llvm',type=Path,default=Path.home()/'.cache/solana/v1.51/platform-tools/llvm')
    args=parser.parse_args();require(3<=args.samples<=20,'samples must be 3..20')
    out=args.output.resolve();stage=ROOT/'build/milestone2-bench'/out.name
    require(not out.exists() and not stage.exists(),'choose new evidence/staging directories');out.mkdir(parents=True);stage.mkdir(parents=True)
    (out/'tools').mkdir();(out/'inputs').mkdir();(stage/'tmp').mkdir()
    paths=[Path(__file__).resolve(),ROOT/'scripts/build-cli.sh',ROOT/'go.mod']
    for folder in ('cmd/gosvm','internal/compiler','internal/project','internal/runner','internal/testvm','internal/sbftest','sdk','solana','benchmarks/anchor/go-token','examples/full-swap','examples/escrow'):
        paths.extend(p for p in (ROOT/folder).rglob('*') if p.is_file() and p.suffix!='.md' and 'build' not in p.relative_to(ROOT/folder).parts)
    source_hashes={str(p.relative_to(ROOT)):sha(p) for p in sorted(set(paths))}
    env=dict(os.environ,GOCACHE=str(stage/'go-cache'),GOSVM_CACHE=str(stage/'cache'),TMPDIR=str(stage/'tmp'),GOPROXY='off',GOSUMDB='off',GOWORK='off',GOTOOLCHAIN='local')
    for name in ('GOFLAGS','GOSVM_TEST_FEATURES','GOSVM_TEST_RUNNER','SBF_LLVM'):env.pop(name,None)
    summary={'schema':1,'passed':False,'machine':platform.platform(),'samples':args.samples,'source_sha256':source_hashes,'setup':{},'rows':[],
             'limitations':['Local macOS arm64 observations with warm OS caches and installed v1.51 LLVM; not portable promises',
                            'Fresh native cache is measured for CLI bootstrap, then shared; project-clean is not a cold machine',
                            'Manual swap is a direct compiler command; project commands also validate and generate schema artifacts',
                            'Historical swap-only CLI/source is deliberately pinned; frontend/SDK improvements and lifecycle additions are distinct effects',
                            'Complete swap includes four instructions; manual and historical generated comparators provide swap only',
                            'Escrow has no matched Rust counterpart in this experiment',
                            'No downloads, validator or Cargo build in measured paths; prior Rust benchmarks remain separately scoped',
                            'Native tests differ by application; no matched native-suite speedup is claimed',
                            'Logical and allocated disk bytes are distinguished; compiler temp peak is sampled in separate untimed probes',
                            'No public release, external developer trial or current RPC rent quotation is implied']}
    counter=0
    def run(label,command,*,cwd=ROOT,profiles=False,measure=True):
        nonlocal counter;counter+=1;command=[str(v) for v in command];log=out/f'{counter:03d}-{label}.log';start=time.perf_counter();cpu=resource.getrusage(resource.RUSAGE_CHILDREN);before=os.getloadavg()
        with log.open('wb') as f:done=subprocess.run(command,cwd=cwd,env=env,stdout=f,stderr=subprocess.STDOUT,timeout=240)
        after=resource.getrusage(resource.RUSAGE_CHILDREN);row={'label':label,'command':command,'cwd':str(cwd),'wall_seconds':time.perf_counter()-start,'cpu_seconds':after.ru_utime+after.ru_stime-cpu.ru_utime-cpu.ru_stime,'load_average_before':before,'exit_code':done.returncode,'log':log.name}
        require(done.returncode==0,f'{label}: see {log}')
        if profiles:
            reports=[json.loads(s[len('gosvm timings: '):]) for s in log.read_text().splitlines() if s.startswith('gosvm timings: ')]
            require(len(reports)==1 and reports[0]['passed'],'missing/failed phase timings');row['phases']=reports[0]
        if measure:summary['rows'].append(row)
        print(f'{label}: {row["wall_seconds"]:.3f}s',flush=True);return row,log
    try:
        cli=out/'tools/gosvm';summary['setup']['cli_fresh_cache']=run('cli-fresh-cache',['bash','scripts/build-cli.sh',cli],measure=False)[0]
        summary['setup']['go_cache_after_bootstrap']=footprint(stage/'go-cache')
        cli_hash=sha(cli);summary['setup']['cli_warm_cache']=run('cli-warm-cache',['bash','scripts/build-cli.sh',cli],measure=False)[0];require(sha(cli)==cli_hash,'CLI bytes changed after warm build')
        archive=ROOT/'results/svm/2026-10-05-runner-install-complete/gosvm-svm-runner-0.5.0-darwin-arm64.tar.gz'
        summary['setup']['runner_install']=run('runner-install',[cli,'runner','install','-archive',archive],measure=False)[0]
        runner=stage/'cache/runners/0.5.0/darwin-arm64/bin/gosvm-svm-runner';require(runner.is_file(),'runner install path changed')
        pinned=load(ROOT/'internal/runner/pins/darwin-arm64.json');require(sha(runner)==pinned['files']['bin/gosvm-svm-runner']['sha256'],'runner pin drift')
        summary['tools']={'cli':{'sha256':sha(cli),**footprint(cli)},'runner':{'sha256':sha(runner),**footprint(runner)},'runner_archive':{'sha256':sha(archive),**footprint(archive)},
                          'backend':{n:{'sha256':sha((args.llvm/'bin'/n).resolve()),**footprint((args.llvm/'bin'/n).resolve())} for n in ('clang','ld.lld')},
                          'llvm_directory':footprint(args.llvm),'go_version':subprocess.check_output(['go','version'],text=True).strip(),
                          'clang_version':subprocess.check_output([str(args.llvm/'bin/clang'),'--version'],text=True).splitlines()[0]}
        oldroot=ROOT/'build/framework-swap/2026-10-05-framework-swap-lifecycle-final';oldcli=out/'tools/swap-only-gosvm';shutil.copyfile(oldroot/'gosvm',oldcli);oldcli.chmod(0o755)
        summary['tools']['historical_cli']={'sha256':sha(oldcli),**footprint(oldcli)}
        jobs={}
        sources={'generated_swap_only':oldroot/'full-swap','managed_swap':ROOT/'examples/full-swap','escrow':ROOT/'examples/escrow'}
        evidence={'generated_swap_only':ROOT/'results/compiler/2026-10-05-framework-swap-lifecycle-final','managed_swap':ROOT/'results/compiler/2026-10-06-framework-swap-managed','escrow':ROOT/'results/compiler/2026-10-06-escrow-token-lifecycle'}
        for name,src in sources.items():
            dst=stage/name;shutil.copytree(src,dst,ignore=shutil.ignore_patterns('build','__pycache__'))
            jobs[name]={'dir':dst,'cli':oldcli if name=='generated_swap_only' else cli,'expected':load(evidence[name]/'summary.json')['elf_sha256'],'source':dst/'program.go','test':dst/'model/quote_test.go' if name!='escrow' else dst/'program_test.go','elf':dst/'build/program.so','proof':evidence[name]}
        manual=stage/'manual_swap';manual.mkdir();shutil.copyfile(ROOT/'benchmarks/anchor/go-token/swap.go',manual/'swap.go')
        oracle=ROOT/'results/svm/2026-10-05-general-complete';reference=load(oracle/'general-0.json')
        jobs['manual_swap']={'dir':manual,'cli':cli,'expected':reference['elf_sha256'],'source':manual/'swap.go','elf':manual/'build/program.so','proof':oracle}
        baseline_trees={name:tree(job['dir']) for name,job in jobs.items()};summary['input_sha256']=baseline_trees
        for name,job in jobs.items():
            with tarfile.open(out/'inputs'/f'{name}.tar.gz','w:gz') as archivefile:
                for file in sorted(job['dir'].rglob('*')):
                    if file.is_file():archivefile.add(file,arcname=file.relative_to(job['dir']))
        def build_cmd(name):
            j=jobs[name]
            if name=='manual_swap':return [j['cli'],'-arch','v3','-llvm',args.llvm,'-o',j['elf'],j['source']]
            return [j['cli'],'build','-dir',j['dir'],'-llvm',args.llvm,'-timings']
        originals={n:j['source'].read_bytes() for n,j in jobs.items()};test_originals={n:j['test'].read_bytes() for n,j in jobs.items() if 'test' in j}
        generated={n:{rel:sha(j['dir']/rel) for rel in ('zz_gosvm.go','client/zz_gosvm.go','idl.json','layouts.json')} for n,j in jobs.items() if n!='manual_swap'}
        for name,j in jobs.items():
            run(name+'-initial-build',build_cmd(name),profiles=name!='manual_swap',measure=False);require(sha(j['elf'])==j['expected'],'initial ELF differs from authoritative proof '+name)
            if name!='manual_swap':run(name+'-initial-native',[j['cli'],'test','-dir',j['dir'],'--','-count=1'],measure=False)
        rng=random.Random(20261006)
        for scenario in ('project_clean_build','source_edit_build','no_op_build'):
            for sample in range(args.samples):
                names=list(jobs);rng.shuffle(names)
                for name in names:
                    j=jobs[name]
                    if scenario=='project_clean_build':shutil.rmtree(j['dir']/'build',ignore_errors=True)
                    elif scenario=='source_edit_build':j['source'].write_bytes(originals[name]+f'\n// benchmark comment {sample}\n'.encode())
                    before=j['elf'].stat().st_mtime_ns if j['elf'].exists() else None
                    row,_=run(f'{name}-{scenario}-{sample}',build_cmd(name),profiles=name!='manual_swap');row.update(workload=name,scenario=scenario,sample=sample)
                    require(sha(j['elf'])==j['expected'],'edit/build changed ELF '+name)
                    require((j['elf'].stat().st_mtime_ns==before)==(scenario=='no_op_build'),'unexpected backend cache behavior '+name)
                    row['elf_sha256']=sha(j['elf']);row['retained_build']=footprint(j['dir']/'build')
        for name,j in jobs.items():
            j['source'].write_bytes(originals[name]);run(name+'-restored-build',build_cmd(name),profiles=name!='manual_swap',measure=False)
        for scenario in ('native_forced','native_test_edit'):
            for sample in range(args.samples):
                names=[n for n in jobs if n!='manual_swap'];rng.shuffle(names)
                for name in names:
                    j=jobs[name]
                    if scenario=='native_test_edit':j['test'].write_bytes(test_originals[name]+f'\n// native test edit {sample}\n'.encode())
                    before=j['elf'].stat().st_mtime_ns
                    row,log=run(f'{name}-{scenario}-{sample}',[j['cli'],'test','-dir',j['dir'],'-timings','--','-count=1'],profiles=True);row.update(workload=name,scenario=scenario,sample=sample)
                    require('(cached)' not in log.read_text(),'native tests unexpectedly cached')
                    require(j['elf'].stat().st_mtime_ns==before,'native test invalidated/rebuilt ELF')
        for name,original in test_originals.items():jobs[name]['test'].write_bytes(original)
        suites={
            'manual_swap':{'shared_swap':(oracle/'token-fixtures.json',oracle/'general-0.json')},
            'generated_swap_only':{'shared_swap':(jobs['generated_swap_only']['proof']/'fixtures.json',jobs['generated_swap_only']['proof']/'svm-0.json')},
            'managed_swap':{'shared_swap':(jobs['managed_swap']['proof']/'fixtures.json',jobs['managed_swap']['proof']/'svm-0.json'),
                            'lifecycle':(ROOT/'results/compiler/2026-10-06-managed-swap-supported/fixtures.json',ROOT/'results/compiler/2026-10-06-managed-swap-supported/svm-0.json')},
            'escrow':{'lifecycle':(jobs['escrow']['proof']/'fixtures.json',jobs['escrow']['proof']/'svm-0.json')}}
        for sample in range(args.samples):
            pairs=[(n,s,files) for n,data in suites.items() for s,files in data.items()];rng.shuffle(pairs)
            for name,suite,(fixtures,expected_path) in pairs:
                j=jobs[name];report=out/f'{name}-{suite}-svm-{sample}.json'
                row,_=run(f'{name}-{suite}-svm-{sample}',[cli,'svm-test','-elf',j['elf'],'-fixtures',fixtures,'-runner',runner,'-report',report]);row.update(workload=name,scenario='svm_'+suite,sample=sample)
                actual=load(report);expected=load(expected_path)
                require(actual['cases']==expected['cases'],'SBF case/CU/log drift '+name+' '+suite);require(actual['elf_sha256']==j['expected'],'another ELF ran')
                require(actual['runner_sha256']==sha(runner),'another runner ran')
                row.update(startup_seconds=actual['startup_seconds'],fixture_seconds=actual['fixture_seconds'],runner_execution_seconds=actual['runner']['execution_us']/1e6,report=report.name)
        for sample in range(args.samples):
            names=['managed_swap','escrow'];rng.shuffle(names)
            for name in names:
                j=jobs[name];fixture='lifecycle.json' if name=='managed_swap' else 'svm.json';report=j['dir']/'build/svm-results.json'
                row,_=run(f'{name}-integrated-test-{sample}',[cli,'test','--svm','-dir',j['dir'],'-llvm',args.llvm,'-svm-runner',runner,'-svm-fixtures',j['dir']/'testdata'/fixture,'-timings','--','-count=1'],profiles=True);row.update(workload=name,scenario='integrated_native_svm',sample=sample)
                actual=load(report);expected=load(suites[name]['lifecycle'][1]);require(actual['cases']==expected['cases'] and actual['elf_sha256']==j['expected'],'integrated result drift')
        # Separate peak probes keep disk polling out of timed distributions.
        summary['temporary_peak_probes']={}
        summary['setup']['shared_tmp_retained']=footprint(stage/'tmp')
        for name,j in jobs.items():
            probe_tmp=stage/('probe-tmp-'+name);probe_tmp.mkdir()
            probe_env=dict(env,TMPDIR=str(probe_tmp))
            command=build_cmd(name);command.insert(1 if name=='manual_swap' else 2,'-no-cache');log=out/f'{name}-temp-probe.log'
            peak={'logical_bytes':0,'allocated_bytes':0,'files':0};polls=0;start=time.perf_counter()
            with log.open('wb') as f:
                proc=subprocess.Popen([str(x) for x in command],cwd=ROOT,env=probe_env,stdout=f,stderr=subprocess.STDOUT)
                while proc.poll() is None:
                    try:
                        current=footprint(probe_tmp);peak={k:max(peak[k],current[k]) for k in peak};polls+=1
                    except FileNotFoundError:pass
                    require(time.perf_counter()-start<240,'temp probe timed out');time.sleep(.025)
                require(proc.returncode==0,'temp probe failed '+name)
            require(footprint(probe_tmp)['files']==0,'compiler temp files leaked');require(sha(j['elf'])==j['expected'],'uncached probe changed ELF')
            summary['temporary_peak_probes'][name]={'sampled_peak':peak,'polls':polls,'interval_seconds':.025,'log':log.name}
        summary['footprint']={'go_cache_final':footprint(stage/'go-cache'),'runner_install':footprint(stage/'cache'),'workloads':{}}
        for name,j in jobs.items():
            require(tree(j['dir'])==baseline_trees[name],'source/generated artifacts changed '+name)
            for rel,h in generated.get(name,{}).items():require(sha(j['dir']/rel)==h,'generator changed wire/client artifact '+name)
            summary['footprint']['workloads'][name]={'elf':footprint(j['elf']),'elf_sha256':sha(j['elf']),'retained_build':footprint(j['dir']/'build'),
                'project_total':footprint(j['dir']), 'source_files_excluding_build':len(baseline_trees[name]), 'sdk':footprint(j['dir']/'.gosvm/sdk') if name!='manual_swap' else None}
        summary['distributions']={}
        for name in jobs:
            summary['distributions'][name]={}
            for scenario in sorted({r['scenario'] for r in summary['rows'] if r.get('workload')==name}):
                rows=[r for r in summary['rows'] if r.get('workload')==name and r.get('scenario')==scenario]
                summary['distributions'][name][scenario]={'wall':stats([r['wall_seconds'] for r in rows]),'cpu':stats([r['cpu_seconds'] for r in rows])}
        require(source_hashes=={p:sha(ROOT/p) for p in source_hashes},'authoritative sources changed during experiment')
        summary['passed']=True;print('PASS: milestone-2 benchmark '+str(out),flush=True)
    except Exception as error:summary['failure']=str(error);raise
    finally:save(out/'summary.json',summary)

if __name__=='__main__':main()
