#!/usr/bin/env python3
"""Project-clean, source-edit and no-op builds; toolchains are already installed.

All timings include process startup. No network, validator, or test harness is in
the timed path. A separate fresh GOCACHE measures frontend bootstrap cost.
"""
import json
import hashlib
import random
import os
from pathlib import Path
import platform
import resource
import shutil
import statistics
import subprocess
import sys
import tempfile
import time

ROOT = Path(__file__).resolve().parents[1]
os.chdir(ROOT)
TOOLS = Path(os.environ.get("SBF_TOOLS", Path.home() / ".cache/solana/v1.51/platform-tools"))
ENV = dict(os.environ, PATH=str(TOOLS / "rust/bin") + os.pathsep + os.environ["PATH"])
REPEATS = int(os.environ.get("BENCH_REPEATS", "7"))
ARCH = os.environ.get("SBF_ARCH", "v3")
LAST_CPU = 0.0


def run(cmd, *, cwd=ROOT, env=None):
    global LAST_CPU
    start = time.perf_counter()
    before = resource.getrusage(resource.RUSAGE_CHILDREN)
    p = subprocess.run([str(x) for x in cmd], cwd=cwd, env=env, capture_output=True, text=True)
    after = resource.getrusage(resource.RUSAGE_CHILDREN)
    LAST_CPU = after.ru_utime + after.ru_stime - before.ru_utime - before.ru_stime
    if p.returncode:
        raise RuntimeError(f"{cmd}\n{p.stdout}\n{p.stderr}")
    return time.perf_counter() - start


def size(path):
    if not path.exists():
        raise FileNotFoundError(path)
    if path.is_file():
        return path.stat().st_size
    return sum(p.stat().st_size for p in path.rglob("*") if p.is_file() and not p.is_symlink())


def stats(samples):
    return {"median_ms": round(statistics.median(samples) * 1000, 3),
            "min_ms": round(min(samples) * 1000, 3),
            "max_ms": round(max(samples) * 1000, 3),
            "samples_ms": [round(x * 1000, 3) for x in samples]}


def version(cmd):
    return subprocess.check_output([str(x) for x in cmd], text=True).strip()


def measure(commands, work, repeats):
    """Interleave backends in a seeded shuffled order to reduce time/load bias."""
    scenarios = ("project_clean", "source_edit", "no_op")
    samples = {b: {s: {"wall": [], "cpu": []} for s in scenarios} for b in commands}
    originals = {b: c[3].read_text() for b, c in commands.items()}
    for cmd, cwd, env, _ in commands.values():
        run(cmd, cwd=cwd, env=env)
    rng = random.Random(20261004)
    for scenario in scenarios:
        for i in range(repeats):
            order = list(commands)
            rng.shuffle(order)
            for backend in order:
                cmd, cwd, env, source = commands[backend]
                if scenario == "project_clean":
                    if backend == "rust_sbf":
                        shutil.rmtree(work / "rust-target", ignore_errors=True)
                        shutil.rmtree(work / "rust-out", ignore_errors=True)
                    elif backend == "go_sbf":
                        (work / "go.so").unlink(missing_ok=True)
                    else:
                        # Force the package build; retain the installed stdlib cache.
                        (work / "native-exe").unlink(missing_ok=True)
                        source.write_text(originals[backend] + f"\n// clean {time.time_ns()}-{i}\n")
                elif scenario == "source_edit":
                    source.write_text(originals[backend] + f"\n// edit {time.time_ns()}-{i}\n")
                sample = samples[backend][scenario]
                sample["wall"].append(run(cmd, cwd=cwd, env=env))
                sample["cpu"].append(LAST_CPU)
    report = {}
    for backend, scenarios in samples.items():
        report[backend] = {}
        for scenario, sample in scenarios.items():
            report[backend][scenario] = stats(sample["wall"])
            report[backend][scenario]["cpu"] = stats(sample["cpu"])
        commands[backend][3].write_text(originals[backend])
    return report


