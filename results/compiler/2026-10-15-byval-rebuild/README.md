# Aggregate parameters on SBF: fix for all code, new reference ELFs

October 15, 2026. Local only (`network_validation: false`); pinned runners
0.5.0 (fogo-v0) and 0.6.0 (solana-v3), platform-tools v1.57.

## Why the reference hashes changed

Clang passes C struct parameters (goSVM's strings, slices, interfaces,
structs and arrays) `byval`: the IR promises the callee its own copy, so the
caller passes the address of its own variable (a package variable, a heap
field, an address-taken local, an interface's box), and LLVM forwards copies
into such arguments. The SBF backend lowers `byval` as that plain pointer and
copies nothing. Host builds are correct; on SBF:

1. a callee that writes its parameter (`s = s[1:]`, `p.f++`, `a[i] = v`, a
   value receiver's field) writes the caller's variable — found by the
   standard-library agent in overlay code, fixed there only;
2. (found in this audit) a callee that changes the caller's variable through
   a package variable or pointer and then reads its parameter reads the new
   value: LLVM treats byval memory as private and reads it (or passes its
   address on) after the write;
3. (found in this audit) `t := append(s, x)` with spare capacity lengthened a
   package-level or heap `s`: the C runtime's `gosvm_grow` wrote `s.len` in
   its parameter.

The user decided to fix this for all code. `internal/compiler/byval.go` now
copies, at entry of every function and literal, each aggregate parameter it
writes (plainly) and each one it may read after a write that can change its
caller's variable (through an opaque pointer, so the copy is taken at entry
and never forwarded). A write counts unless it provably reaches only the
call's own locals or only values of types the variable cannot hold; callees
are summarized by the types they write and the parameters they write
through. Copies over 128 bytes stay whole in memory (an asm takes their
address): letting LLVM split the full swap's 700-byte accounts struct rebuilt
it at every `return a, code`, growing the ELF by 13 KB (fogo-v0) and 20 KB
(solana-v3) for 210 CU less. `gosvm_grow` returns a new header. Programs
with no such parameters and no `append` keep their C.

So the full-swap ELFs changed on purpose. Only these full-swap functions
write aggregate parameters (none needs an opaque copy):

| Function | Parameter | Size | Copy |
| --- | --- | ---: | --- |
| `Swap` (program.go) | `a SwapAccounts` | ~700 B | whole, in memory |
| `ManagedSwap` (lifecycle.go) | `a` (accounts) | ~700 B | whole, in memory |
| `CreatePool` (lifecycle.go) | `a` (accounts) | ~700 B | whole, in memory |
| `pda.Seeds.AddKey` (SDK) | `key [32]byte` | 32 B | scalars |
| `pda.Address` (SDK) | `program [32]byte` | 32 B | scalars |

## New reference

[metrics.json](metrics.json) is `scripts/verify_rebuild.py`'s output (two
fresh `-no-cache` builds from different paths, identical, no embedded host
strings) and is now the script's default `--reference`:

| Target | Workload | Bytes (before → after) | SHA-256 (before → after) |
| --- | --- | ---: | --- |
| fogo-v0 | full swap | 69,032 → 68,808 | `4697a9f5…` → `a66c5a97b1dea963d17a9438a0da3bd5f9ccd55627e0a4386b23da53b8aa6bbf` |
| solana-v3 | full swap | 69,352 → 69,200 | `2b5f21d9…` → `b419245ba541da5338f88dbb354c493af06d56fe560dd84555b8420f610282f5` |
| fogo-v0 | clock2 | 5,304 | `0f768370…` unchanged |
| solana-v3 | clock2 | 4,400 | `54f34b65…` unchanged |
| legacy v1.51 (target-less) | full swap | 68,352 → 68,472 | `4e96d2bb…` → `05f52dd22dc3dfdb36788283c45c6f550dc1bf9126a998cdaa1daa25f230dd4a` |

The legacy row is a `gosvm build -no-cache` of `examples/full-swap` without
`-target`; its CU was not remeasured. Historical results keep the old hashes
as their record.

## Behaviour and CU (full swap, both targets)

[acceptance/metrics.json](acceptance/metrics.json) is
`scripts/verify_target_profiles.py` (no Bend manifest): the generated token
swap (13 scenarios), the managed lifecycle (29) and `clock2` (12), three
repetitions per profile, with the native Go tests and an unchanged
application/IDL check. It passed; ELFs and per-case CU are identical across
repetitions. Every case's errors, account bytes and rollback match the
[October 7 reference](../2026-10-07-target-profiles-final/metrics.json)
(asserted when computing [cu-deltas.json](cu-deltas.json)); only CU differs:

