#!/usr/bin/env python3
"""Package and rebuild a self-contained snapshot from the completed CU proof."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import stat
import subprocess
import tarfile

ROOT=Path(__file__).resolve().parents[1]


def sha(p):
    return hashlib.sha256(p.read_bytes()).hexdigest()


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--proof',required=True,type=Path)
    parser.add_argument('--output',required=True,type=Path)
    args=parser.parse_args();proof=args.proof.resolve();out=args.output.resolve()
    stage=ROOT/'build/whirlpool-snapshot'/out.name
    assert proof.is_relative_to(ROOT/'results') and out.is_relative_to(ROOT/'results')
    assert not out.exists() and not stage.exists()
    verified=json.loads((proof/'summary.json').read_text())
    assert verified['passed'] and verified['handler']['median_paired_ratio']<=1.33
    source=ROOT/'build/whirlpool-optimized'/proof.name/'snapshot'
    original=json.loads((source/'manifest.json').read_text())
    assert all(sha(source/name)==want for name,want in original['files'].items())
    out.mkdir(parents=True);stage.mkdir(parents=True)
    snapshot=stage/'whirlpool-optimized';shutil.copytree(source,snapshot)
    # Preserve the proof archive. This delivery archive adds executable mode,
    # source frontend, instructions and an actual extracted-archive rebuild.
    (snapshot/'gosvm').chmod(0o755)
    frontend=ROOT/'results/compiler/2026-10-06-packed-store-canonical-final/frontend.tar.gz'
    shutil.copy2(frontend,snapshot/'frontend.tar.gz')
    readme='''# Experimental optimized Whirlpools snapshot

The matched default-O2 classic-token fixed-fee handler (events omitted) measures
1.280853x scoped Rust, median paired successful-case CU. All 166 scenarios pass
three runs against Go, unchanged scoped Rust and full upstream Rust. Expanded
29,713-case SBF arithmetic probes pass three runs in Go and unchanged Rust.
Individual successful ratios range from 1.226617x to 1.617088x.

Both Go modules include their exact matching SDK. The macOS arm64 CLI is
executable. frontend.tar.gz contains its previously verified complete frontend
source/SDK snapshot. Solana platform-tools v1.51 LLVM is required separately.

From this directory, rebuild the exact default-O2 artifact:

    ./gosvm -no-cache -llvm /absolute/path/to/platform-tools/llvm -o rebuilt.so ./handler

The expected ELF SHA256 is recorded in manifest.json. Record a new complete
application/frontend/SDK pin before benchmark adoption. Original benchmark
defaults remain unchanged. Native Go tests live in the separate go module.
On the tested Go 1.22/macOS host, use an explicit writable GOCACHE and
go test -ldflags=-linkmode=external ./... in that module.

The private applyValidated core is called only by checked Apply and the fully
validating ProcessHandler in the same package. Keep exact-buffer/key validation
and the interval without writes or CPIs intact. Public Apply still validates
independent callers, including buffers changed after earlier validation.

This scoped experiment does not add events, adaptive fees or dynamic arrays.
See the linked project proof/report for fixture/runtime pins and per-case CU.
'''
    (snapshot/'README.md').write_text(readme)
    manifest=dict(original,source_proof_sha256=sha(proof/'summary.json'),delivery_script_sha256=sha(Path(__file__)))
    manifest['files']={str(p.relative_to(snapshot)):sha(p) for p in sorted(snapshot.rglob('*')) if p.is_file() and p.name!='manifest.json'}
    manifest['file_modes']={name:stat.S_IMODE((snapshot/name).stat().st_mode) for name in manifest['files']}
    (snapshot/'manifest.json').write_text(json.dumps(manifest,indent=2)+'\n')
    archive=out/'optimized-snapshot.tar.gz'
    with tarfile.open(archive,'w:gz') as tar:tar.add(snapshot,arcname='whirlpool-optimized')
    extracted=stage/'extracted';extracted.mkdir()
    with tarfile.open(archive,'r:gz') as tar:
        for name,want in manifest['files'].items():
            member=tar.getmember('whirlpool-optimized/'+name)
            assert member.isfile() and hashlib.sha256(tar.extractfile(member).read()).hexdigest()==want
            assert stat.S_IMODE(member.mode)==manifest['file_modes'][name]
        # Only extract our newly generated archive, after checking its entries.
        for member in tar.getmembers():
            parts=Path(member.name).parts
            assert parts[0]=='whirlpool-optimized' and '..' not in parts and not Path(member.name).is_absolute()
            assert member.isdir() or member.isfile()
        tar.extractall(extracted)
    unpacked=extracted/'whirlpool-optimized'
    assert os.access(unpacked/'gosvm',os.X_OK)
    env=dict(os.environ,GOCACHE=str(ROOT/'build/lifecycle-go-cache'),GOWORK='off',GOTOOLCHAIN='local',GOPROXY='off',GOSUMDB='off')
    command=[str(unpacked/'gosvm'),'-no-cache','-llvm',str(Path.home()/'.cache/solana/v1.51/platform-tools/llvm'),'-o',str(out/'rebuilt.so'),str(unpacked/'handler')]
    with (out/'extracted-build.log').open('wb') as log:
        result=subprocess.run(command,cwd=unpacked,env=env,stdout=log,stderr=subprocess.STDOUT)
    assert result.returncode==0,'extracted archive build failed'
    assert sha(out/'rebuilt.so')==verified['handler']['elf_sha256']==sha(unpacked/'handler.so')
    assert all(sha(source/name)==want for name,want in original['files'].items()),'proof source drift'
    summary=dict(passed=True,scope=verified['scope'],source_proof_sha256=sha(proof/'summary.json'),handler=verified['handler'],components=verified['components'],archive_sha256=sha(archive),frontend_archive_sha256=sha(frontend),manifest_sha256=sha(snapshot/'manifest.json'),cli_sha256=sha(unpacked/'gosvm'),command=command,extracted_rebuild_elf_sha256=sha(out/'rebuilt.so'),script_sha256=sha(Path(__file__)))
    (out/'summary.json').write_text(json.dumps(summary,indent=2)+'\n')
    shutil.copyfile(__file__,out/'package.py.txt')
    print('PASS: archive hashes/modes and extracted-source rebuild match the verified CU artifact.',flush=True)


if __name__=='__main__':main()
