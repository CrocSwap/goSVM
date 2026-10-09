# Go + Bend + Rust in one program

One schema-1 Go program whose handler calls a pure Bend library
(`mixed.evaluate`, the ABI v1 fixture) and a pure Rust library (the
`internal/library/testdata/rust/quote` crate), then writes state. Go owns the
account, validation and errors; Bend admits a request value; Rust quotes an
amount against the admitted value with exact u128 arithmetic.

`fixtures.py` models both libraries independently and writes `sbf.json`: success,
the Bend 40/41 boundary, Bend refusal and malformed input (104), Rust malformed
input (304), the u128 boundary, zero amount, and a successful instruction followed
by a malformed one that must roll the whole transaction back.

```sh
gosvm new -module example.com/trio trio && cd trio
gosvm library add -name bend <fogo-v0 manifest> <solana-v3 manifest>
cp -R <goSVM>/internal/library/testdata/rust/quote rust/
gosvm library add -name quote -function Quote -rust rust/quote
cp <goSVM>/tests/go-bend-rust/program.go.txt program.go
cp <goSVM>/tests/go-bend-rust/program_test.go.txt program_test.go
cp <goSVM>/tests/go-bend-rust/sbf.json testdata/sbf.json
gosvm test --svm      # native Go/Bend/Rust tests, then both targets' linked images
```

October 9 local development run: 9/9 cases on fogo-v0 (runner 0.5.0) and on
solana-v3 (runner 0.6.0); images 11,856 and 11,088 bytes. The solana-v3 Bend
object was Bend's in-progress v1.57 test output, not a published bundle, so
this is development evidence; qualification awaits Bend's published v3 bundle
and a fresh results directory.