def main():
    tokens = "--tokens" in sys.argv
    suite = "tokenswap" if tokens else "amm"
    rustdir = "tokenswap-rust" if tokens else "rust"
    rustelf = "tokenswap_rust.so" if tokens else "amm_rust.so"
    sourcefile = "swap.go" if tokens else "amm.go"
    run(["bash", "scripts/build-tokens.sh" if tokens else "scripts/build.sh"])
    if platform.system() == "Darwin":
        run(["go", "build", "-ldflags=-linkmode=external", "-o", "build/verify", "./cmd/verify"])
        run(["codesign", "--force", "--sign", "-", "build/verify"])
    else:
        run(["go", "build", "-o", "build/verify", "./cmd/verify"])
    report = {"platform": platform.platform(), "repeats": REPEATS,
              "workload": suite,
              "go": version(["go", "version"]),
              "clang": version([TOOLS / "llvm/bin/clang", "--version"]).splitlines()[0],
              "rustc": version([TOOLS / "rust/bin/rustc", "--version"]),
              "scope": f"sBPF {ARCH}; installed tools and warm OS cache; no dependency downloads",
              "builds": {}}
    with tempfile.TemporaryDirectory(prefix="bench-", dir=ROOT / "build") as temp:
        work = Path(temp)
        rust = work / "rust"
        shutil.copytree(ROOT / "baselines" / rustdir, rust)
        rustenv = dict(ENV, CARGO_TARGET_DIR=str(work / "rust-target"))
        gofile = work / "amm.go"
        gofile.write_text((ROOT / "examples" / suite / sourcefile).read_text())
        native = work / "native"
        native.mkdir()
        (native / "go.mod").write_text(f"module nativebench\n\ngo 1.22\nrequire gosvm v0.0.0\nreplace gosvm => {ROOT}\n")
        (native / "amm.go").write_text(gofile.read_text().replace("package " + suite, "package main", 1))
        driver = 'package main\nimport "gosvm/solana"\nfunc main(){println(Process(solana.Context{}))}\n' if tokens else "package main\nfunc main(){s:=make([]byte,24);i:=make([]byte,16);s[2]=16;s[10]=32;i[1]=1;println(Process(s,i))}\n"
        (native / "main.go").write_text(driver)
        commands = {
            "go_sbf": ([ROOT / "build/gosvm", "-arch", ARCH, "-llvm", TOOLS / "llvm", "-o", work / "go.so", gofile], ROOT, None, gofile),
            "rust_sbf": (["cargo-build-sbf", "--manifest-path", rust / "Cargo.toml", "--sbf-out-dir", work / "rust-out",
                          "--tools-version", "v1.51", "--arch", ARCH, "--offline", "--no-rustup-override"], ROOT, rustenv, rust / "src/lib.rs"),
            "go_native": (["go", "build", "-o", work / "native-exe", "."], native, None, native / "amm.go"),
        }
        report["builds"] = measure(commands, work, REPEATS)
        for backend, measurements in report["builds"].items():
            print(backend, {k: v["median_ms"] for k, v in measurements.items()}, flush=True)
        report["sampling_order"] = "serial, backends interleaved with random seed 20261004 within each scenario"
        report["elf_sha256"] = {b: hashlib.sha256(p.read_bytes()).hexdigest() for b, p in
                                (("go", work / "go.so"), ("rust", work / "rust-out" / rustelf))}
        report["project_artifact_bytes"] = {
            "go_sbf_elf": size(work / "go.so"),
            "go_sbf_build_receipt": size(work / "go.so.gosvm-cache"),
            "rust_sbf_elf": size(work / "rust-out" / rustelf),
            "rust_target_and_deploy": size(work / "rust-target") + size(work / "rust-out"),
            "native_go_executable": size(work / "native-exe"),
        }
        for elf in (work / "go.so", work / "rust-out" / rustelf):
            assert int.from_bytes(elf.read_bytes()[48:52], "little") == int(ARCH[1:]), "comparison must use matching ELF targets"
        # Prove the SBF path needs only these two backend executables, with no
        # adjacent LLVM headers/libraries or Rust installation in its tool root.
        minimal = work / "minimal-llvm/bin"
        minimal.mkdir(parents=True)
        for name in ("clang", "ld.lld"):
            os.link((TOOLS / "llvm/bin" / name).resolve(), minimal / name)
        run([ROOT / "build/gosvm", "-arch", ARCH, "-llvm", minimal.parent, "-o", work / "minimal.so", gofile])
        assert (work / "minimal.so").read_bytes() == (work / "go.so").read_bytes()
        report["isolated_clang_and_lld_build_verified"] = True
        # No modules downloaded; isolates stdlib compilation from ordinary contract builds.
        cacheenv = dict(os.environ, GOCACHE=str(work / "fresh-go-cache"))
        report["frontend_bootstrap_fresh_go_cache_ms"] = round(run(
            ["bash", "scripts/build-cli.sh", work / "fresh-gosvm"], env=cacheenv) * 1000, 3)
        report["frontend_bootstrap_cache_bytes"] = size(work / "fresh-go-cache")
    report["toolchain_bytes"] = {
        "gosvm_frontend": size(ROOT / "build/gosvm"),
        "clang_and_lld": size(TOOLS / "llvm/bin/clang") + size(TOOLS / "llvm/bin/ld.lld"),
        "installed_solana_llvm": size(TOOLS / "llvm"),
        "installed_solana_rust": size(TOOLS / "rust"),
        "installed_go": size(Path(version(["go", "env", "GOROOT"]))),
    }
    validator = Path(shutil.which("solana-test-validator")).resolve()
    report["test_harness_bytes"] = {
        "go_harness_binary": size(ROOT / "build/verify"),
        "validator_binary": size(validator),
        "installed_solana_cli_directory": size(validator.parent.parent),
        "note": "Logical bytes; validator binary is included in CLI directory, not additive. Temporary ledgers are removed after verification. Shared Go build cache excluded from project artifacts; fresh frontend cache measured separately.",
    }
    destination = ROOT / "build" / ("tokenswap-benchmark.json" if tokens else "benchmark.json")
    destination.write_text(json.dumps(report, indent=2) + "\n")
    print(f"Wrote {destination.relative_to(ROOT)}", flush=True)


if __name__ == "__main__":
    main()
