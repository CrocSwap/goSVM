# Pure Rust libraries (producer)

GoSVM can build a pure `#![no_std]` Rust crate into the same pure-library
**ABI v1** shape that [Bend libraries](BEND_LIBRARIES.md) use: one fingerprinted
C symbol over canonical fixed-width bytes, status codes 0–7, one relocatable SBF
object per target profile, a native host object, a C header, a schema and one
producer manifest per profile. Go keeps accounts, syscalls and CPI; the Rust
library only computes.

This is the producer half. Linking into a Go program requires the consumer to
accept `kind: "gosvm-rust-library-v1"`; see *Consumer contract* below.

## Authoring

Write an ordinary safe function over fixed byte arrays and declare it in
`Cargo.toml`. No unsafe code, no ABI glue, no macros:

```toml
[package.metadata.gosvm-library]
function = "quote"          # path within the crate, e.g. "pricing::quote"
input-bytes = 16
output-bytes = 8
schema = "Quote{amount:u64le,reserve_in:u32le,reserve_out:u32le} -> u64le"  # optional
```

```rust
#![no_std]
pub struct Malformed;
pub fn quote(input: &[u8; 16]) -> Result<[u8; 8], Malformed> { /* ... */ }
```

The function takes `&[u8; IN]` and returns `[u8; OUT]` or
`Result<[u8; OUT], E>` for any `E`. `Err` means malformed canonical input
(status 4). Width mismatches are rustc type errors. GoSVM generates the wrapper
that checks, in Bend's order: null pointers (1), pointer-range wrap (2),
`output_len` alignment (6), overlap (3), exact input length and `Err` (4), output
capacity (5). It copies input to a private buffer and writes output and
`output_len` only on success. Status 7 is never produced.

The fingerprint is SHA-256 over the ABI kind/version, root
(`<crate>::<function>`), widths and the optional `schema` text. The symbol is
`gosvm_rust_v1_call_<fingerprint>`. Changing any of these changes the symbol.

## Build

```sh
gosvm library build-rust -crate path/to/crate -output path/to/new-dir \
  [-targets fogo-v0,solana-v3] [-platform-tools ~/.cache/solana/v1.57/platform-tools]
```

The output directory must not exist. It contains `fogo-v0/` and `solana-v3/`, each
with `library.manifest.json` (written last), `library.sbf.o`, `library.host.o`,
`library.h` and `library.schema.json`.

The build uses the platform-tools **v1.57** `cargo`/`rustc` (1.95.0-dev, LLVM
22.1.2), content-pinned on macOS arm64 together with the Go compiler's pinned
`clang`/`ld.lld`. Cargo runs offline with an isolated `CARGO_HOME`, so only path
dependencies resolve. GoSVM generates a wrapper crate (`staticlib`,
`opt-level=3`, `codegen-units=1`, `lto="fat"`, `panic="abort"`,
`overflow-checks=false`) and emits one object with `--emit=obj`:
`sbpf-solana-solana` for fogo-v0 (machine 263, flags 0) and
`sbpfv3-solana-solana` for solana-v3 (machine 247, flags 3). The host object
uses the same pinned rustc with its host sysroot. Builds are reproducible
across output locations.

## What is checked

- The crate is `#![no_std]` and declares the function/widths.
- **No reachable panics**: the SBF panic handler references an undefined
  marker; if optimization does not remove every panic path (unwrap, unchecked
  indexing, division by a possibly-zero value, ...), the build fails. Return
  `Err` instead. The host object's handler calls `abort`.
- Compiler libcalls such as `__udivti3` are resolved from the pinned sysroot's
  `compiler_builtins` members, merged with `ld.lld -r` and made file-local. Its
  memory functions (which call `sol_mem*` syscalls) are never linked.
- The published object exports exactly one global (hidden) function, the ABI
  symbol, and imports only `memcpy`, `memset`, `memmove` and `__multi3`, each
  provided once by the GoSVM image. Any other import (syscalls, `abort`,
  `memcmp`/`bcmp`, allocation) is refused.
- No nonempty writable allocated sections (mutable statics, atomics, pointer
  tables), matching the v3 link script that discards `.data`/`.bss`.
- ELF type/machine/flags per profile; host object Mach-O type and export.

The manifest records selected artifacts with sizes and hashes, source input
hashes, tool hashes, the compiler_builtins archive/members, the remaining imports
and `finalElfAuthorized: false`.

## Consumer contract

To bind these artifacts, the consumer must accept, per target manifest:
`kind "gosvm-rust-library-v1"`, `version 1`, `abiVersion 1`, symbol prefix
`gosvm_rust_v1_call_`, `toolchains.rust.platformTools "v1.57"` with
`cpu`/`sbpfVersion` `generic`/0 (fogo-v0) or `v3`/3 (solana-v3), and no memory
or support object. The header defines the same `BEND_LIBRARY_*` macros as
Bend's (plus `GOSVM_LIBRARY_*` aliases), and the schema has Bend's top-level
fields (`version`, `abiVersion`, `root`, `symbol`, `schemaFingerprint`,
`inputBytes`, `outputBytes`). The host object, header and schema are identical
across the per-target manifests.

## Limits

- macOS arm64 with platform-tools v1.57 only; no public-network validation.
- Path dependencies only; no `std`, allocation, syscalls, logging or state.
- Wrapping release arithmetic; use `checked_*` where overflow matters.
- Widths are 1–1024 bytes and canonical encoding is the author's
  responsibility. The optional `schema` text is identity, not a checked codec.
- A linked image proves only the fixture that was run. Re-qualify each new root,
  toolchain or target.
