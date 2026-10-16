![goSVM — compile Go to Solana programs](assets/gosvm-banner.svg)

**Write Solana and Fogo programs in Go.**

goSVM compiles Go into SVM programs, with generated account validation,
clients, and native Go tests. Share types and business logic between your
on-chain program and an ordinary Go backend. The compiled program runs without
a Go runtime, garbage collector, or scheduler.

The project is experimental, with working token-swap and escrow applications
and scoped ports of real Solana workloads.

## Why goSVM?

- **A shorter edit–build–test loop.** Small applications rebuild in under a second
  in local measurements. Test business logic with Go tooling, then execute the
  compiled program with an SVM runner or local validator.
- **One set of Go types and calculations.** On-chain programs and off-chain
  services import the same model packages. Your backend can use normal Go
  libraries around that shared code.
- **Generated account plumbing.** Declare instructions and accounts; generate
  owner, signer, writable, PDA, and relationship checks, explicit wire codecs,
  Go clients, and an experimental IDL.
- **Real Solana integration.** The SDK supports classic SPL Token transfers,
  signed CPIs, Clock and Rent, account creation and closing, and return data.

## What has been measured?

| Application | Edit build | Native tests + lifecycle SBF tests |
| --- | ---: | ---: |
| Generated token swap | 0.917 s | 0.613 s |
| Generated SOL escrow | 0.520 s | 0.504 s |

These are median local measurements on macOS arm64 with installed tools and warm
caches. The test measurements reuse the compiled SBF artifact.
[Methodology, distributions, and footprint](results/compiler/2026-10-06-milestone2-bench-supported/README.md).

A separate optimized **Whirlpools swap-handler port uses approximately 1.28×
Rust's compute units**, measured as the median paired successful-case ratio.
All 166 scenarios pass three repetitions against scoped and full upstream Rust,
including failed CPIs and rollback. This covers the classic-token, fixed-fee
handler with events omitted at O2; it is a scoped port, not the complete
Whirlpools contract. [Benchmark scope and evidence](docs/WHIRLPOOLS_133_TARGET.md).

These measurements use the historical v1.51 experimental v3 backend.
They do not measure the new Fogo/modern Solana target profiles.

## Try it

Source installation is tested on **macOS arm64**, with Go and host Clang required.
One project builds for both Fogo and Solana; goSVM picks the instruction set,
pinned compiler backend and test runtime for each network. Building Go programs
does not require Cargo or Rust.

```sh
git clone https://github.com/CrocSwap/goSVM.git
cd goSVM
bash scripts/install.sh "$HOME/.local/bin"
export PATH="$HOME/.local/bin:$PATH"

gosvm toolchain install   # Download the pinned backend for Fogo and Solana
gosvm doctor
gosvm new my-swap         # Targets fogo and solana
cd my-swap

gosvm check               # Check the supported Go subset
gosvm test                # Run native Go tests
gosvm build               # build/fogo/program.so and build/solana/program.so
gosvm deploy -network solana-devnet -check   # Confirm the cluster accepts it
```

The starter is a small arithmetic-only swap, with generated bindings and tests.
Edit `program.go` and rebuild. For multi-account applications, explore the
[token swap](examples/full-swap/README.md) and [escrow](examples/escrow/README.md),
or scaffold with `gosvm new -schema 2 -sdk 2 my-program`.

