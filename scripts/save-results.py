#!/usr/bin/env python3
"""Save a verified experiment snapshot after the commands in README have run."""
import datetime
import hashlib
import json
import os
from pathlib import Path
import shutil
import statistics
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
os.chdir(ROOT)
TOOLS = Path(os.environ.get("SBF_TOOLS", Path.home() / ".cache/solana/v1.51/platform-tools"))


def digest(path):
    h = hashlib.sha256()
    with path.open("rb") as f:
        while b := f.read(1024 * 1024):
            h.update(b)
    return h.hexdigest()


def read(name):
    return json.loads((ROOT / "build" / (name + ".json")).read_text())


def main():
    bench, verified = read("tokenswap-benchmark"), read("tokenswap-verification")
    for backend, filename in (("go", "tokenswap-go.so"), ("rust", "tokenswap_rust.so")):
        assert bench["elf_sha256"][backend] == verified["elf_sha256"][filename] == digest(ROOT / "build" / filename), "benchmark/VM artifacts differ"
    scale, scale_vm = read("scaling-benchmark"), read("scaling-verification")
    for row in scale["rows"]:
        for backend in ("go", "rust"):
            matched = [r for r in scale_vm["results"] if r["functions"] == row["functions"] and r["backend"] == backend]
            assert len(matched) == len(row["vectors"]) and all(r["elf_sha256"] == row["elf_sha256"][backend] for r in matched)
    destinations = ("tokenswap-benchmark", "tokenswap-verification", "scaling-benchmark", "scaling-verification")
    for name in destinations:
        shutil.copy(ROOT / "build" / (name + ".json"), ROOT / "results" / (name + ".json"))
    # Optional historical artifacts are not prerequisites for a fresh reproduction.
    for src, dst in (("tokenswap-before-cache-benchmark.json", "tokenswap-exploratory-benchmark.json"),
                     ("tokenswap-o1-verification.json", "tokenswap-o1-verification.json"),
                     ("clang-profile.txt", "clang-o2-profile.txt")):
        if (ROOT / "build" / src).exists():
            shutil.copy(ROOT / "build" / src, ROOT / "results" / dst)
    summary = {}
    successes = {}
    for backend in ("go", "rust"):
        rows = [r for r in verified["results"] if r["backend"] == backend and r["code"] == 0]
        successes[backend] = {r["name"]: r for r in rows}
        cu = [r["cu"] for r in rows]
        summary[backend] = {"successes": len(rows), "cu_min": min(cu), "cu_median": statistics.median(cu), "cu_max": max(cu),
                            "caller_plus_cpi_overhead_median": statistics.median(r["cu"] - r["token_cu"] for r in rows)}
    assert successes["go"].keys() == successes["rust"].keys()
    delta = [100 * (r["cu"] / successes["rust"][name]["cu"] - 1) for name, r in successes["go"].items()]
    summary["paired_go_cu_percent_vs_rust"] = {"min": min(delta), "median": statistics.median(delta), "max": max(delta)}
    (ROOT / "results/summary.json").write_text(json.dumps(summary, indent=2) + "\n")
    files = subprocess.check_output(["git", "ls-files", "--cached", "--others", "--exclude-standard", "-z"]).decode().split("\0")
    sources = {p: digest(ROOT / p) for p in sorted(set(files)) if p and not p.startswith("results/") and (ROOT / p).is_file()}
    tools = {"go": Path(shutil.which("go")), "clang": TOOLS / "llvm/bin/clang", "ld.lld": TOOLS / "llvm/bin/ld.lld",
             "rustc": TOOLS / "rust/bin/rustc", "cargo-build-sbf": Path(shutil.which("cargo-build-sbf")),
             "solana-test-validator": Path(shutil.which("solana-test-validator"))}
    provenance = {name: {"path": str(path.resolve()), "sha256": digest(path)} for name, path in tools.items()}
    versions = {name: subprocess.check_output([str(path), "version" if name == "go" else "--version"], text=True).strip()
                for name, path in tools.items()}
    manifest = {"recorded_at": datetime.datetime.now(datetime.timezone.utc).isoformat(), "platform_tools": "v1.51", "target": "sBPF v3",
                "python": sys.version, "versions": versions, "tools": provenance, "source_sha256": sources,
                "results_sha256": {p.name: digest(p) for p in sorted((ROOT / "results").glob("*")) if p.is_file() and p.name != "source-manifest.json"}}
    (ROOT / "results/source-manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
    print("Saved verified results, CU summary and source/tool provenance to results/")


if __name__ == "__main__":
    main()