| Case (step) | fogo-v0 | solana-v3 |
| --- | --- | --- |
| wide swap | 47,605 → 48,208 (+603) | 38,314 → 38,913 (+599) |
| input-frozen / output-frozen / input-overflow | +563 / +602 / +565 | +559 / +599 / +560 |
| stateful swaps (3 steps) | +603, +603, +1,168 | +599, +599, +1,159 |
| lifecycle create / swap / drain | +792 / +587 / 0 | +807 / +583 / 0 |
| create failures after the copies | +717 to +795 | +724 to +805 |
| atomic create + bad swap rollback | +1,340 | +1,351 |
| swap slippage after create | +548 | +544 |
| early failures (signer, owner, alias, PDA, bump, mint, close paths) | 0 | 0 |
| clock2 (all) | 0 | 0 |

The cost is the entry copy of the 700-byte accounts struct in `Swap`,
`ManagedSwap` and `CreatePool` (the runtime's word-copy loop) on the paths
that reach those functions.

## Reproduction of the bug in Go

`examples/byval-values` (21 cases; `TestByvalDifferential` host vs native Go,
`TestByvalSBF` on both targets, 84 SBF cases) keeps strings, slices, structs
and arrays in memory and passes them to non-inlined callees. At the
previous commit (`8843887`) the host matched native Go in every case and SBF
differed in 15 of 21 on both targets:

| Case | Path | Before (SBF) | After |
| --- | --- | --- | --- |
| 0, 1 | package variables written by callees | differ | match |
| 2, 3 | heap object fields | differ | match |
| 4 | address-taken locals | differ | match |
| 5 | value receivers | differ | match |
| 6 | value method through an interface (the box was written) | differ | match |
| 7 | method values and function values | differ | match |
| 8 | deferred calls' saved arguments passed on to writers | differ | match |
| 9 | closures with aggregate parameters | match | match |
| 10 | results replacing the argument's variable (`gp = swapP(gp)`, sret) | match | match |
| 11 | string results of a writer (`gs = rest(gs)`) | differ | match |
| 12 | slice and array elements | differ | match |
| 13 | map values (read as copies) | match | match |
| 14 | the same variable passed twice | match | match |
| 15 | a parameter passed on to writers by a non-writing callee | differ | match |
| 16 | callee changes the caller's variable via a package variable/pointer, then reads its parameter | differ | match |
| 17 | same through a pointer to an address-taken local | match | match |
| 18 | `clobberI`: interface parameter, box replaced through a pointer | differ | match |
| 19 | string parameter after the package variable is replaced | match | match |
| 20 | `append` with spare capacity on package/heap slices | differ | match |

Other paths audited: the C runtime's helpers (only `gosvm_grow` wrote an
aggregate parameter), sret results (LLVM keeps the result separate from a
byval argument naming the same memory: case 10), library (Bend/Rust) calls
(they pass pointers and lengths only), interface dispatch and function-value
dispatchers (they pass arguments through; the callee's copy covers them),
deferred-call records (private to each `defer`; case 8 covers what they pass
on) and heap-moved parameters (already copied to the heap at entry).
`TestByvalCopies` checks which parameters are copied and how.

## Other ELFs

See [elf-changes.md](elf-changes.md): every SBF ELF the root test suites
build, compared between the previous commit and this one.

## Reproduce

```sh
bash scripts/build-cli.sh build/gosvm && bash scripts/build-cli.sh build/target-gosvm
python3 scripts/verify_rebuild.py --output <new dir> \
  --reference results/compiler/2026-10-14-v157-rebuild/metrics.json
python3 scripts/verify_target_profiles.py --output <new dir> \
  --v0-runner <0.5.0 runner> --v3-runner <0.6.0 runner>
go test -ldflags=-linkmode=external ./internal/compiler -run 'TestByval|TestCorpus' -count=1
```

Runners: `gosvm runner install -archive` with the packages in
[runner-install-complete](../../svm/2026-10-05-runner-install-complete/README.md)
and [runner-0.6.0-package](../../svm/2026-10-09-runner-0.6.0-package/README.md),
into a scratch `GOSVM_CACHE`. Logs and raw runner reports are local-only.
