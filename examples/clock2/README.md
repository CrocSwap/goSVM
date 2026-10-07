# SDK-2 Clock consumer probe

This low-level compiler fixture calls the real runtime `solana.Clock` API into
caller-owned storage. It writes all five Clock words into a small owned record,
rejects a negative Unix timestamp or time regression, and exercises a simple
uint64 reward delta. Its rate overflow returns zero growth for the test probe;
this does not implement or validate Orca's U128 reward math.

[The proof](../../results/compiler/2026-10-05-clock2-supported/README.md) uses a fresh
SDK-2 scaffold, then compiles this probe through its pinned module. Twelve
scenarios/15 transactions run three times, covering signed/unsigned extremes,
controlled Clock changes, unchanged time, invalid output lengths, handler failure
and multi-instruction rollback. SDK-1 does not acquire the new API.

```sh
python3 scripts/clock2_verify.py --output results/compiler/new-clock-proof
```

Run from the repository root with v1.51 LLVM and the pinned local runner archive.
The fixture has no token transfers and is not a Whirlpools production handler.
