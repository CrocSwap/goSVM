#!/usr/bin/env python3
"""Verify Phoenix v0 multiplication and SDK-2 return data using fresh owner pins."""
import argparse,base64,copy,hashlib,json,os,random,re,shutil,statistics,struct,subprocess,tarfile,time
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1];BENCH=ROOT.parent/'goSVM-benchmarks'
TRADING=BENCH/'results/phoenix-v1/2026-10-06-trading-matched-v0'
MUL=BENCH/'results/phoenix-v1/2026-10-06-v0-multiply-reproduction'
OLD=BENCH/'build/gosvm/de6798bdb47925ea5599d6303c8e1fe09c48655078efe5a6799981895ebe5dda/gosvm'
PARENT='''package retprobe
import "gosvm/solana"
func Process(c solana.Context)uint64{
 d:=solana.Instruction(c);if len(d)==0{return 7360};mode:=d[0]
 if mode==1{return solana.SetReturnData(c,d[1:])}
 if mode==10{return solana.SetReturnData(c,solana.Data(c,0)[1:])}
 var b [20]byte;b[0]=1;b[4]=100;b[12]=1
 code:=solana.SetReturnData(c,b[:]);if code!=0{return code}
 if mode==2{for i:=uint64(0);i<20;i++{b[i]=99};return 0}
 if mode==3{return solana.SetReturnData(c,nil)}
 solana.Data(c,0)[0]=77
 if mode==7{return 7350}
 if mode==8{return solana.Invoke(c,99,nil,nil,nil)}
 var ix [1]byte
 if mode==5{ix[0]=1};if mode==9{ix[0]=2}
 code=solana.Invoke(c,1,nil,ix[:],nil);if code!=0{return code}
 if mode==6{return solana.SetReturnData(c,b[:])}
 return 0
}
'''
CHILD='''package child
import "gosvm/solana"
func Process(c solana.Context)uint64{
 d:=solana.Instruction(c);if len(d)!=1{return 7361}
 if d[0]==2{return 7351}
 if d[0]==1{var b [3]byte;b[0]=9;b[1]=8;b[2]=7;return solana.SetReturnData(c,b[:])}
 return 0
}
'''
def sha(p):return hashlib.sha256(p.read_bytes()).hexdigest()
def b58(data):
 alphabet='123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz';n=int.from_bytes(data,'big');v=''
 while n:n,r=divmod(n,58);v=alphabet[r]+v
 return '1'*(len(data)-len(data.lstrip(b'\0')))+v

