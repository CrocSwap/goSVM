#!/usr/bin/env python3
"""Validate typed sysvars and VM checkpoints; preserve each run in a new directory."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import shutil
import subprocess

ROOT = Path(__file__).resolve().parents[1]


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def run(command, log, env=None):
    with log.open("w") as output:
        subprocess.run(command, cwd=ROOT, env=env, stdout=output,
                       stderr=subprocess.STDOUT, check=True, timeout=300)


def load(path):
    return json.loads(path.read_text())


def save(path, value):
    path.write_text(json.dumps(value, indent=2) + "\n")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--llvm", type=Path, default=Path.home() / ".cache/solana/v1.51/platform-tools/llvm")
    args = parser.parse_args()
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=False)
    stage = ROOT / "build/svm-controls" / output.name
    stage.mkdir(parents=True, exist_ok=False)
    target = ROOT / "build/svm-controls-target"
    manifest = "benchmarks/svm-runner/Cargo.toml"
    run(["cargo", "test", "--release", "--locked", "--offline", "--manifest-path", manifest,
         "--target-dir", str(target)], output / "rust-tests.log")
    run(["cargo", "build", "--release", "--locked", "--offline", "--manifest-path", manifest,
         "--target-dir", str(target)], output / "runner-build.log")
    runner = target / "release/gosvm-svm-runner"
    probe = stage / "probe.so"
    llvm = args.llvm.resolve()
    run([str(llvm / "bin/clang"), "-target", "sbf", "-mcpu=v3", "-O2", "-fno-builtin", "-fPIC",
         "-fno-stack-protector", "-std=c11", "-Werror", "-c", "benchmarks/svm-controls/probe.c",
         "-o", str(stage / "probe.o")], output / "probe-compile.log")
    run([str(llvm / "bin/ld.lld"), "-z", "notext", "-shared", "--Bdynamic", "--strip-all", "--entry",
         "entrypoint", "--script", "internal/compiler/sbf-v3.ld", "--no-undefined", "-o", str(probe),
         str(stage / "probe.o")], output / "probe-link.log")
    flags = ["-ldflags=-linkmode=external"] if platform.system() == "Darwin" else []
    env = dict(os.environ, GOSVM_CONTROLS_RUNNER=str(runner), GOSVM_CONTROLS_ELF=str(probe), GOSVM_TEST_FEATURES="")
    run(["go", "test", *flags, "./internal/testvm", "-run", "^TestSVMControlsIntegration$", "-count=3", "-v"],
        output / "controls.log", env)
    cli = stage / "gosvm"
    run(["bash", "scripts/build-cli.sh", str(cli)], output / "cli-build.log")
    common = [str(cli), "test", "--svm", "-dir", "examples/typed-swap", "-llvm", str(llvm), "-svm-runner", str(runner)]
    env = dict(os.environ, GOSVM_TEST_FEATURES="")
    run(common, output / "cli-full.log", env)
    report = ROOT / "examples/typed-swap/build/svm-results.json"
    shutil.copyfile(report, output / "cli-full.json")
    controls = {"clock": {"slot": 9007199254740993, "epoch_start_timestamp": -23, "epoch": 7,
                           "leader_schedule_epoch": 9, "unix_timestamp": -123456},
                "rent": {"lamports_per_byte_year": 777, "exemption_threshold": 1.5, "burn_percent": 17}}
    save(output / "sysvars.json", controls)
    run([*common, "-svm-sysvars", str(output / "sysvars.json")], output / "cli-sysvars.log", env)
    shutil.copyfile(report, output / "cli-sysvars.json")
    run([str(cli), "test", "--sbf", "-dir", "examples/typed-swap", "-llvm", str(llvm)],
        output / "validator.log", dict(os.environ, GOSVM_TEST_RUNNER=""))
    shutil.copyfile(ROOT / "examples/typed-swap/build/sbf-results.json", output / "validator.json")
    full, controlled, validator = (load(output / name) for name in ("cli-full.json", "cli-sysvars.json", "validator.json"))
    for key in ("cases", "elf_sha256", "fixtures_sha256"):
        assert full[key] == controlled[key] == validator[key], key
    assert controlled["runner"]["sysvars"] == controls
    assert len(full["cases"]) == 14
    # Reject malformed controls and prove failure removed the prior passing report.
    save(output / "invalid-sysvars.json", {"clock": {"slot": 1}})
    with (output / "cli-rejection.log").open("w") as log:
        rejection = subprocess.run([*common, "-svm-sysvars", str(output / "invalid-sysvars.json")],
                                   cwd=ROOT, env=env, stdout=log, stderr=subprocess.STDOUT, timeout=60)
    assert rejection.returncode != 0 and not report.exists(), "invalid controls left a passing report"
    # Leave the project with a current complete passing report.
    run(common, output / "cli-restored.log", env)
    sources = [ROOT / p for p in ("scripts/svm_controls.py", "benchmarks/svm-controls/probe.c",
               "benchmarks/svm-runner/Cargo.toml", "benchmarks/svm-runner/Cargo.lock",
               "benchmarks/svm-runner/src/lib.rs", "internal/testvm/session.go", "internal/testvm/command.go",
               "internal/testvm/controls_integration_test.go", "internal/sbftest/fast.go", "cmd/gosvm/project.go")]
    save(output / "summary.json", {"schema": 1, "passed": True, "probe_repetitions": 3,
         "starter_cases": 14, "fresh_validator_agreement": True, "machine": platform.platform(),
         "runtime_version": full["runtime_version"],
         "probe_success_cu": sorted(set(int(v) for v in re.findall(r"replay \d+: (\d+) CU", (output / "controls.log").read_text()))),
         "artifacts": {p.name: {"bytes": p.stat().st_size, "sha256": digest(p)} for p in (runner, probe, cli)},
         "source_sha256": {str(p.relative_to(ROOT)): digest(p) for p in sources},
         "limitations": ["Clock/Rent only; no automatic epoch/slot-hash/consensus advancement",
                         "Runtime probe is C; Go SDK sysvar authoring is not implemented",
                         "Starter CLI schema is still single-account schema 1",
                         "No full-token or dependency-heavy protocol measurements in this experiment",
                         "macOS arm64 only; no portable timing or memory-footprint claim"]})
    print(f"PASS: sysvar/checkpoint controls and 14 fresh-validator fixture matches. Evidence: {output}")


if __name__ == "__main__":
    main()
