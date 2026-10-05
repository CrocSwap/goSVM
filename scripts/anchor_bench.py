#!/usr/bin/env python3
"""Installed-tool, offline contract builds: Go, lean Rust, Anchor 0.32.2.
One empty-target build and three comment-edit/no-op pairs per case. No crate
fetches, CLI compilation, IDL/TS generation, or validator time in contract timings.
"""
import hashlib
import json
import os
from pathlib import Path
import resource
import shutil
import statistics
import subprocess
import time

ROOT=Path(__file__).resolve().parents[1]
WORK=ROOT/'build/anchor'
OUT=ROOT/'results/anchor'
TOOLS=Path(os.environ.get('SBF_TOOLS',Path.home()/'.cache/solana/v1.51/platform-tools'))
ENV=dict(os.environ,CARGO_BUILD_JOBS='4',GOMAXPROCS='4')
for k in ('RUSTC_WRAPPER','RUSTFLAGS','CARGO_ENCODED_RUSTFLAGS','CARGO_TARGET_DIR','CARGO_INCREMENTAL','SBF_OUT_DIR','BPF_OUT_DIR'):
 ENV.pop(k,None)

def disk(p):
 return sum(f.stat().st_size for f in p.rglob('*') if f.is_file() and not f.is_symlink())

def main():
 WORK.mkdir(parents=True,exist_ok=True);OUT.mkdir(parents=True,exist_ok=True)
 report={'target':'sBPF v3','anchor':'0.32.2','platform_tools':'v1.51','measurements':{},'cases':{}}
 def save(): (OUT/'benchmark.json').write_text(json.dumps(report,indent=2)+'\n')
 def run(label,args,env=ENV):
  before=resource.getrusage(resource.RUSAGE_CHILDREN);start=time.perf_counter();load=os.getloadavg()
  print('Running',label,flush=True)
  with (OUT/(label+'.log')).open('w') as log:p=subprocess.run([str(x) for x in args],cwd=ROOT,env=env,stdout=log,stderr=subprocess.STDOUT)
  after=resource.getrusage(resource.RUSAGE_CHILDREN)
  result={'seconds':time.perf_counter()-start,'cpu_seconds':after.ru_utime+after.ru_stime-before.ru_utime-before.ru_stime,'load_before':load,'load_after':os.getloadavg(),'exit_code':p.returncode,'command':[str(x) for x in args]}
  report['measurements'][label]=result;save();print(label,round(result['seconds'],3),'s',flush=True)
  if p.returncode:raise RuntimeError((OUT/(label+'.log')).read_text()[-6000:])
  return result['seconds']
 run('frontend-setup',['bash','scripts/build-cli.sh',WORK/'gosvm'])
 for workload in ('bounded','token'):
  for backend in ('go','lean','anchor'):
   label=workload+'-'+backend
   target=WORK/('bench-'+label+'-target');out=WORK/('bench-'+label+'-out')
   for d in (target,out):
    if d.exists():shutil.rmtree(d)
    d.mkdir()
   if backend=='go':
    src=ROOT/'examples/typed-swap' if workload=='bounded' else ROOT/'benchmarks/anchor/go-token/swap.go'
    edited=src/'program.go' if src.is_dir() else src
    elf=out/'program.so';cmd=[WORK/'gosvm','-llvm',TOOLS/'llvm','-o',elf,src];env=ENV
   else:
    crate=ROOT/'benchmarks/anchor'/ (workload if backend=='anchor' else 'lean-'+workload)
    edited=crate/'src/lib.rs';name=('anchor_'+workload+'_bench') if backend=='anchor' else 'anchor_lean_'+workload
    elf=out/(name+'.so')
    cmd=['cargo-build-sbf','--manifest-path',crate/'Cargo.toml','--sbf-out-dir',out,'--tools-version','v1.51','--arch','v3','--offline','--no-rustup-override','--','--locked','--timings']
    env=dict(ENV,PATH=str(TOOLS/'rust/bin')+os.pathsep+ENV['PATH'],CARGO_TARGET_DIR=str(target))
   original=edited.read_bytes()
   cold=run(label+'-cold',cmd,env)
   cold_disk=disk(target)+disk(out);edit=[];noop=[]
   try:
    for i in range(3):
     edited.write_bytes(original+f'\n// Benchmark comment edit {i}\n'.encode())
     edit.append(run(label+f'-edit-{i}',cmd,env));noop.append(run(label+f'-noop-{i}',cmd,env))
   finally:edited.write_bytes(original)
   # Restore matching receipts/build metadata for the checked-in source.
   run(label+'-restore',cmd,env)
   b=elf.read_bytes();assert b[:4]==b'\x7fELF' and int.from_bytes(b[48:52],'little')==3
   destination=WORK/(('go-'+workload+'.so') if backend=='go' else elf.name)
   shutil.copyfile(elf,destination)
   report['cases'][label]={'cold_seconds':cold,'edit_seconds':edit,'noop_seconds':noop,'edit_median_seconds':statistics.median(edit),'noop_median_seconds':statistics.median(noop),'cold_artifact_bytes':cold_disk,'retained_artifact_bytes':disk(target)+disk(out),'elf_bytes':len(b),'elf_sha256':hashlib.sha256(b).hexdigest()}
   if backend!='go':report['cases'][label]['locked_packages']=(crate/'Cargo.lock').read_text().count('[[package]]')
   save()
 report['sources']={str(p.relative_to(ROOT)):hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted((ROOT/'benchmarks/anchor').rglob('*')) if p.is_file()}
 for name,args in {'go':['go','version'],'validator':['solana-test-validator','--version'],'sbf_rust':[TOOLS/'rust/bin/rustc','--version'],'clang':[TOOLS/'llvm/bin/clang','--version']}.items():report[name]=subprocess.check_output([str(x) for x in args],text=True).strip()
 save()

if __name__=='__main__':main()
