# Pure Bend libraries from Go

GoSVM consumes Bend's **ordinary pure-library ABI v1** artifact contract. The
producer chooses a checked pure root and publishes canonical byte widths, a
schema fingerprint, a fingerprinted C symbol, status codes, native/SBF objects
and hashes. Go keeps account validation, syscalls and CPI in its application.
This does not establish formal verification of the Go shell, GoSVM compiler,
linker or transaction runtime.

The coordinated producer contract is `../bendSVM/docs/library-artifact-contract.md`;
its ABI fixture is `../bendSVM/tests/mixed-library/fixture.md`. Current GoSVM
support accepts **fogo-v0** producers at Bend platform-tools v1.53 (qualified)
or v1.57 (accepted ahead of publication, not yet qualified) and **solana-v3** at
v1.57.
The Go caller uses the separately pinned v1.57 backend. Their actual mixed image
has [local acceptance evidence](../results/compiler/2026-10-07-target-profiles-final/README.md).
Bend's published `solana-v3` / v1.57 bundle (SHA-256 `9db669ce…c37ee`) is also
[qualified locally](../results/compiler/2026-10-09-go-bend-rust-targets/README.md),
alone and together with a Rust library, on both targets. Identical interface
fingerprints do not authorize relabelling objects or arbitrary compiler mixtures.
Qualified SHA replacement libraries have a different selection/authorization
contract and are outside this consumer.

## Add a library to a project

```sh
gosvm library add -name bend \
  ../bendSVM/build/interop/fogo-v0/20261007T134003Z-0bdd2144/library.manifest.json \
  path/to/solana-v3/library.manifest.json
```

Pass one producer manifest per target. GoSVM reads each manifest's target and
SHA-256, checks its contract, records the pins under `libraries` in `gosvm.json`
and generates `libs/bend`, a Go package with each target's producer manifest and
objects under `fogo-v0/` and `solana-v3/`:

```json
"libraries": {
  "bend": {
    "function": "Evaluate",
    "bend": {
      "fogo": {"manifest": "../bendSVM/.../library.manifest.json", "sha256": "ec18012f…"}
    }
  }
}
```

`generate`, `build` and `test` regenerate `libs/<name>` when its pins change and
otherwise revalidate it. A project target without a matching producer fails that
target's build with the missing network named. Accepted producer pairings are
fixed in the consumer: fogo-v0 from Bend's v1.53 producer (qualified with the
v1.57 caller) or v1.57, and solana-v3 from v1.57, the caller's own backend.
`library add` notes when a pairing has not yet been qualified; run
`gosvm test -svm` before deploying such a build.

The lower-level `gosvm library bind -manifest … -sha256 … -allow-library-tools v1.53
-package bend -function Evaluate -output dir` still binds a single producer into a
new directory.

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
gosvm test          # Ordinary native Go tests use the selected header + host object.
gosvm build         # Each target links its own selected SBF object.
gosvm test --svm    # Runs the linked image for every target.
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
