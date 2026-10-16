# Real third-party libraries on chain

This module calls four published Go libraries, consumed as ordinary module
dependencies (`go.mod` requires them; `go.sum` pins them):

| Module | Version | Packages used |
| --- | --- | --- |
| `github.com/holiman/uint256` | v1.3.1 | `uint256` |
| `github.com/KyberNetwork/int256` | v0.1.4 | `int256` |
| `github.com/KyberNetwork/uniswapv3-sdk-uint256` | v0.5.5 | `utils`, `constants` |
| `lukechampine.com/uint128` | v1.3.0 | `uint128` |

`Run(out, in)` ([reallibs.go](reallibs.go)) decodes an operation and its
operands from a 256-byte vector and writes every result, flag, error identity
and error text to a 1 KiB output: uint256 arithmetic (including aliased
operands), shifts, square roots, `Log10`, decimal/hex/JSON/SQL-scan text;
int256 arithmetic and text; the Uniswap v3 tick math, full-precision
mul/div, sqrt-price math and `ComputeSwapStep`; uint128's checked and
wrapping arithmetic. A bench group performs one call each for CU
measurement.

## Native and on chain

- **Natively** the package runs against the unmodified modules from the Go
  module cache. [`oracle`](oracle/main.go) runs vectors from stdin.
- **On chain** the same package compiles against the same unmodified
  modules with [trimming](../../docs/GO_PACKAGES.md#trimming) opted in for
  the four library modules. By default every file of a reachable package
  must satisfy goSVM's subset, and each library has files that import
  `math/big`, `fmt`, `database/sql`, go-ethereum or use floats, so the
  default build is rejected (with a hint naming the opt-in). With the
  opt-in, only the declarations `Process` reaches in those packages are
  checked and compiled; the build warns with what it dropped and writes the
  full list beside the program:

```sh
cd examples/real-libraries
GOWORK=off go mod download   # once: the modules and their requirements (go-ethereum too: trimming type-checks every import)
gosvm -target fogo-v0 -o build/program.so \
  -trim github.com/holiman/uint256 -trim github.com/KyberNetwork/int256 \
  -trim 'github.com/KyberNetwork/uniswapv3-sdk-uint256/...' -trim lukechampine.com/uint128 .
# gosvm: warning: trimmed github.com/holiman/uint256: dropped 21 declarations (6 initializers); type-only imports, ...
# gosvm: warning: full list of dropped declarations: build/program.so.trim.json
```

  In a project, the same patterns go in `gosvm.json` as
  `"trim": ["github.com/holiman/uint256", ...]`.

- **Historical trimmed copies.** Before the opt-in existed the example
  compiled against trimmed copies
  ([results](../../results/compiler/2026-10-16-real-libraries/README.md)):
  each module replaced (an ordinary `go.mod` `replace`) by its upstream
  files minus the declarations goSVM rejects, every kept declaration
  byte-identical. [`trim/trim.json`](trim/trim.json) lists each removed
  declaration with the check error that rejected it (or the earlier removal
  it depended on) and the SHA-256 of every upstream and trimmed file;
  [`trim`](trim/main.go) regenerates the copies from the module cache
  (`GOWORK=off go run ./trim -out /tmp/trimmed`) and verifies both hashes.
  The copies are generated, never committed (KyberNetwork/int256 v0.1.4
  ships no license file).

The tests in `internal/compiler` build both forms in a scratch directory:
`TestRealLibrariesDifferential` (host C against native Go),
`TestRealLibrariesSBF` (fogo-v0 and solana-v3 against native Go, plus the CU
table and initialization costs), `TestRealLibrariesWholePackages` (the
default rejection of each unmodified package, with the opt-in hint, and its
compilation under the opt-in) and `TestRealLibraryForms*` (the language
forms the libraries needed). They skip, saying so, when the module cache
lacks the modules. Results:
[trimming opt-in](../../results/compiler/2026-10-16-trim-opt-in/README.md),
[trimmed copies](../../results/compiler/2026-10-16-real-libraries/README.md).

This is a separate module: root `go test ./...` does not build it. Run
`GOWORK=off go vet ./...` here for its own checks.
