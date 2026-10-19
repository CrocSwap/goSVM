# Logging from a program

A schema-2 SDK-2 program that logs with `solana.Log`, `solana.Log64` and
`solana.LogPubkey`, the Go forms of Rust's `msg!`, `sol_log_64` and
`Pubkey::log`. `Note` counts a journal's notes and logs four lines:

```text
Program log: note
Program log: gas paid: 世界
Program log: 0x1, 0x1, 0xabc, 0x0, 0xffffffffffffffff
Program log: AhSsZs9cFhji9r6VbGKQvkHdjaYivvTJY5jjgd3738dC
```

`cmd/fixtures` writes `testdata/svm.json`
(`go run ./cmd/fixtures > testdata/svm.json`), asserting each line, and that
text which is not valid UTF-8 fails the instruction in the runtime's log
syscall and rolls back the count.

```sh
gosvm test --svm
```

Root tests omit this standalone module; test it with `GOWORK=off go test ./...`.
The fixture seeds are local test identities.
