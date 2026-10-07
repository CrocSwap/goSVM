#!/usr/bin/env python3
"""Capture matched fixtures and compare HTTP, stdio, and same-core embedding."""
import argparse
import base64
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
BASELINE = ROOT / "results/svm/2026-10-05-litesvm"


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def load(path):
    return json.loads(path.read_text())


def save(path, value):
    path.write_text(json.dumps(value, indent=2) + "\n")


def run(command, log, env=None, cwd=ROOT):
    started = time.monotonic()
    with log.open("w") as out:
        subprocess.run(command, cwd=cwd, env=env, stdout=out, stderr=subprocess.STDOUT,
                       check=True, timeout=300)
    return time.monotonic() - started


def summarize(output, runner, library, driver, capture, build_times, samples):
    replay = load(output / "replays.json")
    groups = {}
    for row in replay["timings"]:
        key = (row["trace"], row["transport"], row["batch"])
        groups.setdefault(key, []).append(row)
    medians = [{"trace": key[0], "transport": key[1], "batch": key[2],
                "calls": rows[0]["calls"],
                **{field: statistics.median(r[field] for r in rows)
                   for field in ("startup_seconds", "fixture_seconds", "total_seconds")}}
               for key, rows in sorted(groups.items())]
    sources = [ROOT / "scripts/svm_transport.py"]
    for name in ("benchmarks/svm-runner", "benchmarks/svm-transport", "internal/testvm",
                 "internal/sbftest", "cmd/gosvm", "cmd/verify"):
        sources.extend(p for p in (ROOT / name).rglob("*") if p.is_file())
    summary = {"schema": 1, "samples": samples, "machine": platform.platform(), "equivalence_passed": True,
               "medians": medians, "host_build_seconds": build_times,
               "build_state": "installed tools; existing Cargo target and Go caches",
               "baseline": "2026-10-05-litesvm/sample-0/validator; reuse snapshot, no fresh validator run",
               "baseline_summary_sha256": digest(BASELINE / "summary.json"),
               "artifacts": {p.name: {"bytes": p.stat().st_size, "sha256": digest(p)}
                             for p in (runner, library, driver)},
               "source_sha256": {str(p.relative_to(ROOT)): digest(p) for p in sorted(set(sources))},
               "trace_sha256": {p.name: digest(p) for p in sorted((output / "traces").glob("*.jsonl"))},
               "limitations": [f"{samples} seeded shuffled repetitions on a shared desktop; no isolated CPU/load",
                               "Replay includes response validation but excludes Go fixture/signature construction",
                               "Startup includes VM initialization; embedded dlopen and all shutdowns are excluded",
                               "Same pinned Rust core through an experimental C ABI, not the separate upstream pure-Go engine",
                               "Only bounded and token programs; dependency-heavy protocol not run"]}
    save(output / "summary.json", summary)
    shutil.copyfile(capture / "anchor/tokenswap-verification.json", output / "token-capture.json")
    for backend in ("go", "lean-rust", "anchor"):
        shutil.copyfile(capture / f"anchor/bounded-{backend}/build/sbf-results.json", output / f"bounded-{backend}-capture.json")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--samples", type=int, default=7)
    args = parser.parse_args()
    if not 1 <= args.samples <= 20:
        parser.error("samples must be 1..20")
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=False)
    traces = output / "traces"
    traces.mkdir()
    stage = ROOT / "build/svm-transports" / output.name
    (stage / "capture/anchor").mkdir(parents=True, exist_ok=False)
    target = ROOT / "build/svm-transport-target"
    rust_seconds = run(["cargo", "build", "--release", "--locked", "--manifest-path",
                        "benchmarks/svm-runner/Cargo.toml", "--target-dir", str(target)], output / "rust-build.log")
    runner = target / "release/gosvm-svm-runner"
    suffix = "dylib" if platform.system() == "Darwin" else "so"
    library = target / f"release/libgosvm_svm_runner.{suffix}"
    go_flags = ["-ldflags=-linkmode=external"] if platform.system() == "Darwin" else []
    verifier = stage / "verify"
    driver = stage / "compare"
    verifier_seconds = run(["go", "build", *go_flags, "-o", str(verifier), "./cmd/verify"], output / "verifier-build.log")
    env = dict(os.environ, GOWORK="off")
    driver_seconds = run(["go", "build", *go_flags, "-o", str(driver), "."], output / "driver-build.log", env, ROOT / "benchmarks/svm-transport")
    if platform.system() == "Darwin":
        for binary in (verifier, driver):
            subprocess.run(["codesign", "--force", "--sign", "-", str(binary)], check=True)
    capture = stage / "capture"
    for elf in (ROOT / "build/anchor").glob("*.so"):
        shutil.copyfile(elf, capture / "anchor" / elf.name)
    env = dict(os.environ, GOSVM_RUNNER_TRACE=str(traces), GOSVM_TEST_RUNNER=str(runner),
               GOSVM_TEST_FEATURES=str(BASELINE / "sample-0/validator-features.json"),
               GOSVM_TEST_TOKEN_ELF=str(BASELINE / "sample-0/validator-token.so"),
               GOSVM_VERIFY_BUILD_DIR=str(capture))
    run([str(verifier), "anchor"], output / "token-capture.log", env)
    run([str(verifier), "anchor-bounded"], output / "bounded-capture.log", env)
    # Confirm the refactored HTTP core against the immutable validator snapshot.
    token = load(capture / "anchor/tokenswap-verification.json")
    validator = load(BASELINE / "sample-0/validator/token.json")
    for key in ("results", "observed_errors", "elf_sha256", "token_program_elf_sha256"):
        if token[key] != validator[key]:
            raise RuntimeError(f"capture differs from validator snapshot: {key}")
    if token["runner"]["active_features"] != validator["active_features"]:
        raise RuntimeError("capture feature IDs differ from validator snapshot")
    for backend in ("go", "lean-rust", "anchor"):
        a = load(capture / f"anchor/bounded-{backend}/build/sbf-results.json")
        b = load(BASELINE / f"sample-0/validator/bounded-{backend}.json")
        for key in ("cases", "elf_sha256", "fixtures_sha256"):
            if a[key] != b[key]:
                raise RuntimeError(f"bounded capture differs: {backend}/{key}")
    known = load(BASELINE / "summary.json")["elf_sha256"]
    for trace in traces.glob("trace-*.jsonl"):
        with trace.open() as f:
            images = json.loads(f.readline())["config"]["programs"]
        if len(images) == 4:
            name = "token"
        else:
            checksum = hashlib.sha256(base64.b64decode(images[-1]["elf"])).hexdigest()
            elf = next(n for n, h in known.items() if h == checksum)
            name = {"go-bounded.so": "bounded-go", "anchor_lean_bounded.so": "bounded-lean-rust",
                    "anchor_bounded_bench.so": "bounded-anchor"}[elf]
        trace.rename(trace.with_name(name + ".jsonl"))
    run([str(driver), "-traces", str(traces), "-runner", str(runner), "-library", str(library),
         "-output", str(output / "replays.json"), "-samples", str(args.samples)], output / "replay.log")
    summarize(output, runner, library, driver, capture,
              {"rust_existing_target": rust_seconds, "verifier": verifier_seconds, "cgo_driver": driver_seconds}, args.samples)
    print(f"PASS: complete responses agree across all transports. Evidence: {output}")


if __name__ == "__main__":
    main()
