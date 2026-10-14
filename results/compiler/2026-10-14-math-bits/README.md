# math/bits on SBF: vectors, traps and mulDiv CU (October 14, 2026)

Local only (`network_validation: false`). This is new compiler support for
`import "math/bits"` ([semantics](../../../docs/GO_BITS.md)). `metrics.json`
holds the compact numbers. Raw fixtures, ELFs and runner reports are local-only,
under `build/math-bits-2026-10-14/`.

## Questions

1. Do math/bits calls compiled for fogo-v0 and solana-v3 give the same results
   as Go's `math/bits`, and trap exactly where Go panics?
2. Does full-swap's hand-written `mulDiv`, rewritten with `bits.Mul64` and
   `bits.Div64`, cost more or less CU per call?
3. Are programs that do not use math/bits unchanged?

## Workloads

Neither workload is the bounded starter, the full token swap transaction or the
dependency-heavy protocol. `examples/full-swap` itself was not modified.

- `internal/compiler/testdata/bitsprobe/program.go` is a `Process(solana.Context)`
  program. Account 0 holds 48-byte records (op, a, b, c, then r0, r1), and each
  instruction applies one math/bits function per record.
- `internal/compiler/testdata/bitscu/program.go` is the same harness without the
  vector dispatcher, so each `mulDiv` has exactly one call site, as in
  full-swap:
  - `mulDivHand` is `examples/full-swap/model/quote.go`'s `mulDiv`, copied
    unchanged.
  - `mulDivBits` is the same contract written with `bits.Mul64` and
    `bits.Div64` (see [GO_BITS.md](../../../docs/GO_BITS.md)).
  - A baseline mode runs the same loop, loads and store with no multiply or
    divide.

Inputs are `mulDiv(y, net, x+net)` as full-swap's `Quote` passes them: 64
records per class, the same records for every mode.

- **narrow:** reserves up to 4e9 and `y*net < 2^64`. This is the hand-written
  version's `hi == 0` fast path.
- **wide:** reserves up to 1e18 and `y*net >= 2^64`.
- **skewed:** wide inputs with a small input reserve (1e6..4e9) against a large
  output reserve (1e15..1.8e19), so the divisor is small.

CU per call is (transaction CU − baseline transaction CU) / 64. Mode dispatch
in the loop differs by at most one comparison between `hand` and `bits`.

## Pins

- macOS arm64, Go 1.22.0 (`GOWORK=off GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off`).
- Backend: `~/.cache/solana/v1.57/platform-tools/llvm` (LLVM 22.1.2). Content
  pins verified by `target.CheckBackend`, equal to `ModernBackendHashes`.
- Runners, installed from the local packages into a scratch `GOSVM_CACHE`:
  - fogo-v0: gosvm-svm-runner 0.5.0 (LiteSVM 0.8.2), sha256 `886ea293…`
  - solana-v3: 0.6.0 (LiteSVM 0.13.0), sha256 `550bac10…`
- Compiler: this branch's working tree. The SBF tests call `compiler.Build`
  directly, with `-O2` and no cache.

## Results

### Correctness

| Check | fogo-v0 | solana-v3 |
| --- | --- | --- |
| SBF vectors (Mul64, Div64, Add64, LeadingZeros64, Sub64, Rem64, Div32, Rem32, TrailingZeros64, OnesCount64, Reverse64, RotateLeft64(−3), Add32, Mul32) | 4,261 pass | 4,261 pass |
| Trap transactions (Div64 `y==0`, `y<=hi`; Rem64 `y==0`; Div32 `y==0`, `y<=hi`; Rem32 `y==0`) | 11 × `ProgramFailedToComplete`, state rolled back | same |
| Rem64/Rem32 with `hi >= y` (no trap) | pass | pass |
| ELF imports | validated (`abort` only; no `__multi3`, no `__udivti3`) | validated, `--no-undefined` |

On the host, `TestBitsDifferentialAgainstGo` compares 552,163 vectors over 115
operations with Go's `math/bits`. All results agree, and all 24,408 traps occur
exactly where Go panics.

### mulDiv CU per call

fogo-v0 and solana-v3 give identical numbers.

| Inputs | hand-written | math/bits | Difference |
| --- | ---: | ---: | ---: |
| narrow (`hi == 0`) | 50.0 | 47.0 | −3.0 (−6%) |
| wide (reserves to 1e18) | 144.1 | 151.3 | +7.2 (+5%) |
| skewed (small divisor) | 252.1 | 146.5 | −105.7 (−42%) |

Transaction CU (64 calls, plus the harness baseline of 7,537 CU): narrow
10,737 vs 10,545; wide 16,757 vs 17,219; skewed 23,673 vs 16,911.

Both versions compute the full 128-bit product in the same way. The difference
is in normalizing the divisor:

- The hand-written version shifts the divisor one bit per loop iteration. That
  is cheap when the divisor is close to 2^64 (wide: about 2^60, about 4
  iterations), and costly when it is small (skewed: about 2^32, about 32
  iterations).
- `bits.LeadingZeros64` is a fixed six-step binary search, so `bits.Div64` costs
  about the same for every divisor.

For full-swap's realistic, balanced pools the hand-written version is 7 CU
cheaper per wide call. For skewed pools, or the `hi == 0` path, the math/bits
version is cheaper.

### Byte identity for programs without math/bits

- Without a math/bits call, the compiler emits exactly the C it emitted before.
  The runtime template and the result aggregates for math/bits tuples are added
  only for programs that call math/bits.
- `TestCorpusMatchesPreCheckCompiler` (312 recorded programs) passes unchanged:
  no verdict or C hash changed.
- `scripts/verify_rebuild.py` with the rebuilt CLI gave fresh, identical
  rebuilds that match the pinned hashes:
  - full-swap fogo-v0: `4697a9f572a0609da6a2063d6e323e1dfa1f1f77f2fe373abffcf64078cfcfc0`
  - full-swap solana-v3: `2b5f21d964375b2645d6071c53f0b63af6a7dbe2cd8ee32a63f9c5976c6cba9e`
  - clock2: `0f768370…` (fogo-v0) and `54f34b65…` (solana-v3)

## Reproduce

```sh
GOWORK=off GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off bash scripts/build-cli.sh build/gosvm
export GOSVM_CACHE=$PWD/build/gosvm-cache   # or the default cache, if runners are installed
build/gosvm runner install -archive <0.5.0 package>   # fogo-v0
build/gosvm runner install -archive results/svm/2026-10-09-runner-0.6.0-package/gosvm-svm-runner-0.6.0-darwin-arm64.tar.gz
GOSVM_BITS_RESULTS=$PWD/build/math-bits-2026-10-14 GOWORK=off GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off \
  go test -ldflags=-linkmode=external -count=1 -run 'TestBitsSBF|TestBitsDifferential' -v ./internal/compiler/
GOWORK=off GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off python3 scripts/verify_rebuild.py --output <scratch dir>
```

`metrics.json` was assembled from `build/math-bits-2026-10-14/*/summary.json` and
the runner reports. Fixtures are deterministic (fixed seeds). Their SHA-256
values are recorded in `metrics.json`.

## Scope

- One host (macOS arm64) and local LiteSVM runners only. Public-network
  compatibility is not established.
- The CU comparison isolates `mulDiv`. It is not a full-swap transaction
  measurement.
- The host differential oracle is the arm64 build of `math/bits`. amd64 behaves
  the same way, including the intrinsified `Add64`/`Sub64`; this was checked
  with a Rosetta build, not in the test.
