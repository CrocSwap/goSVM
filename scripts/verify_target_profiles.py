#!/usr/bin/env python3
"""Local target qualification: unchanged application fixtures, fresh result directory."""
import argparse, hashlib, json, os, pathlib, platform, shutil, subprocess
ROOT=pathlib.Path(__file__).resolve().parents[1]
MANIFEST_SHA='ec18012f5bb98cd19813b721f2ec343d628c679052828a3eb122ca1b335bc6de'
def sha(path): return hashlib.sha256(path.read_bytes()).hexdigest()
def main():
    ap=argparse.ArgumentParser(description=__doc__)
    ap.add_argument('--output',type=pathlib.Path,required=True)
    ap.add_argument('--cli',type=pathlib.Path,default=ROOT/'build/target-gosvm')
    ap.add_argument('--v0-runner',type=pathlib.Path,required=True)
    ap.add_argument('--v3-runner',type=pathlib.Path,required=True)
    ap.add_argument('--bend-manifest',type=pathlib.Path)
    ap.add_argument('--bend-sha256',default=MANIFEST_SHA)
    ap.add_argument('--repetitions',type=int,default=3)
    a=ap.parse_args(); a.output=a.output.resolve();a.cli=a.cli.resolve()
    if a.repetitions<1: ap.error('repetitions must be positive')
    a.output.mkdir(parents=True,exist_ok=False)
    env=dict(os.environ);env.update(GOWORK='off',GOTOOLCHAIN='local',GOPROXY='off',GOSUMDB='off')
    env.setdefault('GOCACHE',str(ROOT/'build/target-go-cache'))
    summary={'schema':1,'network_validation':False,'host':platform.platform(),'cli_sha256':sha(a.cli),'runs':[],'source_sha256':{}}
    sources=[]
    for directory in ['cmd/gosvm','internal/compiler','internal/project','internal/library','internal/targets','internal/testvm','internal/sbftest','solana','benchmarks/svm-runner-modern']:
        sources.extend(p for p in (ROOT/directory).rglob('*') if p.is_file() and 'target' not in p.relative_to(ROOT/directory).parts)
    sources.extend([ROOT/'scripts/build_modern_runner.py',ROOT/'scripts/verify_target_profiles.py',ROOT/'go.mod'])
    summary['source_sha256']={str(p.relative_to(ROOT)):sha(p) for p in sorted(sources)}
    logindex=0
    def run(args,expect_failure=False):
        nonlocal logindex
        logindex+=1;log=a.output/f'{logindex:02d}.log'
        with log.open('w') as out:
            status=subprocess.run([str(a.cli),*map(str,args)],cwd=ROOT,env=env,stdout=out,stderr=subprocess.STDOUT).returncode
        if (status==0)==expect_failure: raise RuntimeError(f'Unexpected status {status}: see {log}')
        return log.read_text()
    def collect(label,profile,report):
        data=json.loads(report.read_text());assert data['network_validation'] is False
        assert data['target_profile']==profile and data['platform_tools']=='v1.57'
        copied=a.output/f'{len(summary["runs"]):02d}-{label}-{profile}.raw.json';shutil.copyfile(report,copied)
        summary['runs'].append({'workload':label,'target':profile,'report_sha256':sha(copied),
            'elf_sha256':data['elf_sha256'],'fixtures_sha256':data['fixtures_sha256'],
            'runtime_version':data['runtime_version'],'runner_sha256':data['runner_sha256'],
            'cases':[{**{k:v for k,v in c.items() if k in ('name','cu','error','committed','passed')}, **({'steps':[{k:v for k,v in step.items() if k in ('cu','error','committed')} for step in c['steps']]} if 'steps' in c else {})} for c in data['cases']],'runtime_info_sha256':hashlib.sha256(json.dumps(data['runner'],sort_keys=True).encode()).hexdigest()})
    def project_test(directory,profile,runner,fixture,label):
        for _ in range(a.repetitions):
            text=run(['test','-target',profile,'-dir',directory,'-svm','-svm-fixtures',fixture,'-svm-runner',runner,'--','-ldflags=-linkmode=external'])
            built=next(line.split('Built ',1)[1].split(' (',1)[0] for line in text.splitlines() if line.startswith('Built '))
            collect(label,profile,pathlib.Path(built).parent/'svm-results.json')
    swap=ROOT/'examples/full-swap'
    before={str(p.relative_to(swap)):sha(p) for p in swap.rglob('*') if p.is_file() and 'build' not in p.relative_to(swap).parts and '.git' not in p.parts}
    for profile,runner in [('fogo-v0',a.v0_runner.resolve()),('solana-v3',a.v3_runner.resolve())]:
        project_test(swap,profile,runner,swap/'testdata/svm.json','token-swap')
        project_test(swap,profile,runner,swap/'testdata/lifecycle.json','managed-lifecycle')
        elf=ROOT/f'build/target-profiles/clock-{profile}.so';elf.parent.mkdir(parents=True,exist_ok=True)
        run(['-target',profile,'-o',elf,ROOT/'examples/clock2'])
        fixtures=ROOT/'results/compiler/2026-10-05-clock2-supported/fixtures.json'
        for _ in range(a.repetitions):
            report=a.output/f'clock-{profile}.json'
            run(['svm-test','-target',profile,'-elf',elf,'-fixtures',fixtures,'-runner',runner,'-report',report])
            collect('clock',profile,report)
    after={str(p.relative_to(swap)):sha(p) for p in swap.rglob('*') if p.is_file() and 'build' not in p.relative_to(swap).parts and '.git' not in p.parts}
    assert before==after,'Target selection changed application/generated interfaces'
    summary['unchanged_application_sha256']=before
    if a.bend_manifest:
        program=ROOT/'build/bend-consumer-qualification'/a.output.name/'program'
        run(['new','-target','fogo-v0','-module','example.com/bend-mixed',program])
        run(['library','bind','-manifest',a.bend_manifest.resolve(),'-sha256',a.bend_sha256,'-allow-library-tools','v1.53','-package','bend','-function','Evaluate','-output',program/'bend'])
        for name in ['program.go','program_test.go']:
            shutil.copyfile(ROOT/'tests/bend-library'/f'{name}.txt',program/name)
        shutil.copyfile(ROOT/'tests/bend-library/sbf.json',program/'testdata/sbf.json')
        for _ in range(a.repetitions):
            text=run(['test','-dir',program,'-svm','-svm-runner',a.v0_runner.resolve(),'--','-ldflags=-linkmode=external','-count=1'])
            built=next(line.split('Built ',1)[1].split(' (',1)[0] for line in text.splitlines() if line.startswith('Built '))
            collect('go-bend-mixed','fogo-v0',pathlib.Path(built).parent/'svm-results.json')
        rejection=run(['build','-target','solana-v3','-dir',program],expect_failure=True)
        assert 'no compatible solana-v3 object' in rejection
        summary['bend']={'producer_manifest_sha256':a.bend_sha256,'producer_platform_tools':'v1.53','caller_platform_tools':'v1.57','solana_v3_refused':True,'native_cgo_tests':True}
    summary['passed']=True
    (a.output/'metrics.json').write_text(json.dumps(summary,indent=2)+'\n')
    print('Local qualification passed:',a.output)
if __name__=='__main__': main()
