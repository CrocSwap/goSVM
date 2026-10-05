#!/usr/bin/env python3
"""Controlled code-growth benchmark, not a representative application.

Each distinct function performs eight dependent multiply/xor rounds. Every
function is called and its result is written to account data. Neither frontend
gets inlining directives. VM verification independently checks Python results.
"""
import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import time

from bench import ROOT, TOOLS, ENV, run, size, stats, version
import bench

REPEATS = int(os.environ.get("SCALE_REPEATS", "5"))
MASK = (1 << 64) - 1


def constant(i, j):
    return ((0x9e3779b97f4a7c15 * (i * 8 + j + 1)) & MASK) | 1


def reference(seed, count):
    x = seed
    for i in range(count):
        for j in range(8):
            x = ((x ^ (x >> (17 + j))) * constant(i, j)) & MASK
    return x


def generate(count):
    go = ["package scale\n", "func Process(s,i []byte) uint64 {\nx:=load64(s,0)^load64(i,0)"]
    rust = [(ROOT / "baselines/rust/src/lib.rs").read_text().split("fn process(")[0],
            "fn process(s: &mut [u8], i: &[u8])->u64 { let mut x=unsafe{load(s.as_ptr())^load(i.as_ptr())};"]
    for i in range(count):
        go.append(f"x=mix{i}(x)")
        rust.append(f"x=mix{i}(x);")
    go.append("store64(s,0,x)\nreturn 0\n}\n")
    rust.append("unsafe{store(s.as_mut_ptr(),x)};0}\n")
    for i in range(count):
        go.append(f"func mix{i}(x uint64) uint64 {{")
        rust.append(f"fn mix{i}(mut x:u64)->u64 {{")
        for j in range(8):
            c = constant(i, j)
            go.append(f"x=(x^(x>>{17+j}))*{c}")
            rust.append(f"x=(x^(x>>{17+j})).wrapping_mul({c});")
        go.append("return x\n}")
        rust.append("x}")
    go.append("func load64(s []byte,o uint64)uint64 {v:=uint64(0);for j:=uint64(0);j<8;j++ {v=v|uint64(s[o+j])<<(j*8)};return v}\nfunc store64(s []byte,o,n uint64){for j:=uint64(0);j<8;j++ {s[o+j]=byte(n>>(j*8))}}")
    return "\n".join(go) + "\n", "\n".join(rust) + "\n"


def main():
    run(["bash", "scripts/build-cli.sh", "build/gosvm"])
    work = ROOT / "build/scaling"
    work.mkdir(parents=True, exist_ok=True)
    report = {"platform": platform.platform(), "target": "sBPF v3", "repeats": REPEATS,
              "go": version(["go", "version"]), "rustc": version([TOOLS / "rust/bin/rustc", "--version"]),
              "clang": version([TOOLS / "llvm/bin/clang", "--version"]).splitlines()[0],
              "scope": "Synthetic 8-round dependent integer mixers, all observable; installed toolchains, warm OS/stdlib caches, serial builds; no forced inline/noinline; not representative package/dependency growth",
              "rows": []}
    for count in (1, 16, 64, 256):
        case = work / str(count)
        case.mkdir(exist_ok=True)
        gosrc, rustsrc = generate(count)
        source = case / "scale.go"
        source.write_text(gosrc)
        rust = case / "rust"
        (rust / "src").mkdir(parents=True, exist_ok=True)
        for name in ("Cargo.toml", "Cargo.lock"):
            shutil.copy(ROOT / "baselines/rust" / name, rust / name)
        (rust / "src/lib.rs").write_text(rustsrc)
        native = case / "native"
        native.mkdir(exist_ok=True)
        (native / "go.mod").write_text("module scaling\n\ngo 1.22\n")
        (native / "scale.go").write_text(gosrc.replace("package scale", "package main", 1))
        (native / "main.go").write_text("package main\nfunc main(){s:=make([]byte,24);i:=make([]byte,16);println(Process(s,i));println(s[0])}\n")
        rustenv = dict(ENV, CARGO_TARGET_DIR=str(case / "rust-target"))
        commands = {
            "go_sbf": ([ROOT / "build/gosvm", "-arch", "v3", "-llvm", TOOLS / "llvm", "-o", case / "go.so", source], ROOT, None, source),
            "rust_sbf": (["cargo-build-sbf", "--manifest-path", rust / "Cargo.toml", "--sbf-out-dir", case / "rust-out", "--tools-version", "v1.51", "--arch", "v3", "--offline", "--no-rustup-override"], ROOT, rustenv, rust / "src/lib.rs"),
            "go_native": (["go", "build", "-o", case / "native-exe", "."], native, None, native / "scale.go"),
        }
        row = {"functions": count, "rounds": count * 8, "go_source_lines": len(gosrc.splitlines()),
               "go_source_bytes": len(gosrc), "rust_source_bytes": len(rustsrc), "builds": {}}
        row["builds"] = bench.measure(commands, case, REPEATS)
        row["sampling_order"] = "serial, backends interleaved with random seed 20261004 within each scenario"
        row["elf_bytes"] = {}
        row["elf_sha256"] = {}
        for backend, elf in (("go", case / "go.so"), ("rust", case / "rust-out/amm_rust.so")):
            data = elf.read_bytes()
            assert data[:4] == b"\x7fELF" and int.from_bytes(data[48:52], "little") == 3
            row["elf_bytes"][backend] = len(data)
            row["elf_sha256"][backend] = hashlib.sha256(data).hexdigest()
        row["rust_target_and_deploy_bytes"] = size(case / "rust-target") + size(case / "rust-out")
        row["vectors"] = [{"seed": x, "expected": reference(x, count)} for x in (0, 1, MASK, 0x123456789abcdef0, 0x8000000000000000)]
        report["rows"].append(row)
        print(count, "functions", {k: v["source_edit"]["median_ms"] for k, v in row["builds"].items()}, row["elf_bytes"], flush=True)
    (ROOT / "build/scaling-benchmark.json").write_text(json.dumps(report, indent=2) + "\n")
    print("Wrote build/scaling-benchmark.json; run make verify-scale", flush=True)


if __name__ == "__main__":
    main()
