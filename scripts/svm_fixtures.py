#!/usr/bin/env python3
"""Export independently checked token fixtures and exercise the general SVM CLI."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import subprocess

ROOT = Path(__file__).resolve().parents[1]


def run(command, log, env=None):
    with log.open("w") as out:
        subprocess.run(command, cwd=ROOT, env=env, stdout=out,
                       stderr=subprocess.STDOUT, check=True, timeout=300)


def load(path):
    return json.loads(path.read_text())


def save(path, value):
    path.write_text(json.dumps(value, indent=2) + "\n")


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--llvm", type=Path, default=Path.home() / ".cache/solana/v1.51/platform-tools/llvm")
    args = parser.parse_args()
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=False)
    stage = ROOT / "build/svm-fixtures" / output.name
    (stage / "anchor").mkdir(parents=True, exist_ok=False)
    for name in ("go-token.so", "anchor_lean_token.so", "anchor_token_bench.so"):
        shutil.copyfile(ROOT / "build/anchor" / name, stage / "anchor" / name)
    target = ROOT / "build/svm-fixtures-target"
    manifest = "benchmarks/svm-runner/Cargo.toml"
    run(["cargo", "test", "--release", "--locked", "--offline", "--manifest-path", manifest,
         "--target-dir", str(target)], output / "rust-tests.log")
    run(["cargo", "build", "--release", "--locked", "--offline", "--manifest-path", manifest,
         "--target-dir", str(target)], output / "runner-build.log")
    runner = target / "release/gosvm-svm-runner"
    cli, verifier = stage / "gosvm", stage / "verify"
    run(["bash", "scripts/build-cli.sh", str(cli)], output / "cli-build.log")
    flags = ["-ldflags=-linkmode=external"] if platform.system() == "Darwin" else []
    llvm = args.llvm.resolve()
    probe = stage / "probe.so"
    run([str(llvm / "bin/clang"), "-target", "sbf", "-mcpu=v3", "-O2", "-fno-builtin", "-fPIC",
         "-fno-stack-protector", "-std=c11", "-Werror", "-c", "benchmarks/svm-controls/probe.c", "-o", str(stage / "probe.o")],
        output / "probe-compile.log")
    run([str(llvm / "bin/ld.lld"), "-z", "notext", "-shared", "--Bdynamic", "--strip-all", "--entry", "entrypoint",
         "--script", "internal/compiler/sbf-v3.ld", "--no-undefined", "-o", str(probe), str(stage / "probe.o")], output / "probe-link.log")
    run(["go", "test", *flags, "./internal/testvm", "-run", "^TestSVMControlsIntegration$", "-count=3", "-v"],
        output / "controls.log", dict(os.environ, GOSVM_CONTROLS_RUNNER=str(runner), GOSVM_CONTROLS_ELF=str(probe), GOSVM_TEST_FEATURES=""))
    run(["go", "build", *flags, "-o", str(verifier), "./cmd/verify"], output / "verifier-build.log")
    if platform.system() == "Darwin":
        subprocess.run(["codesign", "--force", "--sign", "-", str(verifier)], check=True)
    fixture = output / "token-fixtures.json"
    env = dict(os.environ, GOSVM_VERIFY_BUILD_DIR=str(stage), GOSVM_TEST_RUNNER="",
               GOSVM_EXPORT_SVM_FIXTURES=str(fixture), GOSVM_CAPTURE_TOKEN_ELF="")
    run([str(verifier), "anchor"], output / "fresh-validator.log", env)
    shutil.copyfile(stage / "anchor/tokenswap-verification.json", output / "fresh-validator.json")
    baseline = load(output / "fresh-validator.json")
    save(output / "validator-features.json", baseline["active_features"])
    env = dict(os.environ, GOSVM_TEST_FEATURES=str(output / "validator-features.json"))
    common = [str(cli), "svm-test", "-elf", str(stage / "anchor/go-token.so"), "-fixtures", str(fixture), "-runner", str(runner)]
    report = output / "general-results.json"
    for sample in range(3):
        run([*common, "-report", str(report)], output / f"general-{sample}.log", env)
        shutil.copyfile(report, output / f"general-{sample}.json")
    data = load(report)
    by_name = {case["name"]: case for case in data["cases"]}
    for row in baseline["results"]:
        if row["backend"] != "go":
            continue
        actual = by_name[row["name"]]["steps"][0]
        assert actual["cu"] == row["cu"] and actual["error"] == baseline["observed_errors"]["go/" + row["name"]]
    assert data["elf_sha256"] == baseline["elf_sha256"]["go-token.so"]
    assert data["program_sha256"]["token"] == baseline["token_program_elf_sha256"]
    assert data["runner"]["active_features"] == baseline["active_features"]
    for sample in range(3):
        assert load(output / f"general-{sample}.json")["cases"] == data["cases"]
    # Scenario selection runs all ordered steps, rather than individual txs.
    selected = output / "selected-results.json"
    run([*common, "-run", "^stateful-swaps", "-report", str(selected)], output / "selected.log", env)
    picked = load(selected)["cases"]
    assert len(picked) == 1 and len(picked[0]["steps"]) == 3
    # Dependency integrity failures must clear a previous report.
    corrupt = load(fixture)
    corrupt["programs"][0]["sha256"] = "00" * 32
    bad_fixture = output / "bad-image-fixtures.json"
    save(bad_fixture, corrupt)
    with (output / "image-rejection.log").open("w") as log:
        result = subprocess.run([str(cli), "svm-test", "-elf", str(stage / "anchor/go-token.so"),
                                 "-fixtures", str(bad_fixture), "-runner", str(runner), "-report", str(report)],
                                cwd=ROOT, env=env, stdout=log, stderr=subprocess.STDOUT, timeout=60)
    assert result.returncode != 0 and not report.exists(), "checksum failure left a stale report"
    # Check a useful account/byte diagnostic by corrupting one expectation.
    wrong_state = load(fixture)
    state = wrong_state["cases"][0]["steps"][0]["expect"]["accounts"][0]["state"]
    state["data"] = ("00" if state["data"][:2] != "00" else "01") + state["data"][2:]
    state_fixture = output / "bad-state-fixtures.json"
    save(state_fixture, wrong_state)
    with (output / "state-rejection.log").open("w") as log:
        result = subprocess.run([str(cli), "svm-test", "-elf", str(stage / "anchor/go-token.so"),
                                 "-fixtures", str(state_fixture), "-run", "^wide-swap$", "-runner", str(runner),
                                 "-report", str(report)], cwd=ROOT, env=env, stdout=log, stderr=subprocess.STDOUT, timeout=60)
    assert result.returncode != 0 and not report.exists()
    assert "account v0_a0" in (output / "state-rejection.log").read_text() and "byte 0" in (output / "state-rejection.log").read_text()
    # Sysvars stay raw until the runner's complete typed-field validation.
    incomplete = load(fixture)
    incomplete["sysvars"] = {"clock": {"slot": 42}}
    invalid_controls = output / "incomplete-clock-fixtures.json"
    save(invalid_controls, incomplete)
    with (output / "clock-rejection.log").open("w") as log:
        result = subprocess.run([str(cli), "svm-test", "-elf", str(stage / "anchor/go-token.so"),
                                 "-fixtures", str(invalid_controls), "-runner", str(runner), "-report", str(report)],
                                cwd=ROOT, env=env, stdout=log, stderr=subprocess.STDOUT, timeout=60)
    assert result.returncode != 0 and not report.exists()
    controlled = load(fixture)
    controlled["sysvars"] = {"clock": {"slot": 9007199254740993, "epoch_start_timestamp": -23,
                                       "epoch": 7, "leader_schedule_epoch": 9, "unix_timestamp": -123456}}
    clock_fixture = output / "controlled-clock-fixtures.json"
    save(clock_fixture, controlled)
    clock_report = output / "controlled-clock-results.json"
    run([str(cli), "svm-test", "-elf", str(stage / "anchor/go-token.so"), "-fixtures", str(clock_fixture),
         "-run", "^wide-swap$", "-runner", str(runner), "-report", str(clock_report)], output / "controlled-clock.log", env)
    assert load(clock_report)["runner"]["sysvars"]["clock"] == controlled["sysvars"]["clock"]
    # Preserve schema-1 project behavior on the same runner.
    run([str(cli), "test", "--svm", "-dir", "examples/typed-swap", "-svm-runner", str(runner)],
        output / "starter.log", env)
    shutil.copyfile(ROOT / "examples/typed-swap/build/svm-results.json", output / "starter.json")
    assert len(load(output / "starter.json")["cases"]) == 14
    # Also exercise general fixtures through native-test/generate/build workflow.
    run([str(cli), "test", "--svm", "-dir", "examples/typed-swap", "-svm-runner", str(runner),
         "-svm-fixtures", str(ROOT / "examples/typed-swap/testdata/svm.json")], output / "project-general.log", env)
    shutil.copyfile(ROOT / "examples/typed-swap/build/svm-results.json", output / "project-general.json")
    assert len(load(output / "project-general.json")["cases"]) == 3
    run([*common, "-report", str(report)], output / "general-restored.log", env)
    sources = [ROOT / "scripts/svm_fixtures.py"]
    for name in ("internal/sbftest", "internal/testvm", "cmd/gosvm", "cmd/verify", "benchmarks/svm-runner/src"):
        sources.extend(p for p in (ROOT / name).rglob("*") if p.is_file())
    sources.extend(ROOT / "benchmarks/svm-runner" / name for name in ("Cargo.toml", "Cargo.lock"))
    sources.append(ROOT / "examples/typed-swap/testdata/svm.json")
    sources.append(ROOT / "benchmarks/svm-controls/probe.c")
    save(output / "summary.json", {"schema": 1, "passed": True, "samples": 3, "machine": platform.platform(),
         "fixture_format": data["fixture_format"], "runtime_version": data["runtime_version"],
         "scenarios": len(data["cases"]), "steps_per_run": sum(len(case["steps"]) for case in data["cases"]),
         "matched_go_simulations": len([r for r in baseline["results"] if r["backend"] == "go"]),
         "fixture_sha256": digest(fixture), "source_sha256": {str(p.relative_to(ROOT)): digest(p) for p in sorted(set(sources))},
         "artifacts": {p.name: {"bytes": p.stat().st_size, "sha256": digest(p)} for p in (cli, runner, probe, stage / "anchor/go-token.so", output / "spl-token.so")},
         "limitations": ["General CLI executes LiteSVM only; fresh validator comparison uses the existing independent harness",
                         "All general scenarios submit; original differential corpus mostly simulates",
                         "Seeded token accounts; account initialization/closing lifecycle remains open",
                         "Only macOS arm64 executed; no timing or memory guarantee", "Dependency-heavy protocol not run"]})
    print(f"PASS: {len(data['cases'])} general scenarios, three runs, fresh-validator Go CU/error matches. Evidence: {output}")


if __name__ == "__main__":
    main()
