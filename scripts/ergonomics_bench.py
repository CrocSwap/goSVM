#!/usr/bin/env python3
"""Measure the standalone project workflow without a public RPC or dependency download."""
import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import statistics
import subprocess
import tempfile
import time

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / "results/ergonomics"
WORK = ROOT / "build/ergonomics"


def main():
    OUT.mkdir(parents=True, exist_ok=True)
    WORK.mkdir(parents=True, exist_ok=True)
    report = {"schema": 1, "machine": platform.platform(), "notes": [
        "Shared M2 desktop; installed tools and warm OS caches; scheduling is uncontrolled.",
        "Source bootstrap has an isolated empty GOCACHE. Downloads are disabled/excluded.",
        "Contract clean builds delete only generated build output; frontend and backend are installed.",
        "Edit samples append a comment to invalidate the whole-program build cache.",
        "Native test timings force test execution but allow the shared Go compilation cache.",
        "There is no equivalent Rust framework comparison in this measurement."
    ], "commands": []}
    env = dict(os.environ, GOWORK="off", GOPROXY="off")

    def run(label, args, cwd=ROOT, extra=None):
        args = [str(a) for a in args]
        started = time.perf_counter()
        p = subprocess.run(args, cwd=cwd, env=dict(env, **(extra or {})), text=True,
                           stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        duration = time.perf_counter() - started
        (OUT / (label + ".log")).write_text(p.stdout)
        report["commands"].append({"label": label, "args": args, "cwd": str(cwd),
                                   "seconds": duration, "exit_code": p.returncode})
        print(f"{label}: {duration:.3f}s", flush=True)
        if p.returncode:
            raise RuntimeError(p.stdout)
        return duration

    with tempfile.TemporaryDirectory(prefix="gosvm-ergonomics-") as temp:
        temp = Path(temp)
        cli = WORK / "bin/gosvm"
        run("install", ["bash", "scripts/install.sh", cli.parent])
        run("bootstrap-fresh-cache", ["bash", "scripts/build-cli.sh", WORK / "fresh-gosvm"],
            extra={"GOCACHE": str(temp / "bootstrap-cache")})
        report["bootstrap_cache_bytes"] = sum(p.stat().st_size for p in (temp / "bootstrap-cache").rglob("*") if p.is_file())
        run("doctor", [cli, "doctor"])
        project = temp / "typed-swap"
        run("new", [cli, "new", project])
        run("check", [cli, "check"], project)
        clean, edit, noop, native = [], [], [], []
        source = project / "program.go"
        original = source.read_bytes()
        for n in range(3):
            shutil.rmtree(project / "build", ignore_errors=True)
            clean.append(run(f"clean-{n}", [cli, "build"], project))
            source.write_bytes(original + f"\n// cache-invalidating edit {n}\n".encode())
            edit.append(run(f"edit-{n}", [cli, "build"], project))
            noop.append(run(f"noop-{n}", [cli, "build"], project))
            native.append(run(f"native-{n}", ["go", "test", "-count=1", "./..."], project))
            source.write_bytes(original)
        run("sbf", [cli, "test", "--sbf"], project)
        shutil.copyfile(project / "build/sbf-results.json", OUT / "sbf-results.json")
        elf = (project / "build/program.so").read_bytes()
        report.update({"clean_build_seconds": clean, "edit_build_seconds": edit,
                       "noop_build_seconds": noop, "native_test_seconds": native,
                       "median_seconds": {k: statistics.median(v) for k, v in {
                           "clean": clean, "edit": edit, "noop": noop, "native": native}.items()},
                       "elf_bytes": len(elf), "elf_sha256": hashlib.sha256(elf).hexdigest(),
                       "cli_bytes": cli.stat().st_size,
                       "build_elf_and_receipt_bytes": len(elf) + (project / "build/program.so.gosvm-cache").stat().st_size,
                       "scaffold_bytes": sum(p.stat().st_size for p in project.rglob("*")
                                             if p.is_file() and "build" not in p.relative_to(project).parts),
                       "scaffold_sha256": {str(p.relative_to(project)): hashlib.sha256(p.read_bytes()).hexdigest()
                                           for p in sorted(project.rglob("*"))
                                           if p.is_file() and "build" not in p.relative_to(project).parts}})
    report["source_sha256"] = {str(p.relative_to(ROOT)): hashlib.sha256(p.read_bytes()).hexdigest()
                              for directory in ("internal/compiler", "internal/project", "internal/sbftest", "cmd/gosvm", "solana")
                              for p in sorted((ROOT / directory).rglob("*")) if p.is_file()}
    report["source_sha256"].update({str(p.relative_to(ROOT)): hashlib.sha256(p.read_bytes()).hexdigest()
                                   for p in [ROOT / "scripts/install.sh", ROOT / "scripts/build-cli.sh", Path(__file__)]})
    for name, args in {"go": ["go", "version"], "validator": ["solana-test-validator", "--version"]}.items():
        report[name] = subprocess.check_output(args, text=True).strip()
    (OUT / "benchmark.json").write_text(json.dumps(report, indent=2) + "\n")
    print(json.dumps(report["median_seconds"], indent=2), flush=True)


if __name__ == "__main__":
    main()
