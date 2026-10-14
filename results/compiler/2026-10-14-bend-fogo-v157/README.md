# Bend fogo-v0 / v1.57 producer qualification — October 14, 2026

**Passes.** Bend's published fogo-v0 / v1.57 bundle, bound together with its
solana-v3 / v1.57 bundle, passes Go+Bend and Go+Bend+Rust local qualification on
both targets (9 cases × 3 runs per fixture per target). ELF hashes and per-case
CU are identical across repetitions.

| Fixture | Target | Cases × runs | ELF bytes | ELF SHA-256 (prefix) | Runner |
| --- | --- | ---: | ---: | --- | --- |
| Go+Bend (`tests/bend-library`) | fogo-v0 | 9 × 3 | 4,664 | `b2219976…` | 0.5.0 (LiteSVM 0.8.2) |
| Go+Bend | solana-v3 | 9 × 3 | 3,896 | `760279ec…` | 0.6.0 (LiteSVM 0.13.0) |
| Go+Bend+Rust (`tests/go-bend-rust`) | fogo-v0 | 9 × 3 | 11,856 | `3139045a…` | 0.5.0 |
| Go+Bend+Rust | solana-v3 | 9 × 3 | 11,088 | `749651ba…` | 0.6.0 |

## Comparison with the v1.53 producer

Against [the October 9 qualification](../2026-10-09-go-bend-rust-targets/README.md)
(fogo-v0 from Bend's v1.53 bundle `ec18012f…c6de`, same solana-v3 bundle), every
case on every fixture/target has the **same CU and error**, and the four linked
ELFs are **byte-identical** (same SHA-256). For example, fogo-v0 Go+Bend
`zero` 309 CU, `bad-bool` 249 CU / error 104, rollback 558 CU; Go+Bend+Rust
`accepted` 605 CU, `u128-boundary` 768 CU. Full per-case values are in
`metrics.json`.

The v1.57 producer's `library.sbf.o` (`c6ecd922…`) and `library.sbf.memory.o`
differ in bytes from the v1.53 ones (`b00f221a…`), but `llvm-objdump -d` of the
two `library.sbf.o` files is identical apart from the file name: the instructions
match, and the final link (GoSVM supplies its own memory helpers and omits the
producer's memory object) produces the same image. The header, C source, schema
and host object hashes are unchanged.

## Pins

- Bend fogo-v0 / v1.57: `../bendSVM/build/interop/fogo-v0/20261009T191953Z-0295b5f8/fogo-v0/v1.57/library.manifest.json`,
  SHA-256 `d84933502b7f2122c3c4339b3ace8e5fd5ae81b323c2449bff9b9f7f1ba66ecd` (verified)
- Bend solana-v3 / v1.57: `../bendSVM/build/interop/solana-v3/20261009T185443Z-f45b18ca/solana-v3/v1.57/library.manifest.json`,
  SHA-256 `9db669cefeee15dfad19cb849671256303a40243129ab105f85beb81dd3c37ee` (verified)
- Rust `quote` crate built by GoSVM's v1.57 Rust producer (unmanaged
  `~/.cache/solana/v1.57/platform-tools`, same content pins)
- GoSVM CLI built from `1bb0123` with `scripts/build-cli.sh` (SHA-256 in `metrics.json`)
- Backend: platform-tools v1.57 Clang/LLD imported into a scratch `GOSVM_CACHE`
  with `gosvm toolchain install -llvm ~/.cache/solana/v1.57/platform-tools/llvm`
  (`qualified-local-backend`, same content pins as the official archive)
- Runners: 0.5.0 from `results/svm/2026-10-05-runner-install-complete/` and 0.6.0
  from `results/svm/2026-10-09-runner-0.6.0-package/`, installed into the same
  scratch cache (binary SHA-256 `886ea293…` and `550bac10…`)

## Reproduce

```sh
export GOSVM_CACHE=<scratch dir>
bash scripts/build-cli.sh build/gosvm
build/gosvm toolchain install -llvm ~/.cache/solana/v1.57/platform-tools/llvm
build/gosvm runner install -archive results/svm/2026-10-05-runner-install-complete/gosvm-svm-runner-0.5.0-darwin-arm64.tar.gz
build/gosvm runner install -archive results/svm/2026-10-09-runner-0.6.0-package/gosvm-svm-runner-0.6.0-darwin-arm64.tar.gz
python3 scripts/verify_go_bend_rust.py --gosvm build/gosvm \
  --output results/compiler/<new-dir> \
  --bend ../bendSVM/build/interop/fogo-v0/20261009T191953Z-0295b5f8/fogo-v0/v1.57/library.manifest.json \
         ../bendSVM/build/interop/solana-v3/20261009T185443Z-f45b18ca/solana-v3/v1.57/library.manifest.json
```

Logs are local-only. These are local runtime results, not Fogo/Solana network
validation; runtime engines and feature sets differ between targets, so CU is
not a matched comparison across targets.
