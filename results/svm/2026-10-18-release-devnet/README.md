# Release binary walkthrough and devnet run — October 18, 2026

**Status: the published v0.1.0-alpha.1 path works end to end, including a real
Solana devnet deploy and calls** ([below](#devnet-run)). On-chain CU is within 4
of the local LiteSVM measurement for every instruction. Public devnet airdrops
were refused all day, so the user funded the throwaway payer; the first funded
payer was lost with a cleared scratch directory, and a second one completed the
run.

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

On-chain CU is in the [devnet run](#devnet-run). These are the
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

## Payer lost; second payer unfunded

The user later funded `2kHEhYThZiD1rXiTBkwrpC9qfwZiHsbmqcfZPjaKqKis` with
0.25 SOL, but by then the session's scratch directory, which held the isolated
HOME and that payer's only keypair, had been cleared. That payer can no longer
sign; its 0.25 devnet SOL is stranded. Nothing was ever deployed from it.

The release was reinstalled into a new isolated HOME (same binary; same
solana-v3 ELF, `571ca8b3…33e8`; fixtures pass again), and `gosvm deploy`
created a second throwaway payer, `CmUSAqkhVTMR63AHPYNJUHAJVMSCSybJyt1i9ucaK3hT`.
Two airdrop requests for it failed (an RPC internal error, then the same 429).
It is unfunded and nothing has been deployed.

## Devnet run

The user funded the second payer with 0.25 devnet SOL. Its project directory had
been copied out of scratch first (to an untracked directory outside the repo), and
the run used the same isolated `HOME` and release binary, against
`https://api.devnet.solana.com` only.

- Program: [`2cZLWu6XrXjBGyPEVoMzqw5eyXNyxtEzpaLTWa89cFsW`](https://explorer.solana.com/address/2cZLWu6XrXjBGyPEVoMzqw5eyXNyxtEzpaLTWa89cFsW?cluster=devnet),
  upgradeable loader, upgrade authority the payer. Deployed ELF `571ca8b3…`, the
  same bytes as the local runs.
- Deploy: buffer `Fk5cTMKs…` (14,861 bytes, 17 chunks), deploy transaction
  [`39EMg6Yh…`](https://explorer.solana.com/tx/39EMg6YhmZq6CJMGL9JbN9X6h2cFJcoGuBSvYv4RbRAFGPuNTRJh7dr8QfghpwJok9QDx3qcM4XazRvmdeVe3YdZ?cluster=devnet).
  The whole run (deploy, counter account rent and fees) cost the payer about
  0.078 SOL (0.25 → 0.172); the upload buffer's rent was refunded.
- Counter PDA `8KLtz6PGFAd62kDkcLgpJ3nDC6ZBuWpUZuiTGKBX7aAz`.

| Call | How | Result | On-chain CU | Local CU (solana-v3) |
| --- | --- | --- | ---: | ---: |
| `Preview(5, 10, 3)` | `gosvm call` (simulate) | returns 8 | 258 | 259 |
| `Preview(9, 10, 3)` | `gosvm call` (simulate) | `Custom(4101)` (`0x1005`) | 150 | — |
| `Preview(5, 10, 3)` | `gosvm call -send`, [`3Xus9rK3…`](https://explorer.solana.com/tx/3Xus9rK3t6NAua7ocmMEKQgSnBUQi2rf2y1miUNNNyMhBR1SjUEBjLVR4qyD8zqenauqofkcbaTRF2V3fnvwHk4E?cluster=devnet) | committed | 258 | 259 |
| `Create(10)` | Go client, [`4uzWn7Zn…`](https://explorer.solana.com/tx/4uzWn7ZnrMv6mgg61pnJw5iFzh4VZ6gGphqLCwSf2Y9QyDRWyqkJRGGy74H5c43THxXKEPUb1xiiNNSqke3FTpBt?cluster=devnet) | count 0 of 10 | 14,794 | 14,790 |
| `Increment(3)` | Go client, [`46DeYaKR…`](https://explorer.solana.com/tx/46DeYaKRpwYhVXRxLiG9ENGsi5pSXyZ4B4URncey46mLHV3rHrE396sBntoEowkEQcJEAyio7y5piHdywqSUR9pu?cluster=devnet) | count 3 of 10 | 1,994 | 1,995 |
| `Increment(8)` | Go client | refused client-side by `model.Next` (4101), nothing sent | — | — |

The small CU differences (±4) are runtime differences between devnet (Agave
4.4.0-beta.0) and runner 0.6.0, not different code: the ELF is identical.
The tutorial's sections 5–6 now show this output. The payer and program
keypairs were deleted after the run; only the pubkeys above are recorded, and no
keys are in this repository. The program stays deployed on devnet, with no
remaining upgrade authority holder.
