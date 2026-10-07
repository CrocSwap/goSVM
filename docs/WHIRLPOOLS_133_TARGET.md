# Whirlpools 1.33× CU target

October 6, 2026. A new owner-side application snapshot measures **1.280853×
Rust**, meeting the 1.33× optimistic target and the 1.5× intermediate checkpoint.
This is the median of paired successful-case CU ratios for the matched
**classic-token, fixed-fee handler with events omitted**, at default O2. It is
not a measurement of the full Whirlpools contract or of the starter/generated
constant-product swap.

The [final proof](../results/compiler/2026-10-06-whirlpool-optimized-final/summary.json)
passes the complete handler and expanded arithmetic suites. The
[delivery snapshot](../results/compiler/2026-10-06-whirlpool-optimized-snapshot/optimized-snapshot.tar.gz)
also passes archive hash/mode checks and an extracted-source rebuild that
reproduces the exact measured ELF.

## Result and scope

The fresh CLI rebuild reproduces the tested ELF. All **166 handler scenarios ×
three repetitions × Go, scoped Rust and unchanged full upstream Rust** pass.
Expected account bytes, metadata, errors, authority/oracle checks, failed CPIs
and rollback are retained. Both Rust artifacts and their per-case CU/log results
reproduce the original references exactly. Actual Token consumed CU and all
non-budget Go log content match the preceding owner checkpoint.

| Measurement | Packed-store checkpoint | Optimized snapshot |
| --- | ---: | ---: |
| Median paired successful-case CU ratio | 1.702520× | **1.280853×** |
| Go ELF | 84,208 bytes | **83,424 bytes** |
| Largest static frame | 1,472 bytes | **1,472 bytes** |

All 103 successful cases improve, saving a median **14,416 CU / 21.91%**.
Savings range from 4,054 to 42,193 CU. Of 63 failures, 29 improve and 34 remain
unchanged; none regress. Individual successful Go/Rust ratios range from
**1.226617× to 1.617088×**: the target is met for the specified median, not every
path. Median paired ratios and median per-case percentage savings are different
statistics; do not derive one by dividing the other two medians.

The ratio uses the event-omitting scoped Rust reference; the full upstream Rust
replay supplies additional correctness evidence. Runtime features, runner
0.5.0, compute budget, fixture inputs, LLVM/platform-tools v1.51, SBF v3 and O2
are unchanged. The canonical compiler and SDK are the previously verified
[packed-store frontend](PACKED_STORE_OPTIMIZATION.md). No new compiler feature,
SDK API, unsafe cast or relaxed account check was required.

## Application changes

### Bounded multiplication/division

`MulDiv` already rejects a product that does not fit U128. Its old successful
path nevertheless widened that product and divisor and used bit-by-bit U256
division. The new path calls the existing checked `DivRound128` helper after
the same zero-divisor and multiplication-overflow checks. If both operands
have zero high limbs, the existing exact `Mul64` helper computes the product
directly; that product always fits U128. Other products retain `Mul128` and
its high-limb overflow rejection. Both rounding modes and error precedence
are preserved, including reward calculations' existing overflow-to-zero policy.

The bounded-division-only control reduces the complete-handler ratio from
1.702520× to **1.408072×**. Adding the small-operand multiplication path reduces
it to **1.394918×**. All 103 successes improve and no failures regress in either
control. [Arithmetic controls](../results/compiler/2026-10-06-whirlpool-math-controls/summary.json)
and [loop controls](../results/compiler/2026-10-06-whirlpool-loop-controls/summary.json)
preserve the baseline and each separate artifact/result.

### Private core after validation

The old successful handler calls `CheckTickArray` six times: once for each of
three accounts in handler validation, then again inside independently callable
`swap.Apply`. The new snapshot places the handler and swap implementation in
the same package and uses a **private** `applyValidated` core. The thin handler
entry calls the checked `swap.ProcessHandler` entry. Its original owner, length,
discriminator, pool-key and all 88 flag checks still run for each account, in
their original order. Its original ordering and oracle checks also remain.

