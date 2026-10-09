# Go+Bend and Go+Bend+Rust on fogo-v0 and solana-v3 — October 9, 2026

**Local qualification passes on both targets with Bend's published bundles.**

| Fixture | Target | Cases × runs | ELF bytes | Runner |
| --- | --- | ---: | ---: | --- |
| Go+Bend (`tests/bend-library`) | fogo-v0 | 9 × 3 | 4,664 | 0.5.0 (LiteSVM 0.8.2) |
| Go+Bend | solana-v3 | 9 × 3 | 3,896 | 0.6.0 (LiteSVM 0.13.0) |
| Go+Bend+Rust (`tests/go-bend-rust`) | fogo-v0 | 9 × 3 | 11,856 | 0.5.0 |
| Go+Bend+Rust | solana-v3 | 9 × 3 | 11,088 | 0.6.0 |

Each case checks simulation and submission, exact errors and account bytes,
including malformed library input and a successful instruction followed by a
failing one that rolls back the transaction. ELF hashes and per-case CU are
identical across repetitions. Native cgo tests of the same Go/Bend/Rust code run
first in every repetition.

Producer pins (unchanged copies are not retained here; see `metrics.json`):

- Bend fogo-v0 / v1.53: SHA-256 `ec18012f5bb98cd19813b721f2ec343d628c679052828a3eb122ca1b335bc6de` (accepted October 7)
- Bend solana-v3 / v1.57: SHA-256 `9db669cefeee15dfad19cb849671256303a40243129ab105f85beb81dd3c37ee`
  (published by Bend October 9; same symbol and schema fingerprint as v0)
- Rust `quote` crate built by GoSVM's pinned v1.57 Rust producer for both targets

Libraries were bound only through `gosvm library add` (`gosvm.json` → `libs/`),
with no runner or tool flags. The solana-v3 image links with `--no-undefined`, and
GoSVM's final-image check refuses any import on v3, matching Bend's stated
requirements (objects import only memcpy/memmove/memset; GoSVM supplies one provider).

## Reproduce

```sh
bash scripts/build-cli.sh build/gosvm
python3 scripts/verify_go_bend_rust.py --gosvm build/gosvm \
  --output results/compiler/<new-dir> \
  --bend <fogo-v0 library.manifest.json> <solana-v3 library.manifest.json>
```

Requires the managed v1.57 backend, the Rust producer toolchain
(`gosvm toolchain install -rust`) and both managed runners. Logs are local-only.
These are local runtime results, not Fogo/Solana network validation; runtime
engines and feature sets differ between targets, so CU is not a matched comparison.
