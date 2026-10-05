#!/usr/bin/env python3
"""Summarize only matching verified ELF/build artifacts; preserve prior results."""
import hashlib
import json
from pathlib import Path
import statistics
ROOT=Path(__file__).resolve().parents[1]
OUT=ROOT/'results/anchor'
def read(p):return json.loads((OUT/p).read_text())
def main():
 b=read('benchmark.json');token=read('token-verification.json')
 names={'go':'Go','lean':'Lean Rust','anchor':'Anchor 0.32.2'}
 rows=[];summary={}
 for workload in ('bounded','token'):
  for backend in ('go','lean','anchor'):
   case=b['cases'][workload+'-'+backend]
   runtime='lean-rust' if backend=='lean' else backend
   if workload=='bounded':
    v=read('bounded-'+runtime+'-verification.json');assert v['elf_sha256']==case['elf_sha256']
    cu=next(r['cu'] for r in v['cases'] if r['name']=='swap')
   else:
    filename='go-token.so' if backend=='go' else 'anchor_lean_token.so' if backend=='lean' else 'anchor_token_bench.so'
    assert token['elf_sha256'][filename]==case['elf_sha256']
    cu=next(r['cu'] for r in token['results'] if r['name']=='wide-swap' and r['backend']==runtime)
   summary[workload+'-'+backend]=dict(case,cu=cu)
   size=case['retained_artifact_bytes'];disk=f'{size/1048576:.2f} MiB' if size>=1048576 else f'{size/1024:.1f} KiB'
   rows.append(f"| {workload} | {names[backend]} | {cu:,} | {case['cold_seconds']:.2f} s | {case['edit_median_seconds']:.2f} s | {case['noop_median_seconds']:.3f} s | {case['elf_bytes']:,} B | {disk} |")
 stats={}
 for backend in ('go','lean-rust','anchor'):
  vals=[r['cu'] for r in token['results'] if r['backend']==backend and r['code']==0]
  stats[backend]={'count':len(vals),'min':min(vals),'median':statistics.median(vals),'max':max(vals)}
 sources={}
 for directory in ('benchmarks/anchor','internal/compiler','solana','cmd/verify'):
  for p in sorted((ROOT/directory).rglob('*')):
   if p.is_file():sources[str(p.relative_to(ROOT))]=hashlib.sha256(p.read_bytes()).hexdigest()
 for p in (ROOT/'scripts').glob('*anchor*'):
  if p.is_file():sources[str(p.relative_to(ROOT))]=hashlib.sha256(p.read_bytes()).hexdigest()
 (OUT/'summary.json').write_text(json.dumps({'cases':summary,'successful_token_CU':stats,'source_sha256':sources},indent=2)+'\n')
 header='''# Anchor comparison results

Measured October 4, 2026 on the shared Apple M2 desktop, using pinned Anchor
0.32.2, platform-tools v1.51 / SBF v3, and validator 3.0.15.

**CU is mixed; build time and artifact footprint favor Go.** The 486-CU generated
starter beats this Anchor implementation, but the full token-transfer swap does
not. This is not evidence of a general Go runtime advantage over Anchor.

| Workload | Implementation | Valid swap CU | Clean build (one sample) | Edit build (median of 3) | No-op (median of 3) | ELF | Retained project artifacts |
|---|---|---:|---:|---:|---:|---:|---:|
'''
 tail='''

The bounded track uses the actual generated Go project. The token track uses the
existing manually written Go SDK program, adapted to the same discriminated wire
format as Anchor and lean Rust. A generated multi-account Go framework is not
implemented yet. “Swap” in the bounded track means arithmetic/state mutation;
only the token track executes two real SPL Token transfers.

The wide token case consumes 9,290 CU inside SPL Token for every implementation.
Go's total is 20,329 CU versus Anchor's 19,431 (Go uses **4.6% more**; Anchor uses
4.4% less). The small starter is 486 versus 768 CU (Go uses **36.7% less**).
Anchor's default instruction-name log is included. These are typed `Account`
programs, not zero-copy/hand-optimized Anchor variants, and not Anchor 1.x.

Correctness: 315 token simulations and 42 bounded simulations, 6 committed token
swaps and 6 token rollback checks, plus all bounded fixtures submitted and checked
for persistent state. Invalid-path error categories and CPI traces are explicit.
Successful token paths check full bytes of the pool and four token accounts
against independent arithmetic; rollback checks verify unchanged state.

The build rows measure ELF compilation with installed tools and downloaded
sources, not first-machine setup. Rust profiles match optimization level 2, LTO,
and one codegen unit. Token-2022 and unrelated Anchor SPL features are disabled.
IDL/TS generation, Go project regeneration, native Rust test harness compilation,
shared Go/Cargo caches, and downloaded toolchain sizes are excluded. The cold
rows are single observations; edits/no-ops have three samples. Wall/CPU times
and load averages are retained because this desktop is not isolated benchmark
hardware. See the [methodology](../../benchmarks/anchor/README.md) for overlap
and compatibility limitations. No Basanos sources or targets were changed.

One follow-up candidate is CPI marshaling: our Go and lean Rust adapters pass
all eight account infos to each token CPI, whereas Anchor SPL passes the three
required infos. That is a source-level difference worth isolating, **not a
measured attribution** of the CU gap. This benchmark does not change the Go SDK
or tune the implementations after observing which one wins.

- [Raw build timings, commands, tools, and source hashes](benchmark.json)
- [Token verification, Token program hash, errors, and per-case CU](token-verification.json)
- [Generated Go bounded verification](bounded-go-verification.json)
- [Lean Rust bounded verification](bounded-lean-rust-verification.json)
- [Anchor bounded verification](bounded-anchor-verification.json)
- [Summary and final source manifest](summary.json)

Reproduce with `scripts/anchor_bench.py`, `scripts/verify-anchor.sh`, then
`scripts/anchor_report.py`, following the dependency preparation steps in the
methodology. Earlier results directories are preserved as historical snapshots.
'''
 (OUT/'README.md').write_text(header+'\n'.join(rows)+tail)
 print(json.dumps(stats,indent=2))
if __name__=='__main__':main()
