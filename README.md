# goSVM

**Write Solana programs in Go.**

goSVM compiles Go into Solana SBF programs, with generated account validation,
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

## Try it

Source installation is tested on **macOS arm64**, with Go and host Clang required.
The backend installer downloads and verifies pinned Solana LLVM tools. Building
Go programs does not require Cargo or Rust.

```sh
git clone https://github.com/CrocSwap/goSVM.git
cd goSVM
bash scripts/install.sh "$HOME/.local/bin"
export PATH="$HOME/.local/bin:$PATH"

gosvm toolchain install
gosvm doctor
gosvm new my-swap
cd my-swap

gosvm check       # Check the supported Go subset
gosvm test        # Run native Go tests
gosvm build       # Compile to build/program.so
```

The starter is a small arithmetic-only swap, with generated bindings and tests.
Edit `program.go` and rebuild. For multi-account applications, explore the
[token swap](examples/full-swap/README.md) and [escrow](examples/escrow/README.md),
or scaffold with `gosvm new -schema 2 -sdk 2 my-program`.

For actual SBF execution, `gosvm test --sbf` uses a separately installed local
validator (tested with Solana 3.0.15). `gosvm test --svm` provides faster local
execution with a separately installed [experimental LiteSVM runner](docs/SVM_RUNNER_PACKAGING.md).
Both run locally without a wallet or public-cluster deployment. Public signed
CLI and runner downloads are still planned; see [installation details](docs/DEVELOPMENT.md#start-a-go-project).

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

The compiler supports multi-file programs, user packages, unsigned integers,
structs, fixed-size scalar arrays, multiple return values, methods, constrained
pointers, and borrowed byte slices. It lowers Go through checked C and Solana
Clang/LLD to SBF.

Full Go language support is outside the current scope: allocation, maps,
interfaces, generics, goroutines, and on-chain standard-library imports are not
supported. This is an experimental toolchain without a production audit.
[Language details](docs/DEVELOPMENT.md#supported-subset-and-remaining-blockers).

## Explore

- **Examples:** [bounded starter](examples/typed-swap/README.md),
  [token-swap lifecycle](examples/full-swap/README.md),
  [SOL escrow](examples/escrow/README.md), [Go backend](examples/full-swap-service/README.md).
- **Build applications:** [multi-account schema](docs/SCHEMA2.md),
  [SDK](docs/SDK2.md), [shared Go packages](docs/GO_PACKAGES.md),
  [SVM test fixtures](docs/SVM_FIXTURES.md).
- **Follow development:** [roadmap](ROADMAP.md),
  [independent developer trial](docs/MILESTONE2_TRIAL.md).
- **Work on goSVM:** [agent orientation](AGENTS.md), [current handoff](HANDOFF.md),
  [development and benchmark reference](docs/DEVELOPMENT.md).
