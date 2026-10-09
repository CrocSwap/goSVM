# Pure Bend libraries from Go

GoSVM consumes Bend's **ordinary pure-library ABI v1** artifact contract. The
producer chooses a checked pure root and publishes canonical byte widths, a
schema fingerprint, a fingerprinted C symbol, status codes, native/SBF objects
and hashes. Go keeps account validation, syscalls and CPI in its application.
This does not establish formal verification of the Go shell, GoSVM compiler,
linker or transaction runtime.

The coordinated producer contract is `../bendSVM/docs/library-artifact-contract.md`;
its ABI fixture is `../bendSVM/tests/mixed-library/fixture.md`. Current GoSVM
support accepts the explicit **fogo-v0 / Bend platform-tools v1.53** producer.
The Go caller uses the separately pinned v1.57 backend. Their actual mixed image
has [local acceptance evidence](../results/compiler/2026-10-07-target-profiles-final/README.md).
Bend `solana-v3` objects are unavailable and are refused. Identical interface
fingerprints do not authorize relabelling objects or arbitrary compiler mixtures.
Qualified SHA replacement libraries have a different selection/authorization
contract and are outside this consumer.

## Generate a package

```sh
gosvm library bind \
  -manifest ../bendSVM/build/interop/fogo-v0/20261007T134003Z-0bdd2144/library.manifest.json \
  -sha256 ec18012f5bb98cd19813b721f2ec343d628c679052828a3eb122ca1b335bc6de \
  -allow-library-tools v1.53 \
  -package bend -function Evaluate -output my-program/bend
```

The output must be a new directory. The binder copies selected artifacts and the
original manifest, generates `bindings.go` and `gosvm.library.json`, and publishes
the package atomically. It never rebuilds or rewrites Bend artifacts. Host objects
are architecture/format-specific; regenerate with a compatible producer on each
host. Native tests require cgo and the host compiler/linker.

Import the generated package normally in your application's module:

```go
var input [6]byte // Canonical mixed.evaluate fixture argument bytes.
var output [5]byte
input[1] = 1
input[2] = 40
status, length := bend.Evaluate(input[:], output[:])
```

This is a synchronous `(input, output []byte) (uint32, uint64)` call. Encode/decode
canonical bytes explicitly; native Go/C struct layouts do not determine wire
layouts. `InputBytes`, `OutputBytes` and `ABI_*` status constants are generated.
The wrapper owns aligned output-length storage, borrows only byte buffers and
retains no pointers. Overlap, nil/short buffers and malformed canonical input
remain ABI errors. Native and SBF calls use the same symbol/status/byte interface.
No arbitrary cgo packages or extern declarations become on-chain language features.

```sh
cd my-program
gosvm test                      # Ordinary native Go tests use selected header + host object.
gosvm build -target fogo-v0      # On-chain call links the selected SBF object.
gosvm test -target fogo-v0 --svm -svm-runner /path/to/qualified-v0-runner
```

## What is checked

The consumer checks the pinned producer manifest, kind/version/ABI, explicit
profile and allowed producer tools, root/symbol/fingerprint/byte widths, selected
schema and C declaration/status constants, artifact basenames/sizes/hashes,
native object architecture/type and SBF object version/machine/export. It
revalidates the producer contract and canonical generated adapter on every
compiler load, rejecting altered/rehashed native adapters, extra native sources,
interface drift, object corruption and target relabelling.

The compiler resolves the generated Go interface through ordinary type/borrow
checking, lowers its restricted wrapper, then links captured verified object bytes
from private temporary storage. Internal library calls bind within the image.
GoSVM supplies exactly one set of memory helpers; the producer's optional SBF
memory object is pinned/validated but omitted from this link. Remaining imports
must match the compiler's syscall allowlist. Dependency changes get separate
output/cache namespaces. `finalElfAuthorized:false` remains the producer's flag:
a library manifest is not final-image or network authorization.

The opt-in producer-contract integration test is:

```sh
BEND_LIBRARY_MANIFEST=/absolute/path/library.manifest.json \
BEND_LIBRARY_SHA256=ec18012f5bb98cd19813b721f2ec343d628c679052828a3eb122ca1b335bc6de \
make test
```

[Consumer fixtures](../tests/bend-library/README.md) cover native success and ABI
boundary errors, actual linked-image success/malformed inputs and transaction
rollback. Native tests alone cannot establish SBF rollback. No Fogo/Solana public
network acceptance is claimed; a new producer pin/root or compiler mixture
requires fresh final-image qualification.
