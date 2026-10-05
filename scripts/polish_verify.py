#!/usr/bin/env python3
"""Check the installed CLI as a developer would, preserving installation evidence.
--download performs a fresh official download into an isolated empty cache.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import time

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / "results/polish"
CLI = ROOT / "build/gosvm"


def main():
    p = argparse.ArgumentParser()
    p.add_argument("--download", action="store_true")
    args = p.parse_args()
    OUT.mkdir(parents=True, exist_ok=True)
    report = {"commands": [], "fresh_download": args.download}
    with tempfile.TemporaryDirectory(prefix="gosvm polish ") as temp:
        temp = Path(temp)
        cache = temp / "cache" if args.download else ROOT / "build/polish/cache"
        env = dict(os.environ, GOSVM_CACHE=str(cache), GOWORK="off", GOPROXY="off")
        env.pop("SBF_LLVM", None)

        def run(name, command, cwd=ROOT, expect=0, extra=None):
            begin = time.perf_counter()
            result = subprocess.run([str(x) for x in command], cwd=cwd, env=dict(env, **(extra or {})),
                                    stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
            elapsed = time.perf_counter() - begin
            (OUT / f"{name}.log").write_text(result.stdout)
            report["commands"].append({"name": name, "args": [str(x) for x in command],
                                       "seconds": elapsed, "returncode": result.returncode})
            print(f"{name}: {elapsed:.3f}s", flush=True)
            if result.returncode != expect:
                raise RuntimeError(result.stdout)
            return result.stdout

        run("build-cli", ["bash", "scripts/build-cli.sh", CLI])
        run("help", [CLI])
        run("subcommand-help", [CLI, "build", "--help"])
        run("typo", [CLI, "buidl"], expect=2)
        run("toolchain-install", [CLI, "toolchain", "install"])
        run("toolchain-idempotent", [CLI, "toolchain", "install"])
        run("toolchain-status", [CLI, "toolchain", "status"])
        receipt = next(cache.glob("toolchains/*/*/receipt.json"))
        report["toolchain"] = json.loads(receipt.read_text())
        report["backend_bytes"] = sum(p.stat().st_size for p in receipt.parent.rglob("*") if p.is_file())
        project = temp / "custom-swap"
        run("new-custom-module", [CLI, "new", "-module", "example.org/protocol/swap/v2", project])
        run("check-from-client-offline", [CLI, "check"], cwd=project / "client", extra={"PATH": ""})
        run("invalid-check-flag", [CLI, "check", "--no-cache"], cwd=project, expect=1)
        run("doctor", [CLI, "doctor"], cwd=project / "client")
        filtered = run("native-filtered", [CLI, "test", "--", "-run", "TestConstraintsAndNoWritesOnFailure", "-count=1", "-v"], cwd=project / "client")
        assert "=== RUN   TestConstraintsAndNoWritesOnFailure" in filtered
        assert "=== RUN   TestSwapReference" not in filtered
        run("build-managed", [CLI, "build", "--no-cache"], cwd=project / "client")
        run("test-sbf", [CLI, "test", "--sbf"], cwd=project)
        shutil.copyfile(project / "build/sbf-results.json", OUT / "sbf-results.json")
        elf = (project / "build/program.so").read_bytes()
        report["elf_bytes"] = len(elf)
        report["elf_sha256"] = hashlib.sha256(elf).hexdigest()
        report["cli_bytes"] = CLI.stat().st_size
    report["source_sha256"] = {str(p.relative_to(ROOT)): hashlib.sha256(p.read_bytes()).hexdigest()
                              for directory in ("internal", "cmd/gosvm", "solana")
                              for p in sorted((ROOT / directory).rglob("*")) if p.is_file()}
    (OUT / "verification.json").write_text(json.dumps(report, indent=2) + "\n")
    print("PASS: fresh standalone workflow, managed backend, and actual SBF", flush=True)


if __name__ == "__main__":
    main()