`gosvm test --svm` runs the compiled program for every target in a local SVM
runtime, choosing the matching managed runner; runners are installed from
pinned local packages for now (`gosvm runner install -archive ...`); add
`-target solana -elf other.so` to test an ELF from another backend. `gosvm deploy`
reads the cluster's live feature gates before deploying with a dedicated
per-network payer, and `gosvm call` simulates or sends an instruction.
Pure [Bend libraries](docs/BEND_LIBRARIES.md) join a program with
`gosvm library add`, from published manifests or built from a `.bend` source
(`-bend-source PROOF.bend -root ns.fn`); [Rust crates](docs/RUST_LIBRARIES.md)
with `-rust`. [Targets, deployment and migration](docs/TARGET_PROFILES.md)
cover the details. Public signed CLI and runner downloads are still planned; see
[installation details](docs/DEVELOPMENT.md#start-a-go-project).

## Share code with your backend

The [ordinary Go service](examples/full-swap-service/README.md) imports the swap's
actual types, quote calculation, and generated client:

```go
args := model.SwapArgs{AmountIn: amount, MinOut: minimum}
quote := model.Quote(reserveX, reserveY, args.AmountIn)
instruction := client.EncodeInstructionSwap(args)
```

Only packages reachable from the on-chain entrypoint need to follow the compiler's
Go subset. The service can use HTTP, JSON, databases, and other ordinary Go
facilities in its own code, without installing the compiler or SBF tools.

## Current scope

The compiler supports multi-file programs, user packages, signed and unsigned
[integers](docs/GO_INTEGERS.md) (`int8`…`int64`, `uint8`…`uint64`, `int`, `uint`)
with Go's exact semantics (compound assignment `x op= y` included), structs (with
[`==`, type aliases and `panic`](docs/GO_TYPES.md)), [fixed-size arrays](docs/GO_ARRAYS.md)
of any element type, multiple
return values, methods, `range` loops, constrained pointers, borrowed byte slices,
[generics](docs/GO_GENERICS.md) (monomorphized at compile time), bounded
[heap allocation](docs/GO_ALLOCATION.md) (`new`, `make`, `append`, `copy`,
slices of any element type), [interface values](docs/GO_INTERFACES.md)
(dynamic dispatch, type assertions and switches),
[strings and `error`](docs/GO_STRINGS.md) (UTF-8 `range`, concatenation,
conversions), [function values, closures and `defer`](docs/GO_FUNCTIONS.md),
[maps](docs/GO_MAPS.md) (every operation except iteration, whose order Go
randomizes), [package-level variables and `init`](docs/GO_GLOBALS.md)
(Go's initialization order on every invocation; initialization that is
fully determined, such as computed constants, is
[evaluated at compile time](docs/GO_GLOBALS.md#compile-time-evaluation) and
installed for a few CU; constant tables cost nothing) and the [standard-library packages](docs/GO_STDLIB.md) `errors`,
`strings`, `bytes`, `strconv` (integers), `encoding/binary`, `unicode/utf8`,
`unicode` (case mapping), `math` (integer limits), `io` (errors and
interfaces) and [`math/bits`](docs/GO_BITS.md), compiled from Go 1.22's own
source. Published modules work as ordinary `go.mod` dependencies; holiman/uint256,
KyberNetwork/int256 and the Uniswap v3 math of uniswapv3-sdk-uint256 run on
chain with Go's results ([real libraries](results/compiler/2026-10-16-real-libraries/README.md)),
unmodified, by opting those dependencies in to
[trimming](docs/GO_PACKAGES.md#trimming) (only the declarations `Process`
reaches are checked and compiled; the rest, such as their `math/big`/`fmt`
conversions, are dropped and reported). It lowers
Go through checked C and Solana Clang/LLD to SBF.

Full Go language support is outside the current scope: garbage collection, map
iteration, goroutines, floats, and other standard-library packages (`fmt`,
`math/big`, ...) are not supported. This is an experimental
toolchain without a production audit.
[Language details](docs/DEVELOPMENT.md#supported-subset-and-remaining-blockers).

## Explore

- **Examples:** [bounded starter](examples/typed-swap/README.md),
  [token-swap lifecycle](examples/full-swap/README.md),
  [SOL escrow](examples/escrow/README.md), [Go backend](examples/full-swap-service/README.md).
- **Build applications:** [multi-account schema](docs/SCHEMA2.md),
  [SDK](docs/SDK2.md), [target profiles](docs/TARGET_PROFILES.md),
  [Bend libraries](docs/BEND_LIBRARIES.md), [shared Go packages](docs/GO_PACKAGES.md),
  [SVM test fixtures](docs/SVM_FIXTURES.md).
- **Follow development:** [roadmap](ROADMAP.md),
  [independent developer trial](docs/MILESTONE2_TRIAL.md).
- **Work on goSVM:** [agent orientation](AGENTS.md), [current handoff](HANDOFF.md),
  [development and benchmark reference](docs/DEVELOPMENT.md).
