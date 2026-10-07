#!/usr/bin/env python3
"""Isolated application math experiments against the frozen 1.70x handler.

Copies both Go modules and SDK into owner staging; never edits sibling sources.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import statistics
import subprocess
import time

ROOT = Path(__file__).resolve().parents[1]
BENCH = ROOT.parent / 'goSVM-benchmarks'
BASE = ROOT / 'results/compiler/2026-10-06-packed-store-canonical-final'
SOURCE = ROOT / 'build/compact-signer-canonical/2026-10-06-compact-signer-canonical-final'
FRONTEND = ROOT / 'build/packed-store/2026-10-06-packed-store-canonical-final'
FIXTURE = ROOT / 'results/compiler/2026-10-06-compact-signer-canonical-final/handler.json'

MULDIV_TEST = r'''package math
import("math/big"; "math/rand"; "testing")
func TestOptimizedMulDiv(t *testing.T) {
 rng:=rand.New(rand.NewSource(20261008))
 check:=func(a,b,d U128){
  t.Helper()
  for _,up:=range []bool{false,true}{
   got,code:=MulDiv(a,b,d,up)
   var old U128;var err uint64
   p:=Mul128(a,b)
   if d.IsZero(){err=DivideByZero}else if p.L[2]!=0||p.L[3]!=0{err=MulDivOverflow}else{old,err=DivRound(p,d.Wide(),up)}
   if got!=old||code!=err{t.Fatalf("a=%+v b=%+v d=%+v up=%v got=%+v/%d old=%+v/%d",a,b,d,up,got,code,old,err)}
   product:=new(big.Int).Mul(big128(a),big128(b))
   wantErr:=uint64(0);want:=new(big.Int)
   if d.IsZero(){wantErr=DivideByZero}else if product.BitLen()>128{wantErr=MulDivOverflow}else{
    rem:=new(big.Int);want.QuoRem(product,big128(d),rem)
    if up&&rem.Sign()!=0{want.Add(want,big.NewInt(1))}
    if want.BitLen()>128{wantErr=NumberDownCast}
   }
   if code!=wantErr||(code==0&&big128(got).Cmp(want)!=0){t.Fatal("math/big mismatch",a,b,d,up,got,code,want,wantErr)}
  }
 }
 edges:=[]U128{{},{Lo:1},{Lo:2},{Lo:1<<32-1},{Lo:1<<32},{Lo:^uint64(0)},{Hi:1},{Lo:^uint64(0),Hi:1},{Hi:1<<63},{Lo:^uint64(0),Hi:^uint64(0)}}
 for _,a:=range edges{for _,b:=range edges{for _,d:=range edges{check(a,b,d)}}}
 for i:=0;i<20000;i++{
  a,b,d:=U128{rng.Uint64(),rng.Uint64()},U128{rng.Uint64(),rng.Uint64()},U128{rng.Uint64(),rng.Uint64()}
  switch i%5{case 0:a.Hi=0;b.Hi=0;d.Hi=0;case 1:b=U128{Lo:uint64(i%10000)};case 2:a.Hi=0;b.Hi=0;case 3:a=U128{Lo:uint64(i%10000)};d.Hi=0}
  check(a,b,d)
 }
}
'''

FLAG_HELPER = '''
// ReadTickFlag retains ReadTick's length, offset and flag checks without
// decoding liquidity/fee/reward fields during a search.
func ReadTickFlag(b []byte, offset uint64) (byte, uint64) {
 if uint64(len(b)) != TickArraySize || offset >= 88 { return 0, 6009 }
 flag := b[12+offset*TickSize]
 if flag > 1 { return flag, ErrLayout }
 return flag, 0
}
'''

FLAG_TEST = r'''package wire
import("math/rand";"testing")
func TestTickFlagMatchesFullDecode(t *testing.T){
 rng:=rand.New(rand.NewSource(20261008))
 for _,size:=range []int{0,1,12,113,9987,9988,9989,10000}{
  b:=make([]byte,size);rng.Read(b)
  for _,offset:=range []uint64{0,1,43,87,88,89,1<<63,^uint64(0)}{
   for _,flag:=range []byte{0,1,2,255}{
    if offset<88&&int(12+offset*TickSize)<size{b[12+offset*TickSize]=flag}
    old,code:=ReadTick(b,offset);v,err:=ReadTickFlag(b,offset)
    if v!=old.Initialized||err!=code{t.Fatal(size,offset,flag,v,err,old.Initialized,code)}
   }
  }
 }
 for trial:=0;trial<10000;trial++{
  b:=make([]byte,9988);rng.Read(b);offset:=uint64(trial%88)
  b[12+offset*TickSize]=byte(trial%4)
  old,code:=ReadTick(b,offset);v,err:=ReadTickFlag(b,offset)
  if v!=old.Initialized||err!=code{t.Fatal(trial,v,err,old.Initialized,code)}
 }
}
'''

CHECKED_APPLY_TEST = r'''package swap
import("testing";"example.org/gosvm-benchmarks/whirlpool/wire")
func TestApplyRechecksPreviouslyValidatedArrays(t *testing.T){
 for _,kind:=range []string{"flag","pool","discriminator","short"}{
  for index:=0;index<3;index++{
   pool:=make([]byte,653);copy(pool,[]byte{63,149,209,12,225,128,99,9})
   key:=wire.Key{7,8,9};arrays:=[][]byte{make([]byte,9988),make([]byte,9988),make([]byte,9988)}
   for _,b:=range arrays{copy(b,[]byte{69,97,189,190,110,7,66,187});copy(b[9956:],key[:]);if code:=wire.CheckTickArray(b,key);code!=0{t.Fatal(code)}}
   want:=wire.ErrLayout
   switch kind{case "flag":arrays[index][12+87*113]=2;case "pool":arrays[index][9956]++;want=6056;case "discriminator":arrays[index][0]++;case "short":arrays[index]=arrays[index][:9987]}
   out,code:=Apply(pool,arrays[0],arrays[1],arrays[2],key,Args{},0)
   if code!=want||out!=(Result{}){t.Fatal(kind,index,out,code,want)}
  }
 }
}
'''


def sha(p):
    return hashlib.sha256(p.read_bytes()).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', required=True, type=Path)
    parser.add_argument('--family', choices=('arithmetic','loop','validation'), default='arithmetic')
    parser.add_argument('--only', help='Run one named variant in a new output directory')
    args = parser.parse_args()
    out = args.output.resolve()
    stage = ROOT / 'build/whirlpool-math' / out.name
    assert out.is_relative_to(ROOT / 'results') and not out.exists() and not stage.exists()
    out.mkdir(parents=True)
    stage.mkdir(parents=True)
    cli = FRONTEND / 'gosvm'
    proof = json.loads((BASE / 'summary.json').read_text())
    assert proof['passed'] and sha(cli) == proof['cli_sha256']
    summary = dict(passed=False, checks=[], variants={}, scope='Copied classic-token fixed-fee handler, events omitted, O2; application arithmetic changes only')
    inputs = [Path(__file__), cli, FIXTURE, BASE / 'canonical-0.json', BENCH / 'scripts/run_budget_suite.py', BENCH / 'results/whirlpool/2026-10-06-compact-signer-v1-verified/rust-0.json', ROOT / 'internal/testvm/profiles/validator-3.0.15.json']
    for folder in (SOURCE / 'handler', SOURCE / 'go', FRONTEND / 'scaffold/.gosvm/sdk'):
        inputs += [p for p in folder.rglob('*') if p.is_file() and 'build' not in p.relative_to(folder).parts]
    summary['input_sha256'] = {str(p): sha(p) for p in inputs}
    env = dict(os.environ, GOCACHE=str(ROOT / 'build/lifecycle-go-cache'), GOWORK='off', GOTOOLCHAIN='local', GOPROXY='off', GOSUMDB='off', PYTHONDONTWRITEBYTECODE='1')

    def save():
        (out / 'summary.json').write_text(json.dumps(summary, indent=2) + '\n')

    def run(name, command, cwd=ROOT):
        start = time.perf_counter()
        with (out / (name + '.log')).open('wb') as log:
            p = subprocess.run([str(v) for v in command], cwd=cwd, env=env, stdout=log, stderr=subprocess.STDOUT)
        summary['checks'].append(dict(name=name, command=[str(v) for v in command], cwd=str(cwd), exit_code=p.returncode, seconds=time.perf_counter()-start))
        save()
        assert p.returncode == 0, name + ': see log'
        print(name + ': PASS', flush=True)

    try:
        llvm = Path.home() / '.cache/solana/v1.51/platform-tools/llvm/bin'
        baseline = json.loads((BASE / 'canonical-0.json').read_text())
        rust = json.loads((BENCH / 'results/whirlpool/2026-10-06-compact-signer-v1-verified/rust-0.json').read_text())
        prefix = 'Program ' + json.loads(FIXTURE.read_text())['program_id'] + ' consumed '
        verified = BENCH / 'build/verification/2026-10-06-fee-growth-handler'
        runner = next((verified / 'cache').rglob('gosvm-svm-runner'))
        def logs(xs):
            return [re.sub(r' of \d+ compute units$', ' of <remaining> compute units', x) for x in xs if not x.startswith(prefix)]
        variants = [('baseline',False,False,False,False),('bounded-muldiv',True,False,False,False),('normalized-delta-a',False,True,False,False),('combined',True,True,False,False)]
        if args.family == 'loop':
            variants = [('bounded-baseline',True,False,False,False),('small-multiply',True,False,True,False),('flag-scan',True,False,False,True),('small-multiply-flag-scan',True,False,True,True)]
        if args.family == 'validation':
            variants = [('unmoved-baseline',True,False,True,True),('moved-control',True,False,True,True),('reuse-validation',True,False,True,True),('reuse-validation-full-scan',True,False,True,False)]
        if args.only:
            variants=[v for v in variants if v[0]==args.only]
            assert variants, 'unknown variant'
        for name, bounded, normalized, small_multiply, flag_scan in variants:
            work = stage / name
            for module in ('handler','go'):
                shutil.copytree(SOURCE / module, work / module, ignore=shutil.ignore_patterns('build','.gosvm'))
                shutil.copytree(FRONTEND / 'scaffold/.gosvm/sdk', work / module / '.gosvm/sdk')
            wide, step = work / 'go/math/wide.go', work / 'go/math/swap_step.go'
            if bounded:
                old = 'return DivRound(p, d.Wide(), up)'
                assert wide.read_text().count(old)==1
                wide.write_text(wide.read_text().replace(old,'return DivRound128(U128{Lo: p.L[0], Hi: p.L[1]}, d, up)'))
            if normalized:
                old = 'DivRound(n, Mul128(hi, lo), up)'
                assert step.read_text().count(old)==1
                step.write_text(step.read_text().replace(old,'DivRoundWords(n, Mul128(hi, lo), up)'))
            if small_multiply:
                old = '\tp := Mul128(a, b)\n\tif p.L[2] != 0 || p.L[3] != 0 {'
                assert wide.read_text().count(old)==1
                wide.write_text(wide.read_text().replace(old,'\tif a.Hi == 0 && b.Hi == 0 {\n\t\treturn DivRound128(Mul64(a.Lo, b.Lo), d, up)\n\t}\n'+old))
            if flag_scan:
                wire = work / 'go/wire/wire.go'
                wire.write_text(wire.read_text()+FLAG_HELPER)
                swap = work / 'go/swap/swap.go'
                old = '\t\t\tt, code := wire.ReadTick(data, uint64(offset))'
                assert swap.read_text().count(old)==1
                swap.write_text(swap.read_text().replace(old,'\t\t\tinitialized, code := wire.ReadTickFlag(data, uint64(offset))').replace('if t.Initialized == 1 {','if initialized == 1 {',1))
                (work / 'go/wire/flag_test.go').write_text(FLAG_TEST)
            if args.family=='validation' and name!='unmoved-baseline':
                handler=work/'handler/program.go'
                moved=handler.read_text().replace('package handler','package swap').replace('\t"example.org/gosvm-benchmarks/whirlpool/swap"\n','').replace('swap.','').replace('func Process(c solana.Context)','func ProcessHandler(c solana.Context)')
                if name.startswith('reuse-validation'):
                    # Put the handler and swap core in the same package. Only
                    # these two audited, checked entry points call the private
                    # core; no public unchecked API or transferable certificate.
                    swap=work/'go/swap/swap.go'
                    original=swap.read_text()
                    signature='func Apply(pool, a0, a1, a2 []byte, poolKey wire.Key, args Args, timestamp uint64) (Result, uint64) {'
                    start=original.index(signature)
                    end=original.index('\tspacing := wire.Read16(pool, 41)',start)
                    checked=original[start:end]
                    loop_start=checked.index('\tfor j := uint64(0); j < 3; j++ {')
                    private='// applyValidated is private: its callers validate these exact buffers and\n// pool key in the same invocation, with no writes or CPIs before this call.\n'+checked[:loop_start].replace(signature,'func applyValidated(pool, a0, a1, a2 []byte, args Args, timestamp uint64) (Result, uint64) {')
                    wrapper=checked+'\treturn applyValidated(pool, a0, a1, a2, args, timestamp)\n}\n\n'
                    swap.write_text(original[:start]+wrapper+private+original[end:])
                    old='Apply(pool, solana.Data(c, order[0]), solana.Data(c, order[1]), solana.Data(c, order[2]), key, args, timestamp)'
                    assert moved.count(old)==1
                    moved=moved.replace(old,'applyValidated(pool, solana.Data(c, order[0]), solana.Data(c, order[1]), solana.Data(c, order[2]), args, timestamp)')
                (work/'go/swap/handler.go').write_text(moved)
                handler.write_text('package handler\nimport("gosvm/solana";"example.org/gosvm-benchmarks/whirlpool/swap")\nfunc Process(c solana.Context)uint64{return swap.ProcessHandler(c)}\n')
            if args.family=='validation':
                (work/'go/swap/checked_apply_test.go').write_text(CHECKED_APPLY_TEST)
            (work / 'go/math/optimized_muldiv_test.go').write_text(MULDIV_TEST)
            for module in ('go','handler'):
                run(name+'-native-'+module,['go','test','-ldflags=-linkmode=external','./...','-count=1'],work/module)
            c, obj, elf = out/(name+'.c'), out/(name+'.o'), out/(name+'.so')
            run(name+'-emit-c',[cli,'-emit-c','-o',c,work/'handler'])
            run(name+'-compile',[llvm/'clang','-target','sbf','-mcpu=v3','-O2','-fno-builtin','-fPIC','-fno-stack-protector','-std=c11','-Werror','-fstack-usage','-c',c,'-o',obj])
            run(name+'-link',[llvm/'ld.lld','-z','notext','-shared','--Bdynamic','--strip-all','--entry','entrypoint','--script',ROOT/'internal/compiler/sbf-v3.ld','--no-undefined','-o',elf,obj])
            if name=='baseline':
                assert sha(elf)==proof['elf_sha256'], 'baseline ELF mismatch'
            if name=='bounded-baseline':
                bounded_proof=json.loads((ROOT/'results/compiler/2026-10-06-whirlpool-math-controls/summary.json').read_text())
                assert bounded_proof['passed'] and sha(elf)==bounded_proof['variants']['bounded-muldiv']['elf_sha256']
            if name=='unmoved-baseline':
                loop_proof=json.loads((ROOT/'results/compiler/2026-10-06-whirlpool-loop-controls/summary.json').read_text())
                assert loop_proof['passed'] and sha(elf)==loop_proof['variants']['small-multiply-flag-scan']['elf_sha256']
            reports=[]
            for i in range(3):
                report = out/(name+'-'+str(i)+'.json')
                run(name+'-sbf-'+str(i),['python3',BENCH/'scripts/run_budget_suite.py','--runner',runner,'--maker',verified/'transaction-maker','--elf',elf,'--features',ROOT/'internal/testvm/profiles/validator-3.0.15.json','--fixtures',FIXTURE,'--report',report])
                actual=json.loads(report.read_text())
                assert actual['passed'] and len(actual['cases'])==len(baseline['cases'])==166
                for key in ('fixtures_sha256','runner_sha256','transaction_maker_sha256','features','runtime_version','compute_unit_limit'):
                    assert actual[key]==baseline[key],key
                for a,b in zip(actual['cases'],baseline['cases']):
                    assert a['name']==b['name'] and len(a['steps'])==len(b['steps'])
                    for s,t in zip(a['steps'],b['steps']):
                        assert s['error']==t['error'] and s['committed']==t['committed'] and logs(s['logs'])==logs(t['logs'])
                if reports:
                    assert actual['cases']==reports[0]['cases']
                reports.append(actual)
            rows=[]
            for a,b,r in zip(reports[0]['cases'],baseline['cases'],rust['cases']):
                assert a['name']==r['name'] and len(a['steps'])==len(r['steps'])
                for s,t,u in zip(a['steps'],b['steps'],r['steps']):
                    assert s['error']==u['error']
                    rows.append(dict(name=a['name'],success=s['error'] is None,baseline_cu=t['cu'],go_cu=s['cu'],rust_cu=u['cu'],saving=t['cu']-s['cu'],paired_ratio=s['cu']/u['cu']))
            success=[r for r in rows if r['success']];fail=[r for r in rows if not r['success']]
            (out/(name+'-per-case-cu.json')).write_text(json.dumps(rows,indent=2)+'\n')
            summary['variants'][name]=dict(elf_bytes=elf.stat().st_size,elf_sha256=sha(elf),max_static_frame=max(int(x.split('\t')[1]) for x in obj.with_suffix('.su').read_text().splitlines()),median_paired_ratio=statistics.median(r['paired_ratio'] for r in success),median_success_saving=statistics.median(r['saving'] for r in success),success_improve=sum(r['saving']>0 for r in success),success_regress=sum(r['saving']<0 for r in success),failure_improve=sum(r['saving']>0 for r in fail),failure_regress=sum(r['saving']<0 for r in fail),scenarios=166,repetitions=3)
            save()
            print(name+': '+json.dumps(summary['variants'][name]),flush=True)
        assert all(sha(Path(p))==h for p,h in summary['input_sha256'].items()), 'input drift'
        shutil.copyfile(__file__,out/'experiment.py.txt')
        summary['passed']=True
    finally:
        save()


if __name__=='__main__':
    main()