Only the public checked `Apply` wrapper and the fully validating handler call
the private core. `Apply` retains its pool checks and all three tick-array
checks, then invokes that core. There is no public unchecked entry, reusable
validation certificate, or cached validation across arbitrary calls.

The handler validates exactly the buffers/key subsequently supplied to the
core. Between validation and that call, the audited code only reads state and
computes ordering/arguments: it performs **no writes, resizing, or CPIs**.
Ordered array indices refer to the already validated accounts from this same
invocation. This is a local application invariant, not a general compiler
assumption about aliases. Future changes that add mutation/CPI in that interval
must revisit it.

The package-move-only control reproduces its preceding ELF and every CU exactly,
so the architectural move alone is not counted as an optimization.
[Validation controls](../results/compiler/2026-10-06-whirlpool-validation-controls/summary.json)
measure the private-core change separately. The
[minimal candidate](../results/compiler/2026-10-06-whirlpool-validation-minimal/summary.json)
keeps the original full tick decoder/scanner and measures the accepted 1.280853×.
Native tests additionally validate arrays, mutate each one's last flag, pool
key, discriminator or length, then call public `Apply`; all 12 cases retain
the original rejection. Public off-chain callers still use that checked entry.

## Alternatives not adopted

Changing `DeltaA` to the normalized wide divider helped in isolation but gave
a slightly worse median when combined with bounded `MulDiv` (1.411664× versus
1.408072×). It is absent from the accepted snapshot. Reading only the tick's
initialized flag had little benefit; the accepted snapshot retains the original
scanner and wire API. The earlier packed-read and comparison-parameter compiler
controls were also not adopted. Preserved results record regressions as well
as savings.

## Reproduction and adoption

Use new result directories. The experiments copy the frozen modules and full
matching SDK into goSVM-owned staging and leave the sibling benchmark's source,
defaults, pins and historical outputs untouched:

```sh
python3 scripts/whirlpool_math_experiment.py --output results/compiler/my-math-controls
python3 scripts/whirlpool_math_experiment.py --family loop --output results/compiler/my-loop-controls
python3 scripts/whirlpool_math_experiment.py --family validation --output results/compiler/my-validation-controls
python3 scripts/whirlpool_optimized_verify.py --output results/compiler/my-optimized-proof
python3 scripts/whirlpool_snapshot.py --proof results/compiler/my-optimized-proof --output results/compiler/my-optimized-snapshot
```

The final verifier uses the recorded minimal candidate as input, rebuilds with
the frozen packed-store CLI, checks static frames, reruns both Rust references,
and compares the complete handler against its original fixtures. It also runs
the preserved 24,713-case component corpus plus 5,000 new MulDiv boundary/random
cases against Go and the unchanged Rust component ELF. New cases use independent
Python integer expectations. Native tests repeat three times and include the
19,381 pinned Rust math vectors, 104 swap-manager cases, existing bounded/wide
division tests and 21,000 MulDiv triples checked against both the old algorithm
and `math/big`, in both rounding modes.

The delivery `optimized-snapshot.tar.gz` contains both source modules, matching
SDK copies, exact executable CLI, tested handler ELF, complete frontend source
archive and a manifest hashing files and recording modes. Its
[packaging proof](../results/compiler/2026-10-06-whirlpool-optimized-snapshot/summary.json)
checks the extracted build. Use this as a **separate application/SDK/frontend
snapshot**. The original frontend source archive is also preserved
[separately](../results/compiler/2026-10-06-packed-store-canonical-final/frontend.tar.gz).
Record the new application identity; adopting only the old frontend pin cannot
apply the application arithmetic and private-core changes. Rerun the sibling
benchmark's own verification before updating its defaults.

These are experimental local macOS arm64/SBFv3 artifacts. Build/edit timings,
other platforms, events, adaptive fees and dynamic tick arrays are not newly
measured here. Independent developer acceptance remains the sole outstanding
milestone-2 closure gate.
