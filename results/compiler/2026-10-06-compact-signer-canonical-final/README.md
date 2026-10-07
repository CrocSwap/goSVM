# Canonical compact Whirlpools handler rebuild

PASS: fresh canonical CLI and SDK-2 scaffold reproduce the preserved compact handler ELF exactly: 84,192 bytes, SHA-256 `19daf911c6348dbb6ba86df8452a0637fe6c415ce9322d582a91232741e83bad`. All 166 scenarios pass three repetitions, with identical errors, CU and logs to the benchmark reference.

The handler/shared Go module were copied into a goSVM-owned staging directory. The sibling benchmark checkout was only read; original/default handlers and SDK pins were not modified. Native handler tests pass. The fixed-fee classic-token handler omits events; the independent benchmark retains its Rust parity and SDK-only control evidence.

[Summary](summary.json) records source and tool hashes and commands; `canonical-0.json` through `canonical-2.json` retain per-case results. The final run additionally checks fixture/runtime/transaction-maker hashes, feature profile and compute budget against the reference. See [adoption contract and regressions](../../../docs/COMPACT_SIGNER_ADOPTION.md).

Reproduce into a new directory: `python3 scripts/compact_signer_canonical_verify.py --output results/compiler/NEW-EXPERIMENT`. Requires the preserved sibling benchmark source, fixture, transaction maker, runner and reference report plus the installed pinned LLVM tooling.