def main():
 parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--output',required=True,type=Path);parser.add_argument('--runner',required=True,type=Path)
 a=parser.parse_args();out=a.output.resolve();stage=ROOT/'build/phoenix-dependencies'/out.name;runner=a.runner.resolve()
 assert out.is_relative_to(ROOT/'results') and not out.exists() and not stage.exists()
 out.mkdir(parents=True);stage.mkdir(parents=True)
 reference=json.loads((TRADING/'summary.json').read_text());assert reference['passed']
 source=BENCH/'programs/phoenix-v1/trading-v1/go'
 for n,h in reference['source_sha256'].items():
  if 'trading-v1/go/' in n:assert sha(BENCH/n)==h,n
 inputs=[Path(__file__),ROOT/'scripts/return_data_svm.py',runner,OLD,TRADING/'summary.json',TRADING/'fixtures.json',MUL/'fixtures.json',BENCH/'scripts/run_phoenix_suite.py',BENCH/'tools/phoenix-v1/runtime-features.json',Path(reference['maker_path']),Path(reference['runner_path'])]
 for folder in ('cmd','internal','sdk','solana','benchmarks/svm-runner'):
  inputs += [p for p in (ROOT/folder).rglob('*') if p.is_file()]
 inputs += [p for p in source.rglob('*') if p.is_file() and 'build' not in p.relative_to(source).parts]
 summary=dict(passed=False,checks=[],source_sha256={str(p):sha(p) for p in inputs},scope=reference['scope'],artifacts={})
 env=dict(os.environ,GOCACHE=str(ROOT/'build/lifecycle-go-cache'),GOWORK='off',GOTOOLCHAIN='local',GOPROXY='off',GOSUMDB='off',PYTHONDONTWRITEBYTECODE='1')
 cli=stage/'gosvm';llvm=Path.home()/'.cache/solana/v1.51/platform-tools/llvm/bin';features=BENCH/'tools/phoenix-v1/runtime-features.json';maker=Path(reference['maker_path'])
 def save():(out/'summary.json').write_text(json.dumps(summary,indent=2)+'\n')
 def run(name,cmd,cwd=ROOT):
  started=time.perf_counter()
  with (out/(name+'.log')).open('wb') as log:r=subprocess.run([str(x) for x in cmd],cwd=cwd,env=env,stdout=log,stderr=subprocess.STDOUT)
  summary['checks'].append(dict(name=name,command=[str(x) for x in cmd],cwd=str(cwd),exit_code=r.returncode,seconds=time.perf_counter()-started));save()
  assert r.returncode==0,name+': see log';print(name+': PASS',flush=True)
 def build(name,package,arch,frontend=cli):
  elf=out/(name+'.so');run(name+'-build',[frontend,'-arch',arch,'-no-cache','-o',elf,package])
  c,obj=out/(name+'.c'),out/(name+'.o');run(name+'-emit',[frontend,'-arch',arch,'-emit-c','-o',c,package])
  run(name+'-frames',[llvm/'clang','-target','sbf','-mcpu='+('generic' if arch=='v0' else 'v3'),'-O2','-fno-builtin','-fPIC','-fno-stack-protector','-std=c11','-Werror','-fstack-usage','-c',c,'-o',obj])
  frames=[dict(function=x.split('\t')[0],bytes=int(x.split('\t')[1]),kind=x.split('\t')[2]) for x in obj.with_suffix('.su').read_text().splitlines()]
  assert frames and all(x['bytes']<=4096 and x['kind']=='static' for x in frames)
  run(name+'-symbols',[llvm/'llvm-readelf','--dyn-syms','--relocations',elf])
  symbols=(out/(name+'-symbols.log')).read_text();assert not re.search(r'UND\s+__\w+',symbols),'unresolved compiler helper'
  summary['artifacts'][name]=dict(bytes=elf.stat().st_size,sha256=sha(elf),arch=arch,max_static_frame=max(x['bytes'] for x in frames),frames=frames);save();return elf
 def suite(name,elf,fixture,rt=runner,returns=False):
  repeats=[];adapter=ROOT/'scripts/return_data_svm.py' if returns else BENCH/'scripts/run_phoenix_suite.py'
  for i in range(3):
   report=out/(name+'-'+str(i)+'.json');run(name+'-'+str(i),['python3',adapter,'--runner',rt,'--maker',maker,'--features',features,'--elf',elf,'--fixtures',fixture,'--report',report])
   actual=json.loads(report.read_text());assert actual['passed']
   if repeats:assert actual['cases']==repeats[0]['cases']
   repeats.append(actual)
  return repeats[0]
 try:
  run('focused-native',['go','test','-ldflags=-linkmode=external','./internal/compiler','./solana','-run','TestMultiplyBuiltin|TestSBFUnresolvedSymbol|TestReturnData|TestSDK2ReturnData','-count=1','-v'])
  run('cli-build',['bash',ROOT/'scripts/build-cli.sh',cli]);run('scaffold',[cli,'new','-schema','2','-sdk','2',stage/'sdk2'])
  for folder,body in [('multiply',(BENCH/'programs/phoenix-v1/reproductions/v0-multiply/go/program.go').read_text()),('return-minimal',(BENCH/'programs/phoenix-v1/reproductions/return-data/go/program.go').read_text()),('return-probe',PARENT),('return-child',CHILD)]:
   p=stage/'sdk2'/folder;p.mkdir();(p/'program.go').write_text(body)
  fixture=json.loads((MUL/'fixtures.json').read_text());cases=fixture['cases'];template=copy.deepcopy(cases[0]);rng=random.Random(20261006);edges=[0,1,2,2**32-1,2**32,2**63-1,2**63,2**64-1]
  pairs=[(x,y) for x in edges for y in edges]
  for i in range(512):
   x,y=rng.getrandbits(64),rng.getrandbits(64)
   if i%4==0:x&=2**32-1;y&=2**32-1
   if i%4==1:y=i%3
   if i%4==2:y=max(1,(2**64-1)//max(1,x))
   pairs.append((x,y))
  for i,(x,y) in enumerate(pairs):
   c=copy.deepcopy(template);c['name']='expanded-'+str(i);initial=struct.pack('<QQQ',x,y,12345);c['overrides'][0]['state']['data']=initial.hex()
   overflow=x*y>=2**64;step=c['steps'][0];step['expect']['error']={'InstructionError':[0,'ProgramFailedToComplete']} if overflow else None
   step['expect']['accounts'][0]['state']['data']=(initial if overflow else struct.pack('<QQQ',x,y,x*y)).hex();cases.append(c)
  mulfixtures=out/'multiply-fixtures.json';mulfixtures.write_text(json.dumps(fixture,separators=(',',':'))+'\n')
  for arch in ('v0','v3'):
   elf=build('multiply-'+arch,stage/'sdk2/multiply',arch);suite('multiply-'+arch,elf,mulfixtures,rt=Path(reference['runner_path']))
  summary['multiply_cases']=len(cases);save()
  # Freeze the accepted trading application; only replace its matching local SDK.
  trading=stage/'trading';shutil.copytree(source,trading,ignore=shutil.ignore_patterns('build'))
  old=build('trading-control',trading,'v3',OLD);assert sha(old)==reference['elfs']['go']['sha256'],'accepted application changed'
  shutil.rmtree(trading/'.gosvm/sdk');shutil.copytree(stage/'sdk2/.gosvm/sdk',trading/'.gosvm/sdk')
  run('trading-native',['go','test','-ldflags=-linkmode=external','./...','-count=3'],trading)
  fixture=out/'trading-fixtures.json';shutil.copyfile(TRADING/'fixtures.json',fixture)
  reports={}
  for arch in ('v0','v3'):
   elf=build('trading-'+arch,trading,arch);reports[arch]=suite('trading-'+arch,elf,fixture,rt=Path(reference['runner_path']))
  for name,key in [('rust','rust'),('full_reference','full_reference')]:
   cmd=next(x['command'] for x in reference['commands'] if x['name']==name+'-2');elf=Path(cmd[cmd.index('--elf')+1]);assert sha(elf)==reference['elfs'][key]['sha256']
   reports[name]=suite('trading-'+name,elf,fixture,rt=Path(reference['runner_path']))
   assert reports[name]['cases']==json.loads((TRADING/(name+'-2.json')).read_text())['cases'],'Rust drift'
  oldreport=json.loads((TRADING/'go-1.json').read_text());prefix='Program '+json.loads(fixture.read_text())['program_id']+' consumed '
  def logs(xs):return [re.sub(r' of \d+ compute units$',' of <remaining> compute units',x) for x in xs if not x.startswith(prefix)]
  rows=[]
  for g0,g3,r,u,b in zip(reports['v0']['cases'],reports['v3']['cases'],reports['rust']['cases'],reports['full_reference']['cases'],oldreport['cases']):
   assert g0['name']==g3['name']==r['name']==u['name']==b['name']
   assert len(g0['steps'])==len(g3['steps'])==len(r['steps'])==len(u['steps'])==len(b['steps'])
   for x,y,z,w,v in zip(g0['steps'],g3['steps'],r['steps'],u['steps'],b['steps']):
    assert x['error']==y['error']==z['error']==w['error']==v['error'] and x['committed'] and y['committed']
    assert logs(x['logs'])==logs(y['logs'])==logs(v['logs'])
    rows.append(dict(name=g0['name'],success=x['error'] is None,go_v0=x['cu'],go_v3=y['cu'],rust_v0=z['cu'],baseline_v3=v['cu']))
  assert len(reports['v0']['cases'])==631
  summary['trading']=dict(cases=631,repetitions=3,successes=sum(x['success'] for x in rows),v0_paired_median=statistics.median(x['go_v0']/x['rust_v0'] for x in rows if x['success']),v3_paired_median=statistics.median(x['go_v3']/x['rust_v0'] for x in rows if x['success']),v3_cu_delta_min=min(x['go_v3']-x['baseline_v3'] for x in rows),v3_cu_delta_max=max(x['go_v3']-x['baseline_v3'] for x in rows))
  (out/'trading-per-case-cu.json').write_text(json.dumps(rows,indent=2)+'\n');save()
  program=json.loads((MUL/'fixtures.json').read_text())['program_id'];child=b58(hashlib.sha256(b'goSVM return-data child').digest());state=dict(data='07',owner=program,lamports=1000000000,executable=False,rent_epoch=0)
  accounts=[dict(name='state',address=b58(hashlib.sha256(b'goSVM return-data state').digest()),initial=state)]
  base=dict(program_id=program,payer_seed='01'*32,accounts=accounts,cases=[])
  payload=struct.pack('<IQQ',1,100,1)
  def returned(data,owner=program):return None if not data else dict(programId=owner,data=[base64.b64encode(data).decode(),'base64'])
  def step(instructions,data,error=None,statebyte=7):
   after=copy.deepcopy(state);after['data']=bytes([statebyte]).hex()
   return dict(instructions=instructions,expect=dict(error=None if error is None else {'InstructionError':[0,{'Custom':error}]},return_data=data,accounts=[dict(account='state',state=after)]))
  def ix(data,metas=True,target='program'):
   return dict(program=target,accounts=[dict(account='state',writable=True,signer=False),dict(account='child',writable=False,signer=False)] if metas else [],data=data.hex())
  for arch in ('v0','v3'):
   childelf=build('return-child-'+arch,stage/'sdk2/return-child',arch)
   minimal=build('return-minimal-'+arch,stage/'sdk2/return-minimal',arch);mf=copy.deepcopy(base);mf['cases']=[dict(name='borsh-order-id',steps=[step([ix(b'',False)],returned(payload))])]
   mp=out/('return-minimal-'+arch+'.json');mp.write_text(json.dumps(mf,separators=(',',':'))+'\n');suite('return-minimal-'+arch,minimal,mp,returns=True)
   elf=build('return-probe-'+arch,stage/'sdk2/return-probe',arch);rf=copy.deepcopy(base);rf['programs']=[dict(name='child',address=child,elf=childelf.name,sha256=sha(childelf))]
   for n in (0,1,20,1023,1024,1025):
    # Carry boundary-sized payloads in account data, fitting valid legacy packets.
    data=bytes((i*37+11)%256 for i in range(n));full=copy.deepcopy(state);full['data']=(b'\x07'+data).hex()
    st=step([ix(b'\x0a')],returned(data) if n<=1024 else None,error=2017 if n>1024 else None);st['expect']['accounts'][0]['state']=copy.deepcopy(full)
    rf['cases'].append(dict(name='payload-'+str(n),overrides=[dict(name='state',state=full)],steps=[st]))
   for mode,data,error,byte in [(2,returned(payload),None,7),(3,None,None,7),(4,None,None,77),(5,returned(b'\x09\x08\x07',child),None,77),(6,returned(payload),None,77),(7,returned(payload),7350,7),(8,returned(payload),2005,7),(9,None,7351,7)]:
    rf['cases'].append(dict(name='mode-'+str(mode),steps=[step([ix(bytes([mode]))],data,error,byte)]))
   rf['cases'].append(dict(name='later-instruction-clears',steps=[step([ix(b'\x02'),ix(b'\x00',False,'child')],None)]))
   rf['cases'].append(dict(name='later-transaction-clears',steps=[step([ix(b'\x02')],returned(payload)),step([ix(b'\x01',False)],None)]))
   rp=out/('return-probe-'+arch+'.json');rp.write_text(json.dumps(rf,separators=(',',':'))+'\n');suite('return-probe-'+arch,elf,rp,returns=True)
  summary['return_data']=dict(probe_cases=len(rf['cases']),minimal_cases=1,repetitions=3,targets=['v0','v3'],simulation_and_submission=True,runner_sha256=sha(runner),runner_source_sha256=sha(ROOT/'benchmarks/svm-runner/src/lib.rs'));save()
  assert all(sha(Path(p))==h for p,h in summary['source_sha256'].items()),'source drift'
  frontend=stage/'frontend';frontend.mkdir();shutil.copy2(cli,frontend/'gosvm');shutil.copytree(stage/'sdk2/.gosvm/sdk',frontend/'sdk');src=frontend/'source';src.mkdir()
  for folder in ('cmd','internal','sdk','solana'):shutil.copytree(ROOT/folder,src/folder)
  shutil.copy2(ROOT/'go.mod',src/'go.mod');(src/'scripts').mkdir();shutil.copy2(ROOT/'scripts/build-cli.sh',src/'scripts/build-cli.sh')
  manifest=dict(cli_sha256=sha(cli),files={str(p.relative_to(frontend)):sha(p) for p in sorted(frontend.rglob('*')) if p.is_file()});(frontend/'manifest.json').write_text(json.dumps(manifest,indent=2)+'\n')
  with tarfile.open(out/'frontend.tar.gz','w:gz') as t:t.add(frontend,arcname='frontend')
  with tarfile.open(out/'probes.tar.gz','w:gz') as t:
   for folder in ('multiply','return-minimal','return-probe','return-child'):t.add(stage/'sdk2'/folder,arcname=folder)
   t.add(stage/'sdk2/.gosvm/sdk',arcname='.gosvm/sdk');t.add(stage/'sdk2/go.mod',arcname='go.mod')
  shutil.copy2(runner,out/'return-data-runner');(out/'runner-source').mkdir();shutil.copytree(ROOT/'benchmarks/svm-runner',out/'runner-source/svm-runner')
  summary.update(frontend_sha256=manifest['cli_sha256'],frontend_archive_sha256=sha(out/'frontend.tar.gz'),probe_archive_sha256=sha(out/'probes.tar.gz'),passed=True)
  shutil.copyfile(__file__,out/'verify.py.txt');shutil.copyfile(ROOT/'scripts/return_data_svm.py',out/'return-data-svm.py.txt')
 finally:save()
if __name__=='__main__':main()
