#!/usr/bin/env python3
"""Compare pinned local-validator fixtures with a persistent LiteSVM sidecar.

Uses existing build/anchor ELFs. Never overwrites an evidence directory.
Runner compilation is developer bootstrap, separate from timed fixture execution.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import statistics
import subprocess
import time

ROOT = Path(__file__).resolve().parents[1]
BACKENDS = ("go", "lean-rust", "anchor")


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def load(path):
    return json.loads(path.read_text())


def save(path, value):
    path.write_text(json.dumps(value, indent=2) + "\n")


def run(command, log, env=None):
    started = time.monotonic()
    with log.open("w") as out:
        subprocess.run(command, cwd=ROOT, env=env, stdout=out, stderr=subprocess.STDOUT,
                       check=True, timeout=300)
    return time.monotonic() - started


def compare(validator, runner):
    token_v = load(validator / "anchor/tokenswap-verification.json")
    token_l = load(runner / "anchor/tokenswap-verification.json")
    for key in ("elf_sha256", "token_program_elf_sha256", "results", "observed_errors",
                "vectors_per_backend", "cpi_logs_verified", "committed_swaps_per_backend",
                "failed_second_cpi_rollback_per_backend", "failed_second_instruction_rollback_per_backend"):
        if token_v[key] != token_l[key]:
            raise RuntimeError(f"token engine mismatch: {key}")
    if token_v["active_features"] != token_l["runner"]["active_features"]:
        raise RuntimeError("active feature sets differ")
    for backend in BACKENDS:
        v = load(validator / f"anchor/bounded-{backend}/build/sbf-results.json")
        l = load(runner / f"anchor/bounded-{backend}/build/sbf-results.json")
        for key in ("elf_sha256", "fixtures_sha256", "cases"):
            if v[key] != l[key]:
                raise RuntimeError(f"bounded/{backend} mismatch: {key}")
        if token_v["active_features"] != l["runner"]["active_features"]:
            raise RuntimeError(f"bounded/{backend} active feature sets differ")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", required=True, type=Path, help="new evidence directory")
    parser.add_argument("--samples", type=int, default=3)
    parser.add_argument("--cold-runner-build", action="store_true", help="use an empty target for runner bootstrap")
    args = parser.parse_args()
    if not 1 <= args.samples <= 10:
        parser.error("samples must be 1..10")
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=False)
    stage = ROOT / "build/svm-experiments" / output.name
    stage.mkdir(parents=True, exist_ok=False)
    target = stage / "runner-target" if args.cold_runner_build else ROOT / "build/svm-runner-target"
    build_seconds = run(["cargo", "build", "--release", "--locked", "--manifest-path",
                         "benchmarks/svm-runner/Cargo.toml", "--target-dir", str(target)], output / "runner-build.log")
    runner = target / "release/gosvm-svm-runner"
    verifier = stage / "verify"
    go_flags = ["-ldflags=-linkmode=external"] if platform.system() == "Darwin" else []
    verifier_build = run(["go", "build", *go_flags, "-o", str(verifier), "./cmd/verify"], output / "verifier-build.log")
    if platform.system() == "Darwin":
        subprocess.run(["codesign", "--force", "--sign", "-", str(verifier)], check=True)
    base_env = dict(os.environ)
    for key in ("GOSVM_TEST_RUNNER", "GOSVM_TEST_TOKEN_ELF", "GOSVM_TEST_FEATURES", "GOSVM_CAPTURE_TOKEN_ELF"):
        base_env.pop(key, None)
    rows = []
    for sample in range(args.samples):
        paths = {}
        for engine in ("validator", "litesvm"):
            work = stage / f"sample-{sample}/{engine}"
            evidence = output / f"sample-{sample}/{engine}"
            (work / "anchor").mkdir(parents=True)
            evidence.mkdir(parents=True)
            paths[engine] = work
            for elf in (ROOT / "build/anchor").glob("*.so"):
                shutil.copyfile(elf, work / "anchor" / elf.name)
            token_elf = stage / f"sample-{sample}/validator-token.so"
            feature_file = stage / f"sample-{sample}/validator-features.json"
            env = dict(base_env, GOSVM_VERIFY_BUILD_DIR=str(work))
            if engine == "validator":
                env["GOSVM_CAPTURE_TOKEN_ELF"] = str(token_elf)
            else:
                env.update(GOSVM_TEST_RUNNER=str(runner), GOSVM_TEST_TOKEN_ELF=str(token_elf),
                           GOSVM_TEST_FEATURES=str(feature_file))
            print(f"Sample {sample + 1}/{args.samples}: {engine} token", flush=True)
            token_seconds = run([str(verifier), "anchor"], evidence / "token.log", env)
            report = load(work / "anchor/tokenswap-verification.json")
            if engine == "validator":
                if not report["active_features"]:
                    raise RuntimeError("validator feature capture is empty")
                save(feature_file, report["active_features"])
                shutil.copyfile(token_elf, output / f"sample-{sample}/validator-token.so")
                shutil.copyfile(feature_file, output / f"sample-{sample}/validator-features.json")
            shutil.copyfile(work / "anchor/tokenswap-verification.json", evidence / "token.json")
            print(f"Sample {sample + 1}/{args.samples}: {engine} bounded", flush=True)
            bounded_seconds = run([str(verifier), "anchor-bounded"], evidence / "bounded.log", env)
            for backend in BACKENDS:
                shutil.copyfile(work / f"anchor/bounded-{backend}/build/sbf-results.json", evidence / f"bounded-{backend}.json")
            rows.append({"sample": sample, "engine": engine, "token_wall_seconds": token_seconds,
                         "bounded_wall_seconds": bounded_seconds, "token_startup_seconds": report["startup_seconds"],
                         "token_fixture_seconds": report["fixture_seconds"],
                         "token_vm_execution_seconds": report.get("runner", {}).get("execution_us", 0) / 1e6
                         if engine == "litesvm" else None})
        compare(paths["validator"], paths["litesvm"])
        print("Equivalent errors, CU, state assertions, CPI checks, rollback, token ELF, and feature IDs", flush=True)
    source_paths = [ROOT / "go.mod", ROOT / "examples/typed-swap/testdata/sbf.json"]
    for directory in ("benchmarks/svm-runner", "internal/testvm", "internal/sbftest", "cmd/verify"):
        source_paths.extend(p for p in (ROOT / directory).rglob("*") if p.is_file())
    source_paths.append(Path(__file__).resolve())
    elfs = list((ROOT / "build/anchor").glob("*.so"))
    summary = {"schema": 1, "date": time.strftime("%Y-%m-%d"), "machine": platform.platform(),
               "samples": args.samples, "timings": rows, "equivalence_passed": True,
               "runner_build_seconds": build_seconds,
               "runner_build_target": "empty target; cached crate sources" if args.cold_runner_build else "existing target",
               "verifier_build_seconds": verifier_build,
               "runner_bytes": runner.stat().st_size, "runner_sha256": digest(runner),
               "verifier_sha256": digest(verifier), "runner_target_logical_bytes": sum(p.stat().st_size for p in target.rglob("*") if p.is_file()),
               "versions": {tool: subprocess.check_output(command, cwd=ROOT, text=True).strip()
                            for tool, command in {"cargo": ["cargo", "--version"], "rustc": ["rustc", "--version"],
                                                  "go": ["go", "version"], "validator": ["solana-test-validator", "--version"],
                                                  "runner": [str(runner), "--version"]}.items()},
               "source_sha256": {str(p.relative_to(ROOT)): digest(p) for p in sorted(set(source_paths))},
               "elf_sha256": {p.name: digest(p) for p in elfs},
               "limitations": ["Shared desktop, fixed validator-then-runner order, no isolated CPU/load control",
                               "Feature IDs agree; activation slots/sysvars and the entire runtime version are not identical",
                               "HTTP fixture adapter has synthetic readiness and confirmation; no consensus or bank simulation",
                               "Fixture state checks cover account data, not fee/rent/bank lifecycle equivalence",
                               "No signed/downloadable runner distribution or embedded-binding comparison yet"]}
    summary["medians"] = {engine: {key: statistics.median(r[key] for r in rows if r["engine"] == engine)
                                    for key in ("token_wall_seconds", "bounded_wall_seconds", "token_startup_seconds", "token_fixture_seconds")}
                          for engine in ("validator", "litesvm")}
    save(output / "summary.json", summary)
    print(json.dumps(summary["medians"], indent=2))
    print(f"Evidence: {output}")


if __name__ == "__main__":
    main()
