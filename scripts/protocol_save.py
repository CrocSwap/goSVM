#!/usr/bin/env python3
"""Archive this benchmark without replacing earlier experiment evidence."""
import datetime
import hashlib
import json
import os
from pathlib import Path
import shutil
import statistics
import subprocess

ROOT = Path(__file__).resolve().parents[1]
WORK = ROOT / "build/protocol"
DEST = ROOT / "results/protocol"


def digest(p):
    h = hashlib.sha256()
    with p.open("rb") as f:
        while b := f.read(1024 * 1024):
            h.update(b)
    return h.hexdigest()


def main():
    bench = json.loads((WORK / "benchmark.json").read_text())
    vm = json.loads((WORK / "verification.json").read_text())
    for name in ("protocol_go", "protocol_bench"):
        assert bench["elf"][name]["sha256"] == vm["elf_sha256"][name] == digest(WORK / (name + ".so")), "benchmark and VM artifacts differ"
    required = ["go-contract-cold", "rust-contract-cold", "rust-programtest-cold",
                "rust-programtest-execute", "go-native-tests-execute", "external-validator-verify"]
    for label in required:
        assert bench["measurements"][label]["exit_code"] == 0, label
    assert vm["vectors_per_backend"] == 116 and len(vm["results"]) == 232
    assert vm["committed_lifecycle_instructions_per_backend"] == 20 and vm["atomic_rollback_per_backend"]
    DEST.mkdir(parents=True, exist_ok=True)
    for p in WORK.glob("*.json"):
        # Keep the compact fixture digest in provenance; the generator is the source.
        if p.name != "fixtures.json":
            shutil.copy(p, DEST / p.name)
    logs = DEST / "logs"
    logs.mkdir(exist_ok=True)
    for row in bench["measurements"].values():
        p = ROOT / row["log"]
        shutil.copy(p, logs / p.name)
    for pattern in ("programtest-*.log", "regression-*.log"):
        for p in WORK.glob(pattern):
            shutil.copy(p, logs / p.name)
    before = WORK / "before-word-helpers-logs"
    if before.exists():
        shutil.copytree(before, DEST / before.name, dirs_exist_ok=True)
    before_body = WORK / "go-body-before-word-helpers.txt"
    if before_body.exists():
        shutil.copy(before_body, DEST / before_body.name)
    for name in ("rust_contract_cold_timing", "rust_programtest_cold_timing"):
        p = ROOT / bench[name]["file"]
        shutil.copy(p, DEST / (name + ".html"))
    successes = {b: {r["name"]: r["cu"] for r in vm["results"] if r["backend"] == b and r["code"] == 0} for b in ("go", "rust")}
    assert successes["go"].keys() == successes["rust"].keys()
    differences = [100 * (v / successes["rust"][k] - 1) for k, v in successes["go"].items()]
    summary = {"success_cases_per_backend": len(successes["go"]),
               "paired_go_cu_percent_vs_rust": {"min": min(differences), "median": statistics.median(differences), "max": max(differences)},
               "lifecycle_cu": {b: sum(v for k, v in cases.items() if not k.startswith("kernel-")) for b, cases in successes.items()},
               "successful_case_cu": successes}
    (DEST / "summary.json").write_text(json.dumps(summary, indent=2) + "\n")
    files = subprocess.check_output(["git", "ls-files", "--cached", "--others", "--exclude-standard", "-z"], cwd=ROOT).decode().split("\0")
    sources = {p: digest(ROOT / p) for p in sorted(set(files)) if p and not p.startswith("results/") and (ROOT / p).is_file()}
    toolroot = Path(os.environ.get("SBF_TOOLS", Path.home() / ".cache/solana/v1.51/platform-tools"))
    tools = {n: Path(shutil.which(n)).resolve() for n in ("go", "rustc", "cargo", "cargo-build-sbf", "solana-test-validator")}
    # Hash the selected host tools rather than only their rustup dispatch shim.
    if shutil.which("rustup"):
        for name in ("rustc", "cargo"):
            tools[name] = Path(subprocess.check_output(["rustup", "which", name], cwd=ROOT, text=True).strip())
    tools.update({"sbf-rustc": toolroot / "rust/bin/rustc", "clang": toolroot / "llvm/bin/clang", "ld.lld": toolroot / "llvm/bin/ld.lld"})
    manifest = {"recorded_at": datetime.datetime.now(datetime.timezone.utc).isoformat(), "versions": bench["versions"],
                "source_sha256": sources, "fixtures_sha256": digest(WORK / "fixtures.json"),
                "tools": {n: {"path": str(p), "sha256": digest(p), "bytes": p.stat().st_size} for n, p in tools.items()},
                "results_sha256": {str(p.relative_to(DEST)): digest(p) for p in sorted(DEST.rglob("*")) if p.is_file() and p.name != "source-manifest.json"}}
    (DEST / "source-manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
    print("Saved verified protocol evidence to", DEST)


if __name__ == "__main__":
    main()
