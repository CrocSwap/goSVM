# GoSVM → Qushta: answers on a verified backend — October 14, 2026

Reply to Qushta's "proposed verified backend for goSVM" (2026-10-14) and its
[implementation plan](https://claude.ai/code/artifact/09a983c7-e68e-49c2-a8a8-3b21791e228f).
Citations are to the goSVM repository at `main` (`da6bf04`).

## Decision: the verified backend targets SBPFv3 only, for now

Fogo testnet and mainnet (`solana-core` 20.0.0, read October 9) have **no SBPF
v1–v3 feature gates**, so Fogo accepts only v0 programs; goSVM's `fogo` target
emits v0 for that reason. V3 deploys today on Solana (mainnet, devnet, testnet;
gate `5cC3foj…`), goSVM's `solana-v3` profile.

goSVM's owner confirms that covering only SBPFv3 is fine for now. Fogo's main
role is hosting the on-chain checker that verifies proofs about external
programs; the programs proved by the verified backend are `solana-v3` builds.
Phase 1 should therefore compare against full-swap's `solana-v3` build and run
the v3 runner. A v0 model can be revisited if verified programs later need to
deploy on Fogo itself. See `docs/TARGET_PROFILES.md`.

## 1. Hook point

There is no intermediate representation. `internal/compiler` goes from `go/ast`
+ `go/types` straight to C text (`compiler.go:41-55`); names, array wrappers and
struct typedefs are created on demand during emission. Only two checks run
before emission — recursion (`compiler.go:125-166`) and borrows
(`pointers.go:163`); every other subset rule is a `g.fail` inside the emitter
(e.g. signed arithmetic `compiler.go:605`, `int` variables `:399`, pointer
element kinds `pointers.go:31`). So today the subset definition *is* the C
emitter. The `*.c.txt` files are runtime and ABI templates (entrypoint, account
deserialisation, CPI, syscalls), not codegen templates.

Account decoding and validation are not in the compiler at all: schema 2
(`gosvm.json`) is turned into **Go source** (`zz_gosvm.go`) by
`internal/project`, then compiled like hand-written code.

Proposal: goSVM extracts the inline subset checks into a validator pass that
runs before any backend, and exposes a checked-program value — `TypedProgram`
(`types.go:14`) plus entry kind, borrow summaries and library bindings. The IR
backend walks the typed syntax tree from there. A fork can prototype against
`CompileProgram` right after `checkBorrows` (`compiler.go:167`), but without
the validator split it would see programs the C path rejects.

## 2. Subset used by examples/full-swap

Hand-written handlers are 163 lines (`program.go`, `lifecycle.go`) plus a
101-line model (`model/`); `zz_gosvm.go` (2,555 lines) is generated.

- **Language:** structs and nested structs; struct literals; fixed byte arrays
  (`PublicKey [32]byte`), array literals, whole-array `==`, array conversion;
  one borrowed slice of an array (`xLabel[:]`); pointers only as `&seeds`
  passed to SDK calls; multiple results (`(Bundle, uint64)`); condition-only
  and `break` loops in the model, three-clause loops in generated code; `switch`
  only in the generated dispatcher; typed and untyped constants; `++`,
  short-circuit `||`, variable shifts. Types: `byte` and `uint64` only. No
  user-defined methods, `range`, generics, or 128-bit type — 64×64→128 multiply
  and 128/64 divide are hand-written in `model/quote.go:18-70`.
- **SDK, from handlers:** `token.Balance`, `Transfer`, `TransferSigned` (PDA-
  signed CPI), `CreateSigned` (System create + `InitializeAccount3`), `Load`,
  `CloseSigned`; `pda.Seeds` with `AddKey`/`AddByte`/`AddBytes`.
- **SDK, from the generated adapter:** account accessors, `token.Load`/`Read`,
  `seeds.Matches`, `system.Create` (reads Rent internally),
  `solana.CloseAccount`.
- **Not used:** Clock, direct Rent reads, resize, return data, generic
  `cpi.Invoke`, logging.

No tool reports this today; the list above was produced by reading the code.
goSVM can add a `go/types` pass that reports features and SDK calls per
program if that would help size later phases.

## 3. Failure semantics

Every runtime trap calls the VM `abort` syscall (static hash on v3, legacy
import on v0; `compiler.go:28-37`). There is no custom error code, panic message
or recovery; the instruction fails and the transaction rolls back.

| Case | Behaviour |
| --- | --- |
| Unsigned `+ - *`, unary `-` | Wraps modulo 2^width (computed in u64, truncated to u8/u32) |
| Signed arithmetic | Not in the subset: compile error |
| Division or modulo by zero | `abort` |
| Shift by ≥ width | Result 0, as in Go; no trap |
| Array, slice or slice-expression bounds | `abort` |
| Nil pointer use | `abort` |
| Integer conversion | Truncate or zero-extend (unsigned only; no `uint16`) |
| Bad account index in SDK accessors | `abort` |
| Stack | Recursion is a compile error; no stack check beyond the VM's |

Errors as data: a nonzero `Process` result becomes the transaction's custom
error. Families: entrypoint deserialisation 1001–1004; SDK 2001–2017 and
3001–3024; schema-2 adapter 6000–6009 (`docs/SDK2.md:206-211`,
`docs/SCHEMA2.md:135-141`).

Failed CPI: SDK pre-checks return a code without invoking (e.g. 2001 not
executable, 2005 bad index, 2006 bad metas, 2007 bad seeds, 2008 data over
10,240 bytes; token 3010–3014). Once the invoke syscall runs, a callee failure
fails the whole transaction in the runtime; the caller does not resume. goSVM's
failed-CPI rollback tests run in LiteSVM, not against a proof of the runtime.

## 4. Account validation

Yes: schema 2 in `gosvm.json` is the source of truth; `build` and `test`
always regenerate from it. Format: `internal/project/schema2.go:23-95`
(layouts, instructions, accounts with `kind`, `access`, `signer`, `owner`,
`address`, `pda` seeds, `init`, `close`, key relations, allowed aliases).
Generated checks, in order, with codes: exact account count 6000;
writable/signer/executable 6001; owner 6002; state length and discriminator
6003; fixed address 6005; duplicate keys unless aliased 6006; relations
including token mint and authority 6007; PDA re-derivation 6008; init target
and payer empty 6009; then init (`system.Create`), handler, post-handler close,
write-back of writable state (`docs/SCHEMA2.md:87-188`). PDA bumps are explicit;
there is no bump search and no rent-exemption check.

For declarative IR constraints, read the schema and `WireLayout`
(`schema2.go:99-119`) directly rather than reverse-engineering the generated
Go. Both paths would then derive from one schema, and the existing fixtures
compare them.

## 5. SDK contracts

`docs/SDK2.md` is the stated contract. The native host implementation
(`solana/api.go`) and the SBF C runtime (`internal/compiler/cpi.c.txt`,
`solana.c.txt`) return the same codes. Summary of each primitive full-swap
needs:

- **Token transfer** (`sdk/token/token.go:124-190`): Token program executable
  (2001); both accounts writable (3014); each revalidated as a 165-byte,
  Token-owned, initialised, non-native account (3010/3011); equal mints (3012);
  source authority equals the authority key (3013), and signs for unsigned
  transfers. Then SPL instruction 3. `TransferSigned` does not pre-check that
  the seeds derive the authority; the runtime's signed invoke enforces it.
- **PDA** (`sdk/pda`): at most 16 seeds of at most 32 bytes (3001); `Matches`
  re-derives with an explicit bump.
- **System create** (`sdk/system/system.go:39-135`): System program executable
  (3021); payer and target distinct, writable, non-executable, payer
  System-owned and empty (3022); payer signs, target signs or matches its PDA
  (3023); size ≤ 10,240 (3024); funding is the current Rent minimum.
- **Close** (owned): source owned, writable, non-executable; refund writable
  and distinct; lamports moved, data zeroed, owner set to System (2005,
  2011–2014). Token close: effective close authority must sign or match seeds
  (3013); SPL instruction 9.
- **Rent:** read through `system.MinimumBalance` (3020 on overflow or over 10 MiB).

Not used by full-swap but specified in the same document: Clock (40-byte read),
resize (≤ original + 10,240, zero-filled), return data (≤ 1,024 bytes; no
getter exists).

## 6. Pointers and aliasing

The checker enforces escape and lifetime rules, **not** exclusivity. Aliasing
is allowed by design: writes through one view are visible through others
(`docs/GO_VIEWS.md:42-44`). Enforced (`pointers.go:163-411`):

- no returned borrow of local storage;
- no borrow stored in a variable that outlives its storage scope;
- only plain identifiers hold borrows: pointer fields, arrays of pointers,
  pointers to pointers and pointers to slices are rejected;
- inter-procedural origin summaries, made possible by the recursion ban;
- nil checks on every dereference;
- byte views are `[]byte` only, bounds-checked, never copied.

Not enforced: SDK account-data borrows are not invalidated after a CPI or
resize; that is left to the program (`docs/GO_VIEWS.md:73-76`). A stricter
phase 1 rule ("no two live mutable borrows overlap") would reject nothing in
full-swap's hand-written code, whose only pointers are `&seeds` arguments.

## 7. SBF targets

| goSVM target | Profile | SBPF | e_machine / e_flags | Platform tools / LLVM |
| --- | --- | --- | --- | --- |
| `fogo` | `fogo-v0` | v0 | 263 / 0 | v1.57 / 22.1.2 |
| `solana` | `solana-v3` | v3 | 247 / 3 | v1.57 / 22.1.2 |
| (historical) | `legacy-v1.51-v3` | experimental v3, different layout | 263 / 3 | v1.51 / 19.1.7, not deployable |

Toolchains are content-pinned for macOS arm64. Solana still accepts v0 until
SIMD-0500 (`B8JJXCy5…`) activates, but `gosvm deploy` always ships v3 there.
The phase 0 harness used v1.51 / Agave 3.0.15, the historical profile.

## 8. Testing an alternative .so

- **`gosvm svm-test -target <profile> -elf other.so -fixtures F`** tests any ELF
  whose header matches the profile and reports CU per step. full-swap's
  `testdata/svm.json` (13 cases) and `testdata/lifecycle.json` (29 cases) pin
  no CU, so another backend passes if its accounts, return values and errors
  match byte for byte. Always pass `-target`; the default is the legacy profile.
- **`gosvm test --svm`** always tests goSVM's own build. goSVM can add an
  `-elf` override if useful.
- **Native Go tests** run the source on the host; they never load a .so.
- **Whirlpools (166 scenarios)** lives in the sibling `goSVM-benchmarks` repo
  (`scripts/run_budget_suite.py`) and already runs non-goSVM ELFs. It is
  qualified only for the legacy profile and runner. It is also a different
  workload from full-swap: the 1.28× figure is the scoped Whirlpools handler
  on the legacy profile, not the token swap. Compare the phase 1 backend
  against full-swap's own C/LLVM CU on the same profile.

## 9. Reproducible builds

Partly established. Fresh `-no-cache` rebuilds are asserted byte-identical for
the legacy v1.51 backend (`scripts/whirlpool_optimized_verify.py:98-99`,
`scripts/whirlpool_snapshot.py:92-96`). For v1.57 (both current targets) there
is no rebuild check: the repeated ELF hashes in current results are cache hits
or one build tested repeatedly. Determinism is designed for (ordered
emission, fixed flags, `--strip-all`, no build ID, content-pinned clang/lld),
and goSVM will add a fresh-rebuild check for both v1.57 targets. The IR
backend's determinism is separate and on Qushta's side.

## 10. Collaboration

Confirmed by goSVM's owner:

- Phase 1 prototypes in a Qushta fork, without waiting for goSVM.
- goSVM lands all four enabling changes on `main` now, in Qushta's priority
  order: the subset validator split and checked-program interface, with parsed
  schema 2 as a Go value; a per-program JSON feature and SDK-usage report; a
  v1.57 fresh-rebuild check; `test --svm -elf`.
- The IR backend is merged into goSVM only behind an explicit opt-in flag
  that leaves the C/LLVM path unchanged, with full-swap's 42 fixtures under
  `svm-test -target solana-v3` as the shared gate.
- Review on the goSVM side: the repository owner.
- Error codes, check order and trap behaviour are a stable contract:
  [ERROR_CONTRACT.md](../ERROR_CONTRACT.md). Changes are announced to Qushta
  before landing; new codes in unused numbers are not changes.
- Baseline: annotated tag `qushta-phase1-baseline` on `main`, pushed to origin.
