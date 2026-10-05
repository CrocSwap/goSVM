#!/usr/bin/env python3
"""Cold dependency builds vs cached source edits, with separate VM harness costs.

Installed compilers and downloaded crate sources are prerequisites, not timed.
Every cold phase gets its own empty output directory. No Basanos targets are used.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import resource
import shutil
import subprocess
import time

ROOT=Path(__file__).resolve().parents[1]
WORK=ROOT/"build/protocol"
TOOLS=Path(os.environ.get("SBF_TOOLS",Path.home()/".cache/solana/v1.51/platform-tools"))
REPORT=WORK/"benchmark.json"
ENV=dict(os.environ,CARGO_BUILD_JOBS="4",GOMAXPROCS="4")
# Never inherit a Rust wrapper or ambient compiler flags into the comparison.
for k in ("RUSTC_WRAPPER","RUSTFLAGS","CARGO_ENCODED_RUSTFLAGS","CARGO_TARGET_DIR","CARGO_INCREMENTAL"):
    ENV.pop(k,None)


def disk(p):
    totals={"logical_bytes":0,"allocated_bytes":0,"files":0}
    for f in p.rglob("*"):
        if f.is_file() and not f.is_symlink():
            s=f.stat();totals["logical_bytes"]+=s.st_size;totals["allocated_bytes"]+=s.st_blocks*512;totals["files"]+=1
    return totals


def run(report,label,cmd,env=ENV,cwd=ROOT):
    log=WORK/(label+".log")
    load_before=os.getloadavg()
    before=resource.getrusage(resource.RUSAGE_CHILDREN);start=time.perf_counter()
    print("Running",label,flush=True)
    with log.open("w") as f:
        p=subprocess.run([str(x) for x in cmd],cwd=cwd,env=env,stdout=f,stderr=subprocess.STDOUT)
    wall=time.perf_counter()-start;after=resource.getrusage(resource.RUSAGE_CHILDREN)
    row={"wall_seconds":wall,"cpu_seconds":after.ru_utime+after.ru_stime-before.ru_utime-before.ru_stime,"load_average_before":load_before,"load_average_after":os.getloadavg(),"exit_code":p.returncode,"command":[str(x) for x in cmd],"log":str(log.relative_to(ROOT))}
    report["measurements"][label]=row;save(report)
    print(label,round(wall,3),"s",flush=True)
    if p.returncode:raise RuntimeError(f"{label} failed; see {log}\n"+log.read_text()[-5000:])


def save(r):
    REPORT.write_text(json.dumps(r,indent=2)+"\n")


def cargo_timing(target):
    candidates=sorted((target/"cargo-timings").glob("cargo-timing-*.html"),key=lambda p:p.stat().st_mtime)
    if not candidates:return {}
    p=candidates[-1];text=p.read_text();marker="const UNIT_DATA = "
    if marker not in text:return {"file":str(p.relative_to(ROOT))}
    units,_=json.JSONDecoder().raw_decode(text.split(marker,1)[1])
    return {"file":str(p.relative_to(ROOT)),"units":len(units),"top_units":sorted(units,key=lambda x:x.get("duration",0),reverse=True)[:20]}


def fresh(p):
    assert p.is_relative_to(WORK)
    if p.exists():shutil.rmtree(p)
    p.mkdir(parents=True)


def edits(report,prefix,cmd,source,env):
    original=source.read_text()
    try:
        for i in range(3):
            source.write_text(original+f"\n// benchmark edit {time.time_ns()}\n")
            run(report,f"{prefix}-edit-{i}",cmd,env)
            run(report,f"{prefix}-noop-{i}",cmd,env)
    finally:source.write_text(original)


def record_elf(r,name):
    p=WORK/(name+".so");b=p.read_bytes()
    assert b[:4]==b"\x7fELF" and int.from_bytes(b[48:52],"little")==3
    r.setdefault("elf",{})[name]={"bytes":len(b),"sha256":hashlib.sha256(b).hexdigest()}


def go_contract(r):
    run(r,"frontend-installed-build",["bash","scripts/build-cli.sh","build/gosvm"])
    src=ROOT/"examples/protocol/protocol.go"
    elf=WORK/"protocol_go.so"
    elf.unlink(missing_ok=True)
    cmd=[ROOT/"build/gosvm","-arch","v3","-llvm",TOOLS/"llvm","-o",elf,src]
    run(r,"go-contract-cold",cmd)
    edits(r,"go-contract",cmd,src,ENV)
    record_elf(r,"protocol_go");save(r)


def contracts(r):
    go_contract(r)
    target=WORK/"sbf-target";fresh(target)
    env=dict(ENV,PATH=str(TOOLS/"rust/bin")+os.pathsep+ENV["PATH"],CARGO_TARGET_DIR=str(target))
    out=WORK/"sbf-out";fresh(out)
    cmd=["cargo-build-sbf","--manifest-path",ROOT/"benchmarks/protocol/rust/Cargo.toml","--tools-version","v1.51","--arch","v3","--sbf-out-dir",out,"--offline","--no-rustup-override","--","--locked","--timings"]
    run(r,"rust-contract-cold",cmd,env)
    r["rust_contract_cold_timing"]=cargo_timing(target)
    r["disk"]["rust_sbf_target_after_cold"]=disk(target);save(r)
    edits(r,"rust-contract",cmd,ROOT/"benchmarks/protocol/rust/src/lib.rs",env)
    shutil.copy(out/"protocol_bench.so",WORK/"protocol_bench.so")
    r["disk"]["rust_sbf_target"]=disk(target);r["disk"]["rust_sbf_deploy"]=disk(out)
    for name in ("protocol_go","protocol_bench"):
        record_elf(r,name)
    save(r)


def host(r,resume=False):
    target=WORK/"host-target"
    if not resume:fresh(target)
    env=dict(ENV,CARGO_TARGET_DIR=str(target),PROTOCOL_FIXTURES=str(WORK/"fixtures.json"),RUST_LOG="error")
    # ProgramTest::new reads these before registering its initial program.
    # This stage intentionally tests the native Rust processor.
    env.pop("BPF_OUT_DIR",None);env.pop("SBF_OUT_DIR",None)
    base=["cargo","test","--manifest-path",ROOT/"benchmarks/protocol/harness/Cargo.toml","--offline","--locked","--tests"]
    if resume:
        assert r["measurements"]["rust-programtest-cold"]["exit_code"]==0
        run(r,"rust-programtest-harness-repair",base+["--no-run"],env)
    else:
        run(r,"rust-programtest-cold",base+["--no-run","--timings"],env)
        r["rust_programtest_cold_timing"]=cargo_timing(target)
        r["disk"]["rust_host_target_after_cold"]=disk(target);save(r)
    run(r,"rust-programtest-execute",base+["--","--test-threads=1"],env)
    edits(r,"rust-programtest",base+["--no-run"],ROOT/"benchmarks/protocol/rust/src/lib.rs",env)
    r["disk"]["rust_host_target_after_edits"]=disk(target)
    r["disk"]["rust_host_subtrees"]={p.name:disk(p) for p in (target/"debug").iterdir() if p.is_dir()}
    r["programtest_verification"]={"mode":"native Rust processor, not SBF", "partitions":12,
        "reference_cases":116,"committed_lifecycle_instructions":20,"atomic_rollback":True,
        "fixtures_sha256":hashlib.sha256((WORK/"fixtures.json").read_bytes()).hexdigest()}
    save(r)


def go_harness(r):
    cache=WORK/"go-harness-cache";fresh(cache)
    env=dict(ENV,GOCACHE=str(cache))
    cmd=["go","build","-p","4"]
    if platform.system()=="Darwin":cmd.append("-ldflags=-linkmode=external")
    cmd.extend(["-o",WORK/"verify","./cmd/verify"])
    run(r,"go-harness-fresh-cache",cmd,env)
    run(r,"go-harness-noop",cmd,env)
    native=["go","test","-c","-p","4","-o",WORK/"native-protocol.test","./examples/protocol"]
    run(r,"go-native-tests-build",native,env)
    run(r,"go-native-tests-execute",[WORK/"native-protocol.test"],env,cwd=ROOT/"examples/protocol")
    edits(r,"go-native-tests",native,ROOT/"examples/protocol/protocol.go",env)
    r["disk"]["go_harness_cache"]=disk(cache);r["disk"]["go_harness_binary_bytes"]=(WORK/"verify").stat().st_size
    r["disk"]["go_native_tests_binary_bytes"]=(WORK/"native-protocol.test").stat().st_size
    if platform.system()=="Darwin":run(r,"go-harness-codesign",["codesign","--force","--sign","-",WORK/"verify"])
    run(r,"external-validator-verify",[WORK/"verify","protocol"])
    save(r)


def main():
    os.chdir(ROOT);WORK.mkdir(parents=True,exist_ok=True)
    parser=argparse.ArgumentParser();parser.add_argument("--stage",choices=["contracts","go-contract","host","host-resume","go-harness","all"],default="all");args=parser.parse_args()
    r=json.loads(REPORT.read_text()) if REPORT.exists() else {"platform":platform.platform(),"scope":"Installed compilers/crate downloads warm; cold project outputs; 4 build jobs; debug ProgramTest; 12 test binaries; one cold sample, three cached edits/noops; no public network or Basanos builds","measurements":{},"disk":{}}
    r["versions"]={"go":subprocess.check_output(["go","version"],text=True).strip(),"host_rustc":subprocess.check_output(["rustc","--version"],text=True).strip(),"sbf_rustc":subprocess.check_output([TOOLS/"rust/bin/rustc","--version"],text=True).strip(),"validator":subprocess.check_output(["solana-test-validator","--version"],text=True).strip(),"platform_tools":"v1.51"}
    r["verification_modes"]={"programtest":"native Rust processor; explicit native mode with SBF output environment cleared", "external_validator":"actual Go and Rust SBFv3 images; sole CU comparison"}
    for stage,fn in (("contracts",contracts),("go-harness",go_harness),("host",host)):
        if args.stage in (stage,"all"):fn(r)
    if args.stage=="go-contract":go_contract(r)
    if args.stage=="host-resume":host(r,resume=True)
    print("Report:",REPORT,flush=True)


if __name__=="__main__":main()
