#!/usr/bin/env python3
"""Exercise the complete __multi3 ABI with nonzero high limbs in actual SBF."""
import argparse,copy,hashlib,json,os,random,struct,subprocess,time
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1];BENCH=ROOT.parent/'goSVM-benchmarks'
def sha(p):return hashlib.sha256(p.read_bytes()).hexdigest()
def main():
 p=argparse.ArgumentParser(description=__doc__);p.add_argument('--cli',required=True,type=Path);p.add_argument('--output',required=True,type=Path);a=p.parse_args()
 out=a.output.resolve();assert out.is_relative_to(ROOT/'results') and not out.exists();out.mkdir(parents=True)
 cli=a.cli.resolve();base=ROOT/'build/phoenix-dependencies/2026-10-06-phoenix-dependencies';fixture=BENCH/'results/phoenix-v1/2026-10-06-v0-multiply-reproduction/fixtures.json'
 runner=BENCH/'build/phoenix-v1/runtime/bin/gosvm-svm-runner';maker=BENCH/'build/phoenix-v1/transaction-maker';features=BENCH/'tools/phoenix-v1/runtime-features.json';llvm=Path.home()/'.cache/solana/v1.51/platform-tools/llvm/bin'
 helper=ROOT/'internal/compiler/builtins.c.txt';inputs=[Path(__file__),cli,helper,fixture,runner,maker,features,base/'sdk2/multiply/program.go']
 summary=dict(passed=False,checks=[],input_sha256={str(x):sha(x) for x in inputs},artifacts={});env=dict(os.environ,PYTHONDONTWRITEBYTECODE='1')
 def save():(out/'summary.json').write_text(json.dumps(summary,indent=2)+'\n')
 def run(name,cmd):
  start=time.perf_counter()
  with (out/(name+'.log')).open('wb') as log:r=subprocess.run([str(x) for x in cmd],cwd=ROOT,env=env,stdout=log,stderr=subprocess.STDOUT)
  summary['checks'].append(dict(name=name,command=[str(x) for x in cmd],exit_code=r.returncode,seconds=time.perf_counter()-start));save();assert r.returncode==0,name;print(name+': PASS',flush=True)
 try:
  f=json.loads(fixture.read_text());template=copy.deepcopy(f['cases'][0]);f['cases']=[];f['accounts'][0]['initial']['data']=bytes(48).hex()
  rng=random.Random(20261006);edges=[0,1,2,2**32-1,2**32,2**64-1,2**64,2**65-1,2**127-1,2**127,2**128-1]
  pairs=[(x,y) for x in edges for y in edges]+[(rng.getrandbits(128),rng.getrandbits(128)) for i in range(512)]
  for i,(x,y) in enumerate(pairs):
   c=copy.deepcopy(template);c['name']='full-128-'+str(i);initial=x.to_bytes(16,'little')+y.to_bytes(16,'little')+bytes(16);c['overrides'][0]['state']['data']=initial.hex()
   c['steps'][0]['expect']['error']=None;c['steps'][0]['expect']['accounts'][0]['state']['data']=(initial[:32]+((x*y)%(2**128)).to_bytes(16,'little')).hex();f['cases'].append(c)
  fp=out/'fixtures.json';fp.write_text(json.dumps(f,separators=(',',':'))+'\n');summary.update(cases=len(pairs),repetitions=3,targets=['v0','v3']);save()
  test='''typedef unsigned __int128 probe_u128;
extern probe_u128 __multi3(probe_u128,probe_u128);
static u64 test_multiply(Context *c){
 if(c->count!=1 || c->accounts[0].data_len!=48)return 7370;
 u8 *b=c->accounts[0].data;
 probe_u128 a=((probe_u128)sol_read64(b+8)<<64)|sol_read64(b);
 probe_u128 v=((probe_u128)sol_read64(b+24)<<64)|sol_read64(b+16);
 probe_u128 r=__multi3(a,v);
 for(u64 i=0;i<16;i++)b[32+i]=(u8)(r>>(8*i));return 0;
}
'''
  for arch in ('v0','v3'):
   original=out/(arch+'-original.c');run(arch+'-emit',[cli,'-arch',arch,'-emit-c','-o',original,base/'sdk2/multiply'])
   c=out/(arch+'.c');text=original.read_text();assert 'return go_Process(&c);' in text
   text=text.replace('u64 entrypoint(u8 *input) {','static u64 test_multiply(Context *c);\nu64 entrypoint(u8 *input) {',1).replace('return go_Process(&c);','return test_multiply(&c);',1)
   c.write_text(text+test);obj,ho,elf=out/(arch+'.o'),out/(arch+'-helper.o'),out/(arch+'.so');cpu='generic' if arch=='v0' else 'v3'
   flags=['-target','sbf','-mcpu='+cpu,'-O2','-fno-builtin','-fPIC','-fno-stack-protector','-std=c11','-Werror','-fstack-usage']
   run(arch+'-compile',[llvm/'clang',*flags,'-c',c,'-o',obj]);run(arch+'-helper',[llvm/'clang',*flags,'-x','c','-c',helper,'-o',ho])
   run(arch+'-helper-disassembly',[llvm/'llvm-objdump','-dr',ho]);assert '__multi3\n' not in (out/(arch+'-helper-disassembly.log')).read_text()
   frames=[]
   for object in (obj,ho):frames += [dict(function=x.split('\t')[0],bytes=int(x.split('\t')[1]),kind=x.split('\t')[2]) for x in object.with_suffix('.su').read_text().splitlines()]
   assert all(x['bytes']<=4096 and x['kind']=='static' for x in frames)
   script=ROOT/'internal/compiler'/('sbf.ld' if arch=='v0' else 'sbf-v3.ld')
   run(arch+'-link',[llvm/'ld.lld','-z','notext','-shared','--Bdynamic','--strip-all','--entry','entrypoint','--script',script,'-o',elf,obj,ho,*(['--no-undefined'] if arch=='v3' else [])])
   run(arch+'-symbols',[llvm/'llvm-readelf','--dyn-syms','--relocations',elf]);assert 'UND __multi3' not in (out/(arch+'-symbols.log')).read_text()
   summary['artifacts'][arch]=dict(sha256=sha(elf),bytes=elf.stat().st_size,frames=frames);save();rows=[]
   for i in range(3):
    report=out/(arch+'-'+str(i)+'.json');run(arch+'-'+str(i),['python3',BENCH/'scripts/run_phoenix_suite.py','--runner',runner,'--maker',maker,'--features',features,'--elf',elf,'--fixtures',fp,'--report',report]);actual=json.loads(report.read_text());assert actual['passed']
    if rows:assert actual['cases']==rows[0]['cases']
    rows.append(actual)
  assert all(sha(Path(p))==h for p,h in summary['input_sha256'].items());summary['passed']=True
  (out/'verify.py.txt').write_text(Path(__file__).read_text())
 finally:save()
if __name__=='__main__':main()
