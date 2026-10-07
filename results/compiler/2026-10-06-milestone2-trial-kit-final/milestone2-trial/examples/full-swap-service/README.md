# Ordinary Go swap consumer

This separate Go module imports the generated full swap's actual `model.Pool`,
`model.SwapArgs`, `model.ManagedPool`, lifecycle arguments, quote function and
generated client codecs. It can use ordinary
Go libraries; its program uses flags, JSON and hex encoding. It has no runtime SDK
or compiler imports and no handwritten goSVM SDK replacement. Its one local
module replacement is normal development resolution for the unpublished swap.

```sh
GOWORK=off go test ./...
GOWORK=off go run -ldflags=-linkmode=external . \
  -pool <hex-account-data> -x 1000000 -y 2000000 -amount 1000
```

The command decodes pool account data, reports the shared quote and constructs
swap instruction data. Add `-managed` for the new managed-pool layout and
`ManagedSwap` instruction; the output includes its canonical creator/core state.
Vault balances are supplied explicitly; it is not an RPC
backend or transaction submission tool. Tests independently construct common
wire vectors and compare 1,000 quote inputs against arbitrary-precision math.
The full-swap verification script also runs an isolated copy and checks its
package dependencies exclude the goSVM compiler/runtime.

Native tests check both layouts and the create/close/managed-swap codecs against
independent wire expectations. The isolated full-swap proof runs both service
modes and verifies that dependency resolution excludes the compiler/runtime SDK.
The service never copies the handler's types or arithmetic.
