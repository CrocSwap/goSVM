# Milestone 1 acceptance procedure

Milestone 1 was closed October 5, 2026 for the
[bounded tested subset](SVM_RUNTIME_COVERAGE.md). The user explicitly skipped the
additional fresh-validator comparison and clean macOS arm64 host checks and
authorized milestone 2. Neither skipped check is represented as passing.
The [current-host rehearsal](../results/svm/2026-10-05-m1-acceptance-final/README.md)
retains its original acceptance status and limitations as historical evidence.
The procedure below remains available for later compatibility/installation work;
it is no longer a prerequisite to starting milestone 2.

## Installation and compiled-program check

Use a fresh macOS arm64 machine or VM. Record how it was created, its OS version,
and the prerequisites installed before this test. Install Go **1.22.0**, Apple
command-line tools and Python **3.9+**. Copy this checkout with its `results/svm`
fixture/reference files and the pinned local runner archive. Existing `build/`,
Solana caches, Cargo, Rust and a validator are not prerequisites for this check.
The archive is an experimental local artifact, not a public release.

From the checkout, run into a new evidence directory:

```sh
python3 scripts/milestone1_acceptance.py \
  --output results/svm/new-clean-mac-acceptance \
  --runner-archive results/svm/2026-10-05-runner-install-complete/gosvm-svm-runner-0.5.0-darwin-arm64.tar.gz \
  --backend-archive /absolute/path/to/platform-tools-osx-aarch64.tar.bz2 \
  --host-note 'Fresh VM provenance, macOS version, and installed prerequisites'
```

The backend archive must be the official platform-tools **v1.51** artifact:
448,012,397 bytes, SHA-256
`a1e32b3137fe8199fa76da81c43ec0122bdbd27acf1279f38d8d265f7a6e6cb3`.
The CLI verifies it before extraction. If that archive is unavailable, replace
`--backend-archive ...` with `--download-backend` to explicitly enable the pinned
download. No other step downloads tools or dependencies.

The script source-installs the CLI into isolated staging, uses empty Go/module
and goSVM caches, installs/verifies the backend and runner, scaffolds a standalone
module, and tests with only Go on PATH. It freshly compiles the bounded starter,
matched full-token adapter, original lifecycle token program and C syscall probe.
It checks exact ELF/dependency hashes and recorded case results, including state,
errors, CU and logs. The three-case project-general fixture is explicitly copied
from the example; it is not supplied by the default scaffold. Native tests are
forced with `-count=1`. The metadata-negative check uses validator expectations
and must reject the known `rent_epoch` difference while clearing its stale report.

Review `summary.json`, receipts and command logs. A successful script reports only
its installation/workflow checks as passed. Host provenance still needs review;
an empty cache cannot establish a clean machine. The script never independently
marks milestone 1 complete. `--existing-backend /path/to/managed/backend/root`
copies a receipt-bearing backend for a rehearsal and makes the run ineligible
for clean-host acceptance. Failed attempts retain `passed:false` evidence; choose
a new output name for every attempt.

## Final independent validator check

Run on a host that permits loopback RPC and validator networking, using validator
**3.0.15**, Go **1.22.0**, platform-tools **v1.51/SBFv3**, and the current source.
Unlike ordinary development, the existing differential verification scripts are
maintainer tools and need the cached host Rust dependencies. Prepare the three
matched token ELFs under `build/anchor` using the
[Anchor reproduction procedure](../benchmarks/anchor/README.md#reproduce).
Keep its benchmark output separate from this acceptance evidence. Do not alter
Basanos or replace historical results.

```sh
solana-test-validator --version
GOCACHE="$PWD/build/lifecycle-go-cache" python3 scripts/svm_fixtures.py \
  --output results/svm/new-final-token-validator
GOCACHE="$PWD/build/lifecycle-go-cache" python3 scripts/svm_lifecycle.py \
  --output results/svm/new-final-lifecycle-validator --samples 3
```

Supply `--llvm /absolute/path/to/v1.51/llvm` if necessary. Do **not** supply
`--validator-evidence` for the final lifecycle run: that option replays a captured
oracle. The token run must produce a new `fresh-validator.json` and log; the
lifecycle summary must say `fresh local validator`. Both must pass and retain
exact program/token hashes, active feature IDs and all original assertions.
The token corpus has 108 fast scenarios/110 transactions and 105 Go vector
CU/error comparisons; the matched independent harness also checks lean Rust and
Anchor. Lifecycle has six scenarios/26 transactions with ten expected failures.

Also verify the generated Go starter with a newly started validator:

```sh
GOCACHE="$PWD/build/lifecycle-go-cache" bash scripts/build-cli.sh build/m1-final-gosvm
build/m1-final-gosvm test --sbf -dir examples/typed-swap -- -count=1
```

Copy `examples/typed-swap/build/sbf-results.json` and the command log into the new
acceptance directory. Compare all 14 cases and ELF/CU/errors with the fast starter
report from the same source. The older matched Rust/Anchor bounded comparisons
remain historical evidence; this final gate tests the current Go application.

## Compatibility review and completion

Retain these explicit limits when reviewing the fresh results:

| Observation | Required interpretation |
| --- | --- |
| LiteSVM uses Agave runtime 3.0.10; validator is 3.0.15 | Exact observed feature IDs and corpus agreement support the tested subset, not universal runtime or mainnet parity |
| New accounts use `rent_epoch=0` in LiteSVM and `uint64` maximum in the validator | Keep separate full-metadata assertions; never remove or silently normalize the field |
| Patched LiteSVM preserves an instruction error before post-execution rent checks | Recheck failed initialization and successful underfunded creation; both must retain the independent validator outcomes |
| Closed account is a zero-lamport System account in simulation and absent after commit | Preserve phase-specific close assertions and rollback checks |
| Clock/Rent controls are explicit host actions | Independent syscall/account-byte expectations cover these controls; they do not imply bank time, consensus or implicit blockhash/history advancement |

Review source/package pins and any new differences when performing later checks.
Record new evidence paths in `HANDOFF.md` and `ROADMAP.md`; preserve the original
milestone 1 closure and experiment reports. Public signing/distribution,
licensing review, other hosts and broader runtime features remain later work.
