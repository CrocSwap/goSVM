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
initialization at build time; package initialization runs on chain, at the
start of every invocation. It supports same-module imports, local replacements and module
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

By default every file/function in a reachable on-chain package must satisfy
the compiler subset, including unused helpers; dependency packages can opt in
to [trimming](#trimming) instead. Package-level variables and `init`
functions are supported in every package, with Go's initialization order
(imported packages first, in Go 1.21's package order) on every invocation
([package-level variables](GO_GLOBALS.md)); other packages' variables are read,
written, addressed and called as `pkg.V`. Function values and interfaces are
supported too ([function values](GO_FUNCTIONS.md),
[interfaces](GO_INTERFACES.md)). `math/bits` and the
[standard-library packages](GO_STDLIB.md) `errors`, `math` (integer limits),
`encoding/binary`, `unicode/utf8`, `unicode` (case mapping), `strings`,
`bytes` and `strconv` can be imported; they need no module resolution.
Recursion and other standard-library imports remain unsupported. Direct methods and constrained borrows follow the
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

## Third-party modules

Published modules work the same way: a program's `go.mod` requires them, the
module cache holds them (`go mod download`), and the resolver reads them
offline. [examples/real-libraries](../examples/real-libraries/README.md)
requires holiman/uint256 v1.3.1, KyberNetwork/int256 v0.1.4,
KyberNetwork/uniswapv3-sdk-uint256 v0.5.5 and lukechampine.com/uint128
v1.3.0. The Uniswap SDK module requires go-ethereum and other heavy modules
for packages and files the on-chain program does not use; with Go's pruned
module graphs (go 1.17+) the resolver's `go list -find` resolved the
libraries from a module cache holding only the four modules (go-ethereum
absent): a requirement's sources are needed only when a reached package
imports it. (A native build of the same program needs them, because the
unmodified `utils` package imports go-ethereum, and so does
[trimming](#trimming), which type-checks the unmodified packages whole.)

No package of the four compiles **unmodified** on chain by default, because
every file of a reachable package must satisfy the subset and each package
has files that import `math/big`, `fmt`, `database/sql` or other packages
that are not available, or use floats ([results](../results/compiler/2026-10-16-real-libraries/README.md#whole-package-compilation)).
The example therefore opts the four modules in to [trimming](#trimming)
and compiles them unmodified from the module cache. (Before trimming existed
it resolved each module, through an ordinary `replace`, to a trimmed copy
generated from the module cache, every kept declaration byte-identical; the
generator, `examples/real-libraries/trim`, is kept to reproduce those
[historical results](../results/compiler/2026-10-16-real-libraries/README.md).)

<a id="trimming"></a>
## Trimming (opt-in)

By default goSVM checks every declaration of every package in the on-chain
import graph, so a package it accepts fits the subset as a whole, as Go
compiles every function. Real libraries rarely do: a big-integer package
also converts to `math/big`, formats with `fmt` or implements
`database/sql` interfaces, none of which a program needs on chain. A
program can therefore **opt in, per dependency, to trimming**: for the
packages it names, only the declarations `Process` reaches are checked and
compiled; the rest are dropped and reported. The opt-in is explicit and
scoped so that a build never silently accepts different behaviour, and the
program's own code stays fully checked.

```json
{ "trim": ["github.com/holiman/uint256", "github.com/KyberNetwork/uniswapv3-sdk-uint256/..."] }
```

- **Where.** `"trim"` in `gosvm.json` for projects (`build`, `test`,
  `generate`, `check`, `features`); a repeatable `-trim <pattern>` flag for
  low-level builds (`gosvm -trim P ... dir`, also with `-emit-c`) and for
  `gosvm features <dir>`. `compiler.ReadOptions.Trim` /
  `compiler.BuildOptions.Trim` set `compiler.Program.Trim` for the Go API.
- **Patterns.** An import path names that package; `path/...` names it and
  every package below it (`go list` syntax). Matching is on import paths,
  across modules. The standard-library overlay, `math/bits` and the SDK are
  never trimmed. The **entry package is trimmed only when a pattern names
  it exactly** (`example.com/app/...` does not trim the entry package
  `example.com/app`): trim dependencies, not your program.
- **Default unchanged.** Without `"trim"`, resolution, checking, the C and
  every message are exactly as before. A whole-package rejection located in
  a module package (an import path starting with a domain name) other than
  the entry package now ends with a hint naming the opt-in:
  `...; to check only the declarations Process reaches in package "P", opt in with "trim" in gosvm.json or -trim (docs/GO_PACKAGES.md#trimming)`.

### What is kept

A trimmed package is still **parsed and type-checked as a whole** (every
declaration, as Go would). Reachability then starts from every declaration
of the packages that are not trimmed (the program's own code and the
standard-library overlay), from `Process` when the entry package itself is
trimmed, and from every `func init()` of a trimmed package, and keeps:

- every function, method, type, constant and variable a kept declaration
  names (identifiers, qualified `pkg.X`, selectors, method values and
  expressions, generic declarations behind their instances), with the
  types of every expression and object in it;
- every method of a reached named type whose name and signature match a
  method of an interface type that kept code mentions (any signature for a
  generic type): a method that dynamic dispatch, an interface conversion, a
  type assertion or a constraint could call. This follows Go's method sets
  conservatively (a method can be kept that is never called through an
  interface, never the reverse), so Check's interface machinery finds
  every method a slot needs;
- a variable's whole specification, with its initializer, when any of its
  names is kept (`var a, b = f()` keeps both).

Everything else in a trimmed package is dropped **before** the subset rules
run: dropped declarations are absent from `TypedProgram.Files`,
`CheckedProgram.Functions`, `Globals` and the C. Kept declarations get
exactly the existing rules and messages: reached code with floats, `go`,
recursion or any other rejected form is rejected as before, at the same
position with the same text (without the hint).

**Package initialization.** Go runs every initializer of every imported
package. Under trimming:

- `func init()` functions are **never dropped**; an unsupported one is
  rejected as before.
- An initializer of a variable that kept code references always runs (in
  Go's order, on every invocation, [as usual](GO_GLOBALS.md)).
- An initializer of a variable no kept code references (including blank
  `var _ = f()` and `var _ I = (*T)(nil)` assertions) **is dropped and
  reported**: it does not run on chain, although Go would run it. Its
  effects (allocation, a panic, writes to other variables) do not happen.
  This is the one behavioural difference trimming accepts, and the reason
  it is opt-in.

### Imports of trimmed packages

An import of a trimmed package is compiled on chain only when a kept
declaration uses it (names the package):

- the standard-library overlay packages are always compiled and
  initialized when imported, as without trimming;
- an unsupported standard-library package (`math/big`, `fmt`,
  `encoding/json`, `database/sql`, ...) used only by dropped declarations is
  **type information only**: goSVM type-checks it from `GOROOT`'s sources
  (linux/amd64, no cgo, declarations without function bodies) and never
  compiles it. A kept declaration that uses it is rejected with the usual
  `unsupported import "math/big"; ...` at the import;
- a module package (go-ethereum, say) used only by dropped declarations is
  type information too: resolution lists it and its module dependencies
  (`go list -deps`, offline, linux/amd64 without cgo) in
  `Program.TypeOnly`, and Check type-checks them without function bodies,
  ignoring their errors. If a kept declaration uses one, it is compiled on
  chain after all and follows the ordinary rules: checked as a whole unless
  a pattern trims it too (Check repeats the load with it on chain).

A type-only import is neither compiled nor initialized (Go would initialize
it); the report lists it.

**Missing modules.** Type-checking a trimmed package needs every import's
declarations, so **every module a trimmed package imports must be in the
module cache** (`go mod download`), even when only dropped declarations use
it. Resolution is offline as always; a missing one fails with
`... trimmed package "P" imports "Q", which must be in the module cache for
type information even when only dropped declarations use it ...`. goSVM
does not guess types or skip unresolvable imports: an unchecked dropped
declaration could hide a type error that Go reports.

**Type errors.** A type error inside a dropped declaration is ignored: it can
only come from the overlay's subset of the standard library (a declaration
using `strconv.FormatFloat`, which the overlay leaves out), and the
declaration is never compiled. A type error in a kept declaration, or
outside every declaration (an import), is reported as without trimming
(through the importing package's "could not import" error).

### Reports

Every dropped declaration and initializer is reported:

- **Build warnings.** `gosvm build`, `gosvm test` and low-level builds print
  one line per trimmed package that dropped something, then where the full
  list is; the full list is written beside the program as
  `<output>.trim.json` (for projects, in the isolated output directory)
  and the warnings are printed again on cache hits:

  ```text
  gosvm: warning: trimmed github.com/holiman/uint256: dropped 21 declarations (6 initializers); type-only imports, not compiled or initialized: database/sql, database/sql/driver, encoding, encoding/json, fmt, math/big
  gosvm: warning: full list of dropped declarations: build/.../program.so.trim.json
  ```

  `<output>.trim.json` is `compiler.TrimReport`: `patterns`, then per
  trimmed package (sorted) its `path`, `dropped` (each `name`, `kind`
  `func`/`method`/`type`/`var`/`const`, `position` `file:line`, and
  `initializer: true` for a dropped var specification with an initializer)
  and `type_only_imports`.
- **`gosvm features`**: `uses.trimmed` (omitted without trimming), per
  trimmed package `dropped_declarations` (`"kind name"`),
  `dropped_initializers` and `type_only_imports`
  ([feature report](FEATURES_REPORT.md)).
- **`CheckedProgram.Trimmed`** (also `TypedProgram.Trimmed`) for backends
  ([backend interface](BACKEND_INTERFACE.md#trimming)).

**Caching.** Build receipts and dependency keys include the patterns, the
type-only module packages' file hashes and the `GOROOT` version whose
standard library supplies type information; a missing report forces a
rebuild. Programs without trimming keep their earlier keys.

Evidence: `TestTrim*` ([trim_test.go](../internal/compiler/trim_test.go)),
`TestTrimmedUses`, `TestTrimSetting`, and `examples/real-libraries`, which
compiles the four library modules unmodified under trimming with the same
results as native Go and as the historical trimmed copies, on the host and
on SBF for both targets
([results](../results/compiler/2026-10-16-trim-opt-in/README.md)).

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
