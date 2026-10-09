# Go/Rust consumer fixture

`program.go.txt` and `program_test.go.txt` are source templates for a fresh
`gosvm new -target fogo-v0 -module example.com/rust-mixed` project. The Go shell
validates the account and calls the pure Rust root
`internal/library/testdata/rust/quote` (`quote::quote`, 16 → 8 bytes) through a
generated `quote` package with `Quote(input, output []byte) (uint32, uint64)`.

Native tests cover business results, u128 boundaries, malformed input and ABI
boundaries (short/null buffers, overlap). `sbf.json` runs six single-instruction
cases plus a successful instruction followed by malformed input in one
transaction, checking exact errors and rollback against the linked image.

Producing the library uses `gosvm library build-rust`; binding it needs consumer
support for `gosvm-rust-library-v1` (see [Rust libraries](../../docs/RUST_LIBRARIES.md)).
