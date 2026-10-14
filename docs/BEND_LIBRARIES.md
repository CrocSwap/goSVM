# Pure Bend libraries from Go

GoSVM consumes Bend's **ordinary pure-library ABI v1** artifact contract. The
producer chooses a checked pure root and publishes canonical byte widths, a
schema fingerprint, a fingerprinted C symbol, status codes, native/SBF objects
and hashes. Go keeps account validation, syscalls and CPI in its application.
This does not establish formal verification of the Go shell, GoSVM compiler,
linker or transaction runtime.

The coordinated producer contract is `../bendSVM/docs/library-artifact-contract.md`;
its ABI fixture is `../bendSVM/tests/mixed-library/fixture.md`. Current GoSVM
support accepts **fogo-v0** producers at Bend platform-tools v1.57 (the default)
or v1.53, and **solana-v3** at v1.57; all three pairings are qualified.
The Go caller uses the separately pinned v1.57 backend. The v1.53 mixed image
has [local acceptance evidence](../results/compiler/2026-10-07-target-profiles-final/README.md).
Bend's published `solana-v3` / v1.57 bundle (SHA-256 `9db669ce…c37ee`) is
[qualified locally](../results/compiler/2026-10-09-go-bend-rust-targets/README.md),
alone and together with a Rust library, on both targets, with the fogo-v0 / v1.53
bundle (`ec18012f…c6de`). Bend's published `fogo-v0` / v1.57 bundle (SHA-256
`d8493350…66ecd`) is [qualified the same way](../results/compiler/2026-10-14-bend-fogo-v157/README.md);
its linked images and per-case CU are identical to the v1.53 ones. Identical interface
fingerprints do not authorize relabelling objects or arbitrary compiler mixtures.
Qualified SHA replacement libraries have a different selection/authorization
contract and are outside this consumer.

## Add a library to a project

```sh
gosvm library add -name bend \
  ../bendSVM/build/interop/fogo-v0/20261009T191953Z-0295b5f8/fogo-v0/v1.57/library.manifest.json \
  ../bendSVM/build/interop/solana-v3/20261009T185443Z-f45b18ca/solana-v3/v1.57/library.manifest.json
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
      "fogo": {"manifest": "../bendSVM/.../library.manifest.json", "sha256": "d8493350…"}
    }
  }
}
```

`generate`, `build` and `test` regenerate `libs/<name>` when its pins change and
otherwise revalidate it. A project target without a matching producer fails that
target's build with the missing network named. Accepted producer pairings are
fixed in the consumer: fogo-v0 from Bend's v1.57 producer (default) or its
earlier v1.53 producer, both qualified with the v1.57 caller, and solana-v3 from
v1.57, the caller's own backend. Should a pairing ever be accepted ahead of its
qualification, `library add` notes it; run `gosvm test -svm` before deploying
such a build.

## Build from Bend source

Instead of pinning published manifests, a project can declare a Bend source and
root; GoSVM then runs the Bend checkout's own `build:library` for every project
target, as it builds [Rust crates](RUST_LIBRARIES.md):

```sh
gosvm library add -name bend -bend-source ../bendSVM/tests/mixed-library/PROOF.bend \
  -root mixed.evaluate -bend-checkout ../bendSVM      # or set GOSVM_BEND
```

```json
"libraries": {
  "bend": {
    "function": "Evaluate",
    "bend_source": {"checkout": "../bendSVM", "file": "../bendSVM/tests/mixed-library/PROOF.bend", "root": "mixed.evaluate"}
  }
}
```

`bend_source` is exclusive with `bend` pins and `rust`. Paths in or beside the
project are recorded relative to it, others absolutely. Without `checkout`,
`$GOSVM_BEND` names the bendSVM checkout. Per target, GoSVM runs

```sh
npm --prefix <checkout> run -s build:library -- <PROOF.bend> <root> <project>/build/bend-libraries/<name>/<key16>/library --target <profile> --json
```

with `SBF_V157_TOOLS` set to GoSVM's platform-tools v1.57 `llvm/bin` (the full
tree from `gosvm toolchain install -rust`, else `~/.cache/solana/v1.57/platform-tools`;
Clang/LLD are checked against GoSVM's content pins and Bend checks Clang and
`llvm-readelf` against its own). It parses the single `--json` result, requires
the requested profile and root, an accepted platform-tools pin and the manifest
at `<key16>/<profile>/<tools>/library.manifest.json`, and verifies the reported
`manifestSha256` against the file. The resulting manifests are then bound
exactly like pinned ones, so every check below applies.

The build key covers the source and its transitive `import … as …` files, the
root, the target list, the checkout's git HEAD and a hash of its uncommitted
changes (a dirty checkout is built with a warning), the pinned Bend dependency
and platform-tools v1.57. `generate`, `build` and `test` reuse a completed build
while the key is unchanged and drop superseded builds of that library.

Before building, GoSVM checks the prerequisites and stops with the fix: a
bendSVM checkout (`package.json` with `build:library`), `npm` on `PATH`,
installed dependencies (`node_modules/.bin/bun`; run `npm --prefix <checkout> ci`),
a clean `.deps/bend` at `7561656155a4285c1e4ccfcb3505ab59524de973` (GoSVM never
lets Bend fetch it), a host `clang` (or `$CC`) and the v1.57 tools. GoSVM writes
nothing in the checkout; it compares `git status --porcelain` (and the
uncommitted-content hash) before and after each build and discards the build if
they differ.

Rebuilt bundles are evidence only for their own inputs; Bend reruns no
qualification for consumer builds. On October 14 the mixed fixture built this way
reproduced the published bundles' objects, C source, header and schema byte for
byte on both targets (only the `.su` stack reports, and so the manifest SHA-256,
differ), linked to the same Go+Bend images and passed `gosvm test --svm` on
fogo-v0 and solana-v3. The opt-in integration test is
`GOSVM_BEND=/abs/bendSVM make test` (`TestBendSourceIntegration`).

## Low-level binding

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
