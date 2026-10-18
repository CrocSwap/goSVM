# Release binary walkthrough and devnet attempt — October 18, 2026

**Status: the published v0.1.0-alpha.1 path works locally end to end; the devnet
deploy and calls are not done.** Every devnet airdrop request (six, 10:00–10:28
EDT) was refused with HTTP 429, "You've either reached your airdrop limit today
or the airdrop faucet has run dry". As instructed, no other funding source was
used. Program ID, transaction signatures and on-chain CU are therefore not
recorded yet.

## What ran

All commands ran with an isolated `HOME` (scratch directory, no Solana CLI
configuration or keypair present; `solana config get` confirmed it), the release
binary first on `PATH`, Go 1.22.0 and, after the macOS issue below, Go 1.23.12
via `GOTOOLCHAIN`. Host: macOS 26.6.2, arm64.

| Step | Result |
| --- | --- |
| `curl … install-release.sh \| sh -s -- --dir "$HOME/.local/bin"` | Installed `gosvm v0.1.0-alpha.1 experimental darwin-arm64` (binary SHA-256 `6028f44c…5e24`), checksum verified by the script |
| `gosvm toolchain install` | 64 s: platform-tools v1.57 (431 MB download, Clang/LLD kept, content pins OK) and runners 0.5.1 and 0.6.0 from the GitHub releases |
| `gosvm doctor` | fogo-v0 and solana-v3 backends and runners OK |
| [Tutorial](../../../docs/TUTORIAL.md) sections 1–4, code blocks applied verbatim to a fresh `gosvm new -schema 2 -sdk 2` | `check` OK, native tests pass, 5 SVM scenarios / 9 transactions pass on both targets; generated files, `idl.json`, `layouts.json` and fixtures byte-identical to [examples/tutorial](../../../examples/tutorial/README.md) |
| `gosvm deploy -network solana-devnet -check` | `solana-devnet (https://api.devnet.solana.com) accepts solana-v3 programs` |
| `gosvm deploy -network solana-devnet` | Created throwaway payer `2kHEhYThZiD1rXiTBkwrpC9qfwZiHsbmqcfZPjaKqKis` (in the scratch project's ignored `deploy/keys/`); stopped asking for about 0.162 SOL |
| Devnet airdrop (`solana airdrop 1 … --url https://api.devnet.solana.com`, then `requestAirdrop` RPC 0.5 SOL ×5) | All refused (429) |
| `simulateTransaction` of `solrpc`-built `Create` and `Increment` transactions on devnet, `sigVerify: true` | Both decoded and signature-checked; failed only with `AccountNotFound` (unfunded fee payer), so the client's message encoding is accepted by devnet |

## Local CU (LiteSVM, release binary, same fixtures on both targets)

ELFs: solana-v3 14,824 bytes (`571ca8b3…33e8`, identical from two separate
project directories), fogo-v0 16,000 bytes (`5bf3006a…3c61`).

| Transaction | solana-v3 (runner 0.6.0) | fogo-v0 (runner 0.5.1) |
| --- | ---: | ---: |
| `Create` (PDA check, Rent, System create CPI) | 14,790 | 14,844 |
| `Increment` | 1,995 | 1,995 |
| `Increment` past limit (4101) | 1,674 | 1,674 |
| `Increment` by a non-owner (6007) | 1,329 | 1,329 |
| `Create` with limit 0 (4101, creation rolled back) | 13,946 | 14,000 |
| `Preview` (return data) | 259 | 259 |

On-chain CU for the same instructions: pending the devnet run. These are the
tutorial counter's measurements, unrelated to the bounded starter, token swap or
protocol workloads.

## Rough edges found

- **Fixed (CLI after alpha.1):** `gosvm test --svm` in a schema-2 project failed
  after building both targets with `open …/testdata/sbf.json: no such file or
  directory`. It now uses `testdata/svm.json` when there is no schema-1
  `sbf.json`, and otherwise fails before building with the fix named. The
  tutorial passes `-svm-fixtures` so it works with alpha.1.
- **Fixed:** `gosvm new` suggested `go test ./...` next; it now suggests `gosvm test`.
- **macOS 26 with Go 1.22:** binaries built by Go 1.22.0 and 1.22.12 that link
  networking (`go run ./cmd/counter`, test binaries importing `net/http`) abort
  with `dyld: missing LC_UUID load command`; Go 1.23.12 works, as does
  `-ldflags=-linkmode=external`. Documented in the tutorial.
- **`gosvm call` passes only the payer** as the instruction's single account, so
  it cannot call instructions with state accounts (here `Create`/`Increment`).
  The tutorial uses a payer-only `Preview` instruction and a Go client. Account
  arguments for `call` would be a larger CLI change.
- A fresh schema-2 scaffold has no SVM fixtures and no fixture-writing helper;
  writing exact account bytes and PDA addresses needs a generator such as the
  tutorial's `cmd/fixtures`.
- Replacing the scaffold leaves its `Vault`/`Policy`/`Move`/`SetLimit` entries
  in `layouts.json` as history (intended, but surprising in a new project).
- The deploy funding message prints amounts without a unit.
- The public devnet faucet's per-IP daily limit can block a first-time user at
  the deploy step; the tutorial points to faucet.solana.com with GitHub sign-in.

## To finish

Fund `2kHEhYThZiD1rXiTBkwrpC9qfwZiHsbmqcfZPjaKqKis` on devnet (about 0.2 SOL),
then from the scratch project run tutorial sections 5–6 and record the program
ID, deploy/call signatures and on-chain CU here. The throwaway keypair is kept
only in the untracked scratch directory until then; no keys are in this
repository.
