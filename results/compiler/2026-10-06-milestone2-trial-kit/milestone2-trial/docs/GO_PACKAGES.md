# Experimental shared Go packages

Milestone 2 now supports named imports of ordinary Go packages in the on-chain
entrypoint's module dependency graph. The same named structs, scalar arrays,
constants, codecs and pure functions can be called by native Go clients. Each
package keeps its Go type identity; declarations are not flattened by name.
Aliases such as `import arithmetic "example.com/app/math"` are supported.

The [example application](../examples/shared-packages/README.md) has this structure:

```text
shared-app module
  model     canonical Pool, SwapArgs, Amount and PublicKey types
  math      bounded arithmetic helper
  quote     shared calculation, importing model and math
  wire      explicit codecs returning/accepting model's types
  program   on-chain handler importing model, quote and wire

shared-service module
  main      ordinary Go client importing model, quote and wire
```

The native client decodes and encodes the actual `model.Pool`/`model.SwapArgs`
types and calls the same quote function as the program. It needs ordinary Go,
not the compiler, SBF backend or SDK. The verification harness copies only the
pure libraries into an isolated module and verifies that all non-standard client
dependencies resolve there. The example module is unpublished; its service uses
a normal local module replacement for development. Public module distribution
and schema-2 generated clients remain separate work.

## Resolution and semantics

`gosvm` still accepts one file or a package directory. User imports require a
`go.mod` enclosing the entrypoint. Files are sorted, dependencies are checked
before importers, and object-based symbol names avoid collisions between packages
with identical package/function/type names. Only the root package's `Process`
becomes the ABI entrypoint; a dependency can have its own `Process` function.

The resolver asks installed Go for package/module metadata with `go list -find`.
It does not compile or execute user code, run generators or execute package
initialization. It supports same-module imports, local replacements and module
sources already available in the Go cache. Resolution is offline, using
`GOPROXY=off`, `GOTOOLCHAIN=local` and read-only module mode. A private alternate
mod/sum pair also prevents checksum bookkeeping from modifying the entry module.
Missing dependencies produce source-located errors rather than implicit downloads.

Workspaces and vendor selection are not supported by this increment. Ambient
`GOWORK` and `GOFLAGS` do not select different source graphs. Build constraints
and platform-specific Go filenames are rejected in every on-chain package.
`_test.go` files are not compiled on chain. Go's `internal` visibility rules are
checked; dot/blank imports, import cycles and importing a `main` package are
rejected. `gosvm/solana` remains a reserved, pinned compiler intrinsic, including
when imported by helpers; user module replacements do not change SBF intrinsics.

Every file/function in a reachable on-chain package must satisfy the compiler
subset, including unused helpers. Globals, `init`, method/function values, recursion, interfaces and standard-library
imports remain unsupported. Direct methods and constrained borrows follow the
[pointer](GO_POINTERS.md) and [view](GO_VIEWS.md) rules.
Other packages in the module are outside that graph: a native service can use
HTTP, JSON, databases, goroutines and other normal Go features in its own code.
Keep those dependencies out of shared packages compiled on chain.

Build receipts hash the reachable source files and module metadata, as well as
the frontend and backend identities. Transitive helper edits and added/removed
files invalidate the cache; edits to host-only packages/tests do not. A cached
build compiles the same loaded source snapshot used to compute its receipt.
Resolution still happens before a cache hit, so removed/missing dependencies and
invalid platform selections cannot be hidden behind an old ELF.

## Validation and remaining work

[Current evidence](../results/compiler/2026-10-05-shared-imports-final/README.md)
includes native-Go/generated-C differential vectors, repeated actual-SBF execution,
an isolated ordinary Go client, live dependency-cache mutation and unchanged
prior-workload ELFs. This is a bounded arithmetic/state application using the
legacy ABI, not a token-transfer program or a dependency-heavy protocol benchmark.

```sh
GOCACHE="$PWD/build/lifecycle-go-cache" bash scripts/build-cli.sh build/gosvm
GOCACHE="$PWD/build/lifecycle-go-cache" build/gosvm -arch v3 \
  -o build/shared-program.so examples/shared-packages/program
(cd examples/shared-packages && GOWORK=off go test ./...)
(cd examples/shared-packages/service && GOWORK=off go run .)
python3 scripts/import_verify.py --output results/compiler/new-import-experiment
```

The last command requires the pinned local runner archive and v1.51 LLVM;
`--llvm` selects another installed copy. Choose a new output directory. On this
macOS/Go 1.22 environment, native commands that import HTTP/JSON may need external
linking (`go run -ldflags=-linkmode=external .`) and the workspace-local Go cache.
Root tests do not test these two separate example modules.

[Multiple results/control flow](GO_CONTROL.md), [methods/constrained pointers](GO_POINTERS.md)
and [byte views](GO_VIEWS.md) are implemented. Schema-2 validation/codecs/clients,
typed CPI/lifecycle APIs and the full swap/escrow migration remain milestone 2 work. The handwritten shared-code example is an
import foundation; it does not complete the framework or shared-client gate.
