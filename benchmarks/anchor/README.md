# Matched Anchor comparison

Two workloads separate the small generated framework from real token transfers.
The experiment pins Anchor/anchor-spl **0.32.2**, platform-tools **v1.51** (Rust
1.84.1-dev / LLVM 19), and **SBF v3**, on validator **3.0.15**. Anchor 0.32.2's
[recommended Solana version is 2.3.0](https://www.anchor-lang.com/docs/updates/release-notes/0-32-2).
This choice preserves the existing backend for all three implementations. It is
not a measurement of the newer Anchor 1.x line.

## Bounded, one-account workload

- **Go:** unmodified generated `examples/typed-swap`, including structs, generated
  account checks, discriminators, codecs, and business logic.
- **Lean Rust:** the original dependency-free bounded program, adapted to the
  identical 32-byte account and 24-byte instruction encoding.
- **Anchor:** `Account<Pool>`, `#[derive(Accounts)]`, typed instruction arguments,
  mutable-account serialization and `#[program]` dispatch. Custom 8-byte
  discriminators match the existing Go starter exactly.

All use the same bounded uint64 arithmetic, reserve limits, fee formula, slippage
checks, overflow checks, and account owner/writable/executable/length policy.
All 14 original fixtures run against each ELF, including persistent swaps and
atomic rollback. Framework error numbers differ; fixture adapters explicitly
map those differences. Go's 486 CU result belongs to this workload.

## Full-width token-transfer workload

`token/` uses `anchor_spl::token`, typed `Account<TokenAccount>` values, a `Signer`,
`Program<Token>`, `has_one`, token mint/authority constraints, PDA seeds with a
stored bump, and two real SPL Token `Transfer` CPIs. It uses native Rust `u128`
for the constant-product quotient. Only the classic Token feature is enabled;
Token-2022, associated-token and other unused Anchor SPL features are disabled.

`go-token/` and `lean-token/` are generated **wire-format adaptations** of the
existing Go SDK and dependency-free Rust programs. `scripts/anchor_prepare.py`
adds the same Anchor pool/instruction discriminators and shifts the payload
offsets. Arithmetic, account validation and CPI implementations stay unchanged.
The Go token implementation uses **manual SDK validation**, not the one-account
project generator. Thus this track does not establish equivalent authoring
convenience for a multi-account Go framework.

All three consume the same account order, 145-byte pool state and 24-byte
instruction, use the same 30-bps rounded-up fee, and produce identical balance
and counter updates for the same numeric inputs. Program/PDA/account addresses
are separate deterministic fixtures per backend. The pool layout is:

```
discriminator[8] | vault_x[32] | vault_y[32] | mint_x[32] | mint_y[32] | bump[1] | swaps[8]
```

The test suite includes 68 successful vectors, 37 invalid vectors, two committed
swaps, failed-second-instruction rollback and failed-second-CPI rollback **per
backend**. It checks full account bytes, expected error categories, and Token CPI
invoke/success counts. Framework constraint evaluation order and error codes are
allowed to differ; the Anchor error map is explicit, not an arbitrary-failure
acceptance test. The Token program ELF hash is captured in the report.

Both Anchor programs include a narrow entry guard to reject surplus instruction
bytes and extra accounts, matching the Go/lean policy. Anchor normally permits
those for instruction decoding/remaining accounts. All typed account parsing,
constraints, dispatch, serialization, and CPI remain Anchor. Default Anchor
instruction-name logging is retained. This is ordinary `Account` serialization,
not an optimized `AccountLoader`/zero-copy alternative. No program implements
initialization, liquidity management, Token-2022, or a deployable complete AMM.

## Reproduce

Install the existing documented toolchains. Fetch dependency sources once,
outside timings, using the pinned Cargo (host Cargo has a separate registry cache
on this machine):

```sh
python3 scripts/anchor_prepare.py
TOOLS="$HOME/.cache/solana/v1.51/platform-tools"
PATH="$TOOLS/rust/bin:$PATH" "$TOOLS/rust/bin/cargo" fetch --locked --manifest-path benchmarks/anchor/token/Cargo.toml
PATH="$TOOLS/rust/bin:$PATH" "$TOOLS/rust/bin/cargo" fetch --locked --manifest-path benchmarks/anchor/bounded/Cargo.toml
python3 scripts/anchor_bench.py
bash scripts/verify-anchor.sh
python3 scripts/anchor_report.py
```

The lockfiles include compatibility pins, notably unicode-segmentation 1.12.0
(the unconstrained newer release requires Rust 1.85). Initial fetch/repair work
is not included in benchmark times. Build profiles match `-O2`, LTO, one codegen
unit, and explicit checked business arithmetic with overflow-checks disabled.

Each contract has one empty-target build followed by three comment edits and
three no-op builds. Edits force recompilation without changing behavior. Targets
are separate, and timed builds run serially with four Cargo jobs. The first
small-program timing pass overlapped the tail of initial validator verification;
the machine is also shared with unrelated work. Wall time, child CPU time, and
load averages are recorded; these are observations, not precise universal ratios.

Timings cover direct Go ELF compilation and `cargo-build-sbf`, including Anchor's
Rust macro expansion. They exclude dependency downloads, compiler installation,
Anchor CLI installation, IDL/TypeScript generation, Go project regeneration, and
validator testing. Retained disk counts are logical project target/output bytes,
not shared compilers, Go caches or Cargo registry downloads. Go's full generated
project workflow was measured separately in `results/ergonomics`.

See [results](../../results/anchor/README.md) for CU, timing, footprint, and the
precise limits on interpreting the comparison.
