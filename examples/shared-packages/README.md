# Shared packages on chain and in ordinary Go

This bounded swap example defines `Pool`, `SwapArgs`, `Amount` and `[32]byte`
`PublicKey` once in `model`. Its on-chain handler imports the same `quote` logic
and `wire` codecs as a separate [native client module](service/main.go). The pure
libraries have no goSVM SDK or compiler dependency. The arithmetic permits
reserves/input sums up to 1,000,000,000 and rejects sequence overflow; it is a
small state-update example without token transfers.

The wire layout is explicit little endian: a 24-byte pool stores X reserve,
Y reserve and sequence; a 16-byte instruction stores input amount and minimum
output. The native client round-trips those canonical types and produces a quote
of 1,998 for 1,000 input at reserves 1,000,000/2,000,000. Invalid input returns
custom error 7; slippage returns 9. Failed handlers do not update pool bytes.
The legacy ABI's account validation remains separate from the shared calculation.

From the repository root:

```sh
GOCACHE="$PWD/build/lifecycle-go-cache" bash scripts/build-cli.sh build/gosvm
GOCACHE="$PWD/build/lifecycle-go-cache" build/gosvm -arch v3 \
  -o build/shared-program.so examples/shared-packages/program
```

These are separate Go modules, excluded from root tests:

```sh
cd examples/shared-packages
GOWORK=off go test ./...
cd service
GOWORK=off go run -ldflags=-linkmode=external .
```

Use an absolute workspace-local `GOCACHE` in the sandboxed macOS environment.
The service's `replace example.com/shared-app => ..` is a local-development
stand-in for this unpublished module. It is an ordinary application dependency,
not SDK wiring. The [verification script](../../scripts/import_verify.py) copies
only `model`, `math`, `quote`, `wire` and `go.mod` into isolated staging and runs
that service without the on-chain package or a dependency on the goSVM checkout.

[Validation](../../results/compiler/2026-10-05-shared-imports-final/README.md)
binds repeated SBF execution to ordinary-Go expectations and checks dependency
cache invalidation. See [import support and limits](../../docs/GO_PACKAGES.md).
These codecs are handwritten; schema-2 generation, the full token swap, escrow
and SDK dependency distribution remain upcoming milestone 2 work.
