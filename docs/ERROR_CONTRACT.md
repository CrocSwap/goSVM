# Error and failure contract

Stable from October 14, 2026, at the request of the Qushta verified backend,
which reproduces this behaviour byte for byte. Everything listed here is a
contract. To change any of it, announce the change to Qushta before landing it
and record it in the changelog at the end. Adding a new code in an unused number
does not change the contract; changing the meaning, number or check order of an
existing code does, and so does turning a trap into a code or a code into a trap.

## Traps

These failures call the VM `abort` syscall: a static syscall hash on v3, the
legacy `abort` import on v0 (`internal/compiler/compiler.go`). The instruction
fails with no custom error code and the transaction rolls back.

- Division or modulo by zero.
- Array index, slice index or slice-expression bounds out of range.
- Nil pointer dereference, field access or method receiver.
- Account index out of range in SDK account accessors.

These are **not** traps:

- Unsigned `+`, `-`, `*` and unary `-` wrap modulo 2^width.
- A shift by the operand's width or more yields 0.
- Unsigned conversions truncate or zero-extend.
- Signed arithmetic is not in the subset, so it never runs.

## Error codes

A nonzero `Process` result is returned unchanged as the transaction's custom
error. Generated entrypoints, adapters and SDK helpers use these numbers;
application handlers use `Err*` constants in 1..5999.

### Entrypoint (generated C)

| Code | Meaning |
| --- | --- |
| 1001 | More accounts than the program's maximum |
| 1002 | Malformed duplicate-account marker |
| 1003 | Account data longer than 10 MiB |
| 1004 | Instruction data longer than 10,240 bytes |

SDK 1 single-account programs use 10–14 for their input checks
(`internal/compiler/abi.c.txt`).

### Runtime boundary (`solana` package)

| Code | Meaning |
| --- | --- |
| 2001 | Program account not executable, or not the classic Token program where required |
| 2002 | SDK 1 signed-transfer seed longer than 32 bytes |
| 2003 | SHA-256 input or output range out of bounds |
| 2004 | SHA-256 result not 32 bytes |
| 2005 | Account index out of range |
| 2006 | Malformed CPI account metas |
| 2007 | Malformed signer seeds or PDA input |
| 2008 | CPI instruction data longer than 10,240 bytes |
| 2009 | Native callback missing (native tests only) |
| 2010 | Resize or original length beyond limits |
| 2011 | Account not writable, or executable |
| 2012 | Account not owned by the program |
| 2013 | Refund account is the source, or malformed native key |
| 2014 | Refund lamport overflow |
| 2015 | Rent output not 17 bytes |
| 2016 | Clock output not 40 bytes |
| 2017 | Return data longer than 1,024 bytes |

### SDK 2 helpers

| Code | Meaning |
| --- | --- |
| 3001 | Seeds: more than 16, or one longer than 32 bytes, or nil where required |
| 3002 | CPI metas: more than 16, or nil |
| 3003 | CPI signers: more than 2 groups or 1,024 bytes |
| 3010 | Token account invalid (owner, executable, 165-byte layout, state, native) |
| 3011 | Token account identity |
| 3012 | Token mints differ |
| 3013 | Token authority or close authority mismatch, or missing signature |
| 3014 | Token account not writable |
| 3020 | Rent: size over 10 MiB, overflow or invalid threshold |
| 3021 | System program account invalid |
| 3022 | System create: account, alias, layout or privilege |
| 3023 | System create: payer signature, or target signer or PDA |
| 3024 | System create: size over 10,240 bytes |

Once a CPI's invoke syscall runs, a failure inside the callee fails the whole
transaction; the codes above come only from checks made before invoking.

### Schema 2 adapter

Checks run in this order; within each phase, accounts follow declaration order
(`internal/project/generate2.go`, [schema 2](SCHEMA2.md)):

1. Instruction discriminator and length: 6004.
2. Account count: 6000.
3. Writable, signer and executable flags: 6001.
4. Program ID length (6002, only when an account declares `owner:
   "program_id"`), then per account: key length and fixed address (6005),
   owner (6002).
5. Duplicate keys not declared as aliases: 6006.
6. Initialization target and payer empty: 6009.
7. Owned state length and discriminator: 6003.
8. Checked token layouts: SDK codes 3010–3014.
9. Relations, including token mint and authority: 6007.
10. Close authority: 6007.
11. PDA re-derivation: 6008.
12. Initialization (`system.Create` codes), in declaration order.
13. The handler; a nonzero result returns unchanged.
14. Post-handler close of declared `close` accounts (`CloseAccount` codes).
15. Write-back of writable, non-closed state, in declaration order. For
    instructions with init or close declarations, each such account is first
    re-read and re-checked: length (6003), owner (6002), then the
    discriminator (6003), or, for a newly initialized account, the
    discriminator is written. Read-only state, token accounts and other
    references are never written back.

Handler results propagate unchanged.

## Changelog

- 2026-10-14: contract established at `da6bf04` plus this document.
- 2026-10-14: steps 4 and 12–15 spelled out to match existing generated code
  (no behaviour change).
