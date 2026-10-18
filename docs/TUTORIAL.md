# Your first goSVM program

This tutorial builds a small program with accounts, tests it natively and in a
local SVM, deploys it to Solana devnet and calls it from the command line and from
ordinary Go. It assumes you have finished the [README quickstart](../README.md#try-it):
`gosvm` is on your `PATH`, `gosvm toolchain install` has run and `gosvm doctor`
reports both targets OK. It takes about half an hour. No Solana CLI, Rust or
Anchor is needed; the Solana CLI is one way to get devnet SOL.

The program is a counter. Each wallet can create one counter with a limit, then
add to it; only its owner can, and never past the limit. That small rule set
exercises instruction arguments, a state account, signer and writable checks, a
program-derived address (PDA), rent-funded account creation and custom errors.
The finished project is [examples/tutorial](../examples/tutorial/README.md).

You need Go 1.22 or newer. On macOS 26, use Go 1.23 or newer: binaries built by
Go 1.22 that use networking abort there with `missing LC_UUID load command`
(adding `-ldflags=-linkmode=external` to `go run`/`go test` also works).

## 1. Create the project

```sh
gosvm new -schema 2 -sdk 2 -module example.com/counter counter
cd counter
```

`-schema 2` selects multi-instruction, multi-account programs and `-sdk 2` the
SDK with PDA, CPI, rent and account-creation helpers. Flags go before the
directory. The project targets both Fogo and Solana. It is an ordinary Go module:

| File | What it is |
| --- | --- |
| `gosvm.json` | The manifest: targets, state layouts, and each instruction's arguments and accounts |
| `model/` | Your state and argument types, as plain Go; shared with off-chain code |
| `program.go` | Your instruction handlers |
| `program_test.go` | Native Go tests |
| `cmd/fixtures/` | Writes `testdata/svm.json`, the scenarios run against the compiled program |
| `zz_gosvm.go` | Generated: the `Process` entrypoint, account checks and wire decoding |
| `client/zz_gosvm.go` | Generated: encoders and decoders for off-chain Go |
| `idl.json` | Generated description of instructions, accounts, layouts and error codes |
| `layouts.json` | History of every wire layout, so a layout cannot change silently |
| `.gosvm/sdk/` | A pinned copy of the SDK (`gosvm/solana`, `gosvm/sdk/...`) |

`gosvm check`, `test` and `build` regenerate the `zz_gosvm.go` files and the
JSON files; commit them along with the SDK copy. The scaffold is a small
two-ledger program. Replace it with the counter.

## 2. Write the program

### Shared types and rules

Replace `model/model.go`:

```go
// Package model holds the counter's state, instruction arguments and rules.
// It is ordinary Go: the on-chain program and off-chain services import it.
package model

type PublicKey [32]byte

// Counter is the state account. Each owner has one, at the address derived
// from the seed "counter", the owner's key and a bump byte.
type Counter struct {
	Owner PublicKey
	Count uint64
	Limit uint64
	Bump  byte
}

type CreateArgs struct {
	Limit uint64
	Bump  byte
}

type IncrementArgs struct {
	Amount uint64
}

type PreviewArgs struct {
	Count  uint64
	Limit  uint64
	Amount uint64
}

// Application error codes, returned as the transaction's custom error.
const (
	ErrZeroAmount uint64 = 4100
	ErrLimit      uint64 = 4101
)

// Next returns the count after adding amount, or a nonzero error code.
func Next(count, limit, amount uint64) (uint64, uint64) {
	if amount == 0 {
		return count, ErrZeroAmount
	}
	if amount > limit || count > limit-amount {
		return count, ErrLimit
	}
	return count + amount, 0
}
```

Account and argument structs are packed little-endian in field order, behind an
8-byte discriminator; `Counter` is 57 bytes on chain. Wire fields are fixed-size
integers, byte arrays and nested structs (`int`, `uint`, strings and maps are
rejected in wire types; handlers can still use them).

### Accounts and instructions

Replace the `layouts` and `instructions` in `gosvm.json` (keep the other fields):

```json
"layouts": [
  {"name": "Counter", "type": "model.Counter", "version": 1}
],
"instructions": [
  {
    "name": "Create", "handler": "Create", "args": "model.CreateArgs",
    "bundle": "CreateAccounts", "version": 1,
    "accounts": [
      {"name": "Counter", "kind": "state", "layout": "Counter",
       "owner": "program_id", "access": "write",
       "pda": {"program": "program_id", "seeds": [
         {"kind": "bytes", "hex": "636f756e746572"},
         {"kind": "key", "account": "Owner"},
         {"kind": "byte", "field": "args.Bump"}]},
       "init": {"payer": "Owner", "system": "System"}},
      {"name": "Owner", "kind": "signer", "access": "write",
       "owner": "0000000000000000000000000000000000000000000000000000000000000000"},
      {"name": "System", "kind": "program", "access": "read",
       "address": "0000000000000000000000000000000000000000000000000000000000000000"}
    ],
    "relations": []
  },
  {
    "name": "Increment", "handler": "Increment", "args": "model.IncrementArgs",
    "bundle": "IncrementAccounts", "version": 1,
    "accounts": [
      {"name": "Counter", "kind": "state", "layout": "Counter",
       "owner": "program_id", "access": "write"},
      {"name": "Owner", "kind": "signer", "access": "read"}
    ],
    "relations": [{"field": "Counter.Owner", "account": "Owner"}]
  },
  {
    "name": "Preview", "handler": "Preview", "args": "model.PreviewArgs",
    "bundle": "PreviewAccounts", "version": 1,
    "accounts": [{"name": "Caller", "kind": "signer", "access": "read"}],
    "relations": []
  }
]
```

Accounts are listed in the order a transaction passes them. What each part does:

- `kind: "state"` with `owner: "program_id"` is an account this program owns,
  decoded as the named layout. `access: "write"` requires it to be writable;
  after a successful handler the generated code writes it back.
- `kind: "signer"` requires a signature. The 64 zero hex digits are the System
  program's address: `Owner`'s `owner` requires an ordinary wallet, and `System`
  must be the System program itself.
- `pda` makes the counter's address a PDA under this program, from the bytes
  `"counter"` (hex `636f756e746572`), the owner's key and the bump byte passed in
  the arguments. Off-chain code finds the bump; no bump search runs on chain.
- `init` creates the account before the handler runs: the payer funds the
  rent-exempt minimum for 57 bytes and the program signs for the PDA. The target
  must not exist yet.
- `relations` binds a key stored in state to an account: `Increment` only runs
  when `Counter.Owner` equals the signing `Owner`.
- `Preview` needs only a signer. That makes it callable with `gosvm call`, which
  passes the fee payer as an instruction's only account.

Each instruction has an 8-byte discriminator derived from its name and version;
see `idl.json`. The generated code checks, in order, the instruction data, the
account count, signer/writable flags, owners and addresses, duplicate accounts,
state layouts, relations and PDAs, then creates any `init` account, runs your
handler and commits writable state. Each failed check returns a fixed code:

| Code | Check |
| --- | --- |
| 6000 | account count |
| 6001 | signer, writable or executable flags |
| 6002 | account owner |
| 6003 | state layout (size, discriminator) |
| 6004 | instruction data (discriminator, length) |
| 6005 | fixed address, such as the System program |
| 6006 | the same account passed twice |
| 6007 | relation |
| 6008 | PDA address |
| 6009 | `init` target or payer |

### Handlers

Replace `program.go`:

```go
package program

import (
	"encoding/binary"

	"example.com/counter/model"
	"gosvm/solana"
)

const ErrZeroAmount = model.ErrZeroAmount
const ErrLimit = model.ErrLimit

// Create initializes the caller's counter. Generated code has already checked
// the accounts and the PDA, and created the rent-exempt account.
func Create(c solana.Context, a CreateAccounts, args model.CreateArgs) (CreateAccounts, uint64) {
	if args.Limit == 0 {
		return a, ErrLimit
	}
	a.Counter = model.Counter{Owner: model.PublicKey(a.Owner.Key), Limit: args.Limit, Bump: args.Bump}
	return a, 0
}

// Increment adds to the counter. Only its owner can sign for it.
func Increment(c solana.Context, a IncrementAccounts, args model.IncrementArgs) (IncrementAccounts, uint64) {
	next, code := model.Next(a.Counter.Count, a.Counter.Limit, args.Amount)
	if code != 0 {
		return a, code
	}
	a.Counter.Count = next
	return a, 0
}

// Preview applies the same rule to its arguments, without any state, and
// returns the new count as eight little-endian bytes of return data.
func Preview(c solana.Context, a PreviewAccounts, args model.PreviewArgs) (PreviewAccounts, uint64) {
	next, code := model.Next(args.Count, args.Limit, args.Amount)
	if code != 0 {
		return a, code
	}
	var out [8]byte
	binary.LittleEndian.PutUint64(out[:], next)
	return a, solana.SetReturnData(c, out[:])
}
```

A handler receives the generated account bundle (decoded state, and a key and
index for every other account) and its decoded arguments, and returns the
updated bundle and a status: 0 for success, anything else fails the
transaction with that custom error code and rolls back every change. Constants
named `Err*` in the root package are listed under `errors` in `idl.json`; they
must be between 1 and 5999.

The scaffold's `program_test.go` and its fixture generator `cmd/fixtures` test
the old program, so delete them for now (`rm -r program_test.go cmd/fixtures`),
then check the program against goSVM's Go subset:

```console
$ gosvm check
check OK (442ms)
```

`layouts.json` keeps the scaffold's `Vault`, `Policy`, `Move` and `SetLimit`
entries as history, and your new entries are added. That is intended: a
recorded layout can later change only with a new version and a migration note.

## 3. Test the logic natively

Native tests call the generated `Process` entrypoint directly with a
`solana.Context` that lists the accounts, much as the runtime would. Create
`program_test.go`:

```go
package program_test

import (
	"bytes"
	"testing"

	program "example.com/counter"
	"example.com/counter/client"
	"example.com/counter/model"
	"gosvm/solana"
)

var programID = bytes.Repeat([]byte{7}, 32)

// incrementContext is an Increment instruction with a program-owned counter
// (count 5, limit 10) and its owner as signer: the accounts a client sends.
func incrementContext(amount uint64) solana.Context {
	owner := model.PublicKey{1}
	counter := model.Counter{Owner: owner, Count: 5, Limit: 10, Bump: 254}
	return solana.Context{
		ID:              programID,
		InstructionData: client.EncodeInstructionIncrement(model.IncrementArgs{Amount: amount}),
		Accounts: []solana.Account{
			{Key: bytes.Repeat([]byte{2}, 32), Owner: programID, Data: client.EncodeAccountCounter(counter), Writable: true},
			{Key: owner[:], Owner: make([]byte, 32), Signer: true},
		},
	}
}

func TestIncrement(t *testing.T) {
	c := incrementContext(3)
	if code := program.Process(c); code != 0 {
		t.Fatalf("Process = %d", code)
	}
	counter, err := client.DecodeAccountCounter(c.Accounts[0].Data)
	if err != nil || counter.Count != 8 || counter.Limit != 10 {
		t.Fatalf("counter = %+v, %v", counter, err)
	}
}

func TestIncrementRejects(t *testing.T) {
	for _, tc := range []struct {
		name string
		code uint64
		edit func(*solana.Context)
	}{
		{"zero amount", program.ErrZeroAmount, func(c *solana.Context) {
			c.InstructionData = client.EncodeInstructionIncrement(model.IncrementArgs{})
		}},
		{"over limit", program.ErrLimit, func(c *solana.Context) {
			c.InstructionData = client.EncodeInstructionIncrement(model.IncrementArgs{Amount: 6})
		}},
		{"owner did not sign", 6001, func(c *solana.Context) { c.Accounts[1].Signer = false }},
		{"counter not writable", 6001, func(c *solana.Context) { c.Accounts[0].Writable = false }},
		{"counter owned by another program", 6002, func(c *solana.Context) { c.Accounts[0].Owner = bytes.Repeat([]byte{9}, 32) }},
		{"someone else signs", 6007, func(c *solana.Context) { c.Accounts[1].Key = bytes.Repeat([]byte{3}, 32) }},
		{"missing account", 6000, func(c *solana.Context) { c.Accounts = c.Accounts[:1] }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := incrementContext(3)
			tc.edit(&c)
			before := bytes.Clone(c.Accounts[0].Data)
			if code := program.Process(c); code != tc.code {
				t.Fatalf("Process = %d, want %d", code, tc.code)
			}
			if !bytes.Equal(before, c.Accounts[0].Data) {
				t.Fatal("a rejected instruction changed the counter")
			}
		})
	}
}
```

The [finished file](../examples/tutorial/program_test.go) also tests `Next` on
its own and `Preview`'s return data (through the context's `WriteReturnData`
callback). Run the tests with `gosvm test`, which regenerates and then runs
`go test ./...`; Go test flags go after `--`:

```console
$ gosvm test -- -v -run Increment
=== RUN   TestIncrement
--- PASS: TestIncrement (0.00s)
=== RUN   TestIncrementRejects
=== RUN   TestIncrementRejects/zero_amount
...
--- PASS: TestIncrementRejects (0.00s)
    --- PASS: TestIncrementRejects/zero_amount (0.00s)
    --- PASS: TestIncrementRejects/over_limit (0.00s)
    --- PASS: TestIncrementRejects/owner_did_not_sign (0.00s)
    --- PASS: TestIncrementRejects/counter_not_writable (0.00s)
    --- PASS: TestIncrementRejects/counter_owned_by_another_program (0.00s)
    --- PASS: TestIncrementRejects/someone_else_signs (0.00s)
    --- PASS: TestIncrementRejects/missing_account (0.00s)
PASS
ok  	example.com/counter	0.007s
```

Native tests run your Go with the Go toolchain, so they are fast and debuggable,
but they model the runtime with callbacks. `Create` calls the System program to
make the account, derives a PDA and relies on the runtime to undo everything when
a transaction fails; test those in a real SVM.

## 4. Test the compiled program in a local SVM

`gosvm test --svm` builds the program for each target and runs transaction
scenarios against the compiled ELF in a local SVM (LiteSVM), with no network.
Scenarios live in a fixture file of named accounts, test signers and ordered
transactions with exact expected account states
([format](SVM_FIXTURES.md)), written by a small Go program. The fixture
builder `gosvm/sdk/fixture`, in the SDK copy, derives keys and addresses and
tracks each account's expected state; the generated client supplies the
instructions and account bytes. (The builder and the generated instruction
builders are newer than v0.1.0-alpha.2; with that release, build `gosvm` from
source for this section.)

Recreate the generator as `cmd/fixtures/main.go`:

```go
// Command fixtures writes testdata/svm.json, the local SVM scenarios for
// gosvm test --svm, using the generated client's instructions and encoders.
//
//	go run ./cmd/fixtures > testdata/svm.json
package main

import (
	"encoding/binary"
	"os"

	"example.com/counter/client"
	"example.com/counter/model"
	"gosvm/sdk/fixture"
)

func main() {
	b, err := suiteJSON()
	if err != nil {
		os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
	os.Stdout.Write(b)
}

// suiteJSON returns the fixture file's contents.
func suiteJSON() ([]byte, error) {
	// Keys derive from this namespace and each name: local test identities only.
	f := fixture.New("counter tutorial")
	owner := f.Signer("owner", 1_000_000_000)
	intruder := f.Signer("intruder", 1_000_000_000)
	counter := f.PDA("counter", []byte("counter"), owner.Key[:])

	create := func(limit uint64) fixture.Instruction {
		return client.CreateInstruction(client.CreateKeys{Counter: counter.Key, Owner: owner.Key},
			model.CreateArgs{Limit: limit, Bump: counter.Bump})
	}
	increment := func(signer fixture.Account, amount uint64) fixture.Instruction {
		return client.IncrementInstruction(client.IncrementKeys{Counter: counter.Key, Owner: signer.Key},
			model.IncrementArgs{Amount: amount})
	}
	state := func(count uint64) []byte {
		return client.EncodeAccountCounter(model.Counter{Owner: owner.Key, Count: count, Limit: 10, Bump: counter.Bump})
	}
	// Create makes the rent-exempt counter, paid by the owner, with one
	// System program invocation.
	created := func(s *fixture.Scenario) {
		s.Step(create(10)).ExpectCreated(counter, state(0), owner).ExpectInvocations(fixture.SystemProgram, 1)
	}

	s := f.Scenario("create-and-increment")
	created(s)
	s.Step(increment(owner, 3)).ExpectData(counter, state(3))
	s.Step(increment(owner, 7)).ExpectData(counter, state(10))

	s = f.Scenario("increment-past-limit")
	created(s)
	s.Step(increment(owner, 11)).Fails(model.ErrLimit)

	s = f.Scenario("only-the-owner-increments")
	created(s)
	s.Step(increment(intruder, 1)).Fails(client.ErrAccountRelation)

	// A failed transaction rolls back the account the generated code created.
	f.Scenario("failed-create-rolls-back").Step(create(0)).Fails(model.ErrLimit)

	var next [8]byte
	binary.LittleEndian.PutUint64(next[:], 8)
	f.Scenario("preview").
		Step(client.PreviewInstruction(client.PreviewKeys{Caller: owner.Key}, model.PreviewArgs{Count: 5, Limit: 10, Amount: 3})).
		ExpectReturn(next[:])

	return f.JSON()
}
```

`f.Signer` declares a test signer with a funded wallet and `f.PDA` derives the
counter's address and bump the way a client would; both are named in the
fixture file. `client.CreateInstruction` and the other generated builders take
the instruction's accounts by name and pass them in the declared order with
their signer and writable flags (the System program's fixed address comes from
`gosvm.json`). `client.ErrAccountRelation` and the other error codes match
`idl.json`.

Each step checks every declared account. A step's expectations change the
expected states and the rest carry over: `ExpectCreated` expects a new
program-owned account holding exactly those bytes and the rent-exempt balance
for them, paid by the owner, and `ExpectData` new data in an existing account.
A failing step (`Fails`) expects every account unchanged, because the runtime
rolls the transaction back. `ExpectReturn` expects the program's return data.

Then add `cmd/fixtures/main_test.go`, so `gosvm test` fails while the committed
file differs from what the generator writes:

```go
package main

import (
	"bytes"
	"os"
	"testing"
)

// The committed fixtures must match what the generator writes.
func TestFixturesUpToDate(t *testing.T) {
	want, err := suiteJSON()
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("../../testdata/svm.json")
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("testdata/svm.json is stale (%v); run: go run ./cmd/fixtures > testdata/svm.json", err)
	}
}
```

Here is the failing `Create` in the generated file, abbreviated:

```json
{
  "name": "failed-create-rolls-back",
  "steps": [{
    "instructions": [{
      "program": "program",
      "data": "b2840bbe154bd9eb0000000000000000ff",
      "accounts": [
        {"account": "counter", "signer": false, "writable": true},
        {"account": "owner", "signer": true, "writable": true},
        {"account": "system", "signer": false, "writable": false}]
    }],
    "expect": {
      "error": {"InstructionError": [0, {"Custom": 4101}]},
      "accounts": [
        {"account": "owner", "state": {"lamports": 1000000000, "owner": "11111111111111111111111111111111", ...}},
        {"account": "intruder", "state": {...}},
        {"account": "counter", "absent": true}]
    }
  }]
}
```

The limit is zero, so the handler returns `ErrLimit` after the generated code
has already created and funded the counter. The transaction fails, so the
runtime undoes the creation: the counter does not exist and the owner keeps its
lamports. Native tests cannot show that; the SVM does. Generate the file and run:

```console
$ go run ./cmd/fixtures > testdata/svm.json
$ gosvm test --svm
ok  	example.com/counter	0.011s
...
Built .../build/fogo-v0/v1.57/f731d72ca2cb5616/deps-ef006d66c9b97d33/program.so (16000 bytes, 2.2s)
fogo program: .../build/fogo/program.so
Testing 5 compiled SBF scenarios with LiteSVM...
PASS create-and-increment step 1          14844 CU
PASS create-and-increment step 2          1995 CU
PASS create-and-increment step 3          1995 CU
PASS increment-past-limit step 1          14844 CU
PASS increment-past-limit step 2          1674 CU
PASS only-the-owner-increments step 1     14844 CU
PASS only-the-owner-increments step 2     1329 CU
PASS failed-create-rolls-back step 1      14000 CU
PASS preview step 1                       259 CU
Built .../build/solana-v3/v1.57/f731d72ca2cb5616/deps-ef006d66c9b97d33/program.so (14824 bytes, 3.31s)
solana program: .../build/solana/program.so
Testing 5 compiled SBF scenarios with LiteSVM...
PASS create-and-increment step 1          14790 CU
PASS create-and-increment step 2          1995 CU
...
PASS preview step 1                       259 CU
```

The native tests run first, then each target is built and tested: `fogo`
(SBPFv0) and `solana` (SBPFv3) are different binaries. Each line is one
transaction and the compute units (CU) it used; a transaction may use 200,000
per instruction by default. `Create` costs about 14,800 CU: it checks the PDA,
reads Rent and calls the System program to create the account. An `Increment`
costs 1,995 CU and a rejected one less, because it stops earlier. CU can differ
slightly between targets and runtimes. Full results, including logs, are in
`svm-results.json` next to each built program.

When an expectation does not hold, the run stops at that step with the
difference and the program's logs. For example, a scenario that expected
incrementing by exactly the limit to fail:

```console
PASS increment-past-limit step 1          14790 CU
gosvm: increment-past-limit step 2 simulation: transaction error null; expected {
              "InstructionError": [
                0,
                {
                  "Custom": 4101
                }
              ]
            }
Program A4bd41Uazko5AD5syV6owBKns4v9HdFuCYPW2U5w5Ari invoke [1]
Program A4bd41Uazko5AD5syV6owBKns4v9HdFuCYPW2U5w5Ari consumed 1995 of 200000 compute units
Program A4bd41Uazko5AD5syV6owBKns4v9HdFuCYPW2U5w5Ari success
```

Add `-target solana` to test one target and `-svm-run <regexp>` to select
scenarios. Fixture keys come from fixed test seeds; they are never used on a
real network. The [fixture builder reference](SVM_FIXTURES.md#writing-fixtures-in-go)
covers accounts that exist before a scenario, closing, other programs, Clock and
Rent, and compute-unit checks.

Section 6 calls the deployed program from Go with `cmd/counter` and a small
Solana helper package, `solrpc` (base58, PDA derivation, transactions and the
JSON-RPC calls it uses). Copy both from the goSVM repository now:

```sh
git clone --depth 1 https://github.com/CrocSwap/goSVM gosvm-src
cp -R gosvm-src/examples/tutorial/solrpc .
cp -R gosvm-src/examples/tutorial/cmd/counter cmd/
rm -rf gosvm-src
go get filippo.io/edwards25519@v1.1.0
```

`solrpc` keeps the example free of a Solana SDK; it uses
[`filippo.io/edwards25519`](https://pkg.go.dev/filippo.io/edwards25519) only to
check that a derived address is off the curve. Off-chain packages like these and
`cmd/` are ordinary Go: only code reachable from the on-chain entrypoint has to
fit goSVM's subset, so they never reach the compiler.

## 5. Deploy to Solana devnet

First check that devnet accepts the program. This builds the `solana` target and
reads the cluster's live feature gates; it sends nothing and needs no keys:

```console
$ gosvm deploy -network solana-devnet -check
Built .../build/solana-v3/v1.57/f731d72ca2cb5616/deps-25c3169730b00da4/program.so (14824 bytes, 813ms)
solana program: .../build/solana/program.so
solana-devnet (https://api.devnet.solana.com) accepts solana-v3 programs
```

Deploying needs a funded payer. goSVM never uses the Solana CLI's configured
wallet, which may belong to something else entirely: the first deploy creates a
dedicated keypair for this project and network, then stops until you fund it:

```console
$ gosvm deploy -network solana-devnet
...
Created deployment payer .../counter/deploy/keys/solana-devnet-payer.json (address CmUSAqkhVTMR63AHPYNJUHAJVMSCSybJyt1i9ucaK3hT)
gosvm: payer CmUSAqkhVTMR63AHPYNJUHAJVMSCSybJyt1i9ucaK3hT has 0.000000 on solana-devnet; deployment needs about 0.162370. Fund it (https://faucet.solana.com or solana airdrop -u devnet), then rerun
```

`deploy/keys/` gets its own `.gitignore`, so keys stay out of Git. The amounts
in that message are in SOL: deploying this 14.8 KB program needs about 0.16 SOL
(rent for the program's data, the same again for a temporary upload buffer that
is refunded, and fees). Fund the printed address with devnet SOL from
[https://faucet.solana.com](https://faucet.solana.com): paste the address,
choose devnet and request 0.5 or 1 SOL.

The faucet and the devnet airdrop RPC are rate-limited per IP address and day;
a refusal reads "You've either reached your airdrop limit today or the airdrop
faucet has run dry". If that happens, sign in to the faucet with GitHub for a
larger allowance, or try again later. The Solana CLI uses the same faucet; if
you use it, pass the devnet URL explicitly so it cannot act on another cluster:

```sh
solana airdrop 1 <payer address> --url https://api.devnet.solana.com
```

Once the payer has at least 0.16 SOL, rerun:

```console
$ gosvm deploy -network solana-devnet
...
Created program keypair .../counter/deploy/keys/solana-devnet-program.json (address 2cZLWu6XrXjBGyPEVoMzqw5eyXNyxtEzpaLTWa89cFsW); keep it to redeploy at the same address
Creating buffer Fk5cTMKsgyTboW4nkWUNWsanq6ZNEf6NFrwdxJKe1rNm (14861 bytes)
Writing 17 chunks
Deploying program 2cZLWu6XrXjBGyPEVoMzqw5eyXNyxtEzpaLTWa89cFsW
Deployed solana on solana-devnet at 2cZLWu6XrXjBGyPEVoMzqw5eyXNyxtEzpaLTWa89cFsW
Record: .../counter/deploy/solana-devnet.json
```

goSVM deploys through the upgradeable loader: it writes the ELF to a temporary
buffer, then deploys it with the payer as upgrade authority. The program's
address comes from `deploy/keys/solana-devnet-program.json`; keep that file to
redeploy at the same address. `deploy/solana-devnet.json` records the program
ID, payer, upgrade authority, ELF hash and target profile, and is safe to commit.
Run `gosvm deploy -network solana-devnet` again after a change and it upgrades
the program in place (an identical build sends nothing). `gosvm authority`
shows or transfers the upgrade authority, or makes the program immutable.

Fogo works the same way: `gosvm deploy -network fogo-testnet` builds the `fogo`
target (SBPFv0), creates a separate `fogo-testnet` payer and points you to
[faucet.fogo.io](https://faucet.fogo.io). Mainnets (`solana-mainnet`,
`fogo-mainnet`) also require `-mainnet`; goSVM is an unaudited alpha, so do not
deploy it there with real funds.

## 6. Call the program

### With `gosvm call`

`gosvm call` sends one instruction to the deployed program with hex data,
passing the project's payer as the instruction's only account (a signer). That
fits `Preview`. Its data is the discriminator `f52cd645c1e8e782` (from
`client.EncodeInstructionPreview`, or `idl.json`) followed by `Count`, `Limit` and
`Amount` as little-endian 64-bit integers; here 5, 10 and 3. By default the call
is simulated, which costs nothing:

```console
$ gosvm call -network solana-devnet -data f52cd645c1e8e78205000000000000000a000000000000000300000000000000
{
  "committed": false,
  "compute_units": 258,
  "error": null,
  "input_hex": "f52cd645c1e8e78205000000000000000a000000000000000300000000000000",
  "logs": [...],
  "network": "solana-devnet",
  "program_id": "2cZLWu6XrXjBGyPEVoMzqw5eyXNyxtEzpaLTWa89cFsW",
  "return_data_hex": "0800000000000000",
  "return_program": "2cZLWu6XrXjBGyPEVoMzqw5eyXNyxtEzpaLTWa89cFsW",
  "signature": ""
}
```

The return data is 8, little-endian. Starting from 9 instead
(`...0900000000000000...`) the program returns `ErrLimit`, reported as
`{"InstructionError":[0,{"Custom":4101}]}`. Add `-send` to submit a real
transaction; the output then includes its signature and `"committed": true`.
`gosvm call` reads the program ID and payer from `deploy/`; `-program` and
`-keypair` override them.

### From Go

`Create` and `Increment` need more accounts than `gosvm call` passes, so call
them from Go. `cmd/counter`, copied in section 4, is an ordinary Go program:
it reads the program ID from the deploy record, derives the counter's PDA and
bump, builds instructions with the generated client and sends them with
`solrpc`. The `increment` command first applies the program's own rule:

```go
counter := read(ctx, rpc, counterKey)
// The same rule the program enforces, checked before paying a fee.
next, code := model.Next(counter.Count, counter.Limit, amount)
if code != 0 {
	log.Fatalf("the program would reject this increment with error %d", code)
}
fmt.Printf("expect count %d\n", next)
send(ctx, rpc, owner, solrpc.Instruction{
	Program: programID,
	Accounts: []solrpc.AccountMeta{
		{Key: counterKey, Writable: true},
		{Key: ownerKey, Signer: true},
	},
	Data: client.EncodeInstructionIncrement(model.IncrementArgs{Amount: amount}),
})
```

`read` fetches the account and decodes it with `client.DecodeAccountCounter`
into a `model.Counter`. For brevity the command uses the deploy payer as the
counter's owner; pass `-keypair` to use another wallet. Run it from the
project directory:

```console
$ go run ./cmd/counter create 10
transaction 4uzWn7ZnrMv6mgg61pnJw5iFzh4VZ6gGphqLCwSf2Y9QyDRWyqkJRGGy74H5c43THxXKEPUb1xiiNNSqke3FTpBt
  https://explorer.solana.com/tx/4uzWn7ZnrMv6mgg61pnJw5iFzh4VZ6gGphqLCwSf2Y9QyDRWyqkJRGGy74H5c43THxXKEPUb1xiiNNSqke3FTpBt?cluster=devnet
  14794 compute units, error null
counter 8KLtz6PGFAd62kDkcLgpJ3nDC6ZBuWpUZuiTGKBX7aAz: count 0 of 10
$ go run ./cmd/counter increment 3
expect count 3
transaction 46DeYaKRpwYhVXRxLiG9ENGsi5pSXyZ4B4URncey46mLHV3rHrE396sBntoEowkEQcJEAyio7y5piHdywqSUR9pu
  https://explorer.solana.com/tx/46DeYaKRpwYhVXRxLiG9ENGsi5pSXyZ4B4URncey46mLHV3rHrE396sBntoEowkEQcJEAyio7y5piHdywqSUR9pu?cluster=devnet
  1994 compute units, error null
counter 8KLtz6PGFAd62kDkcLgpJ3nDC6ZBuWpUZuiTGKBX7aAz: count 3 of 10
$ go run ./cmd/counter increment 8
the program would reject this increment with error 4101
```

The client, a backend service or an indexer can all import `model` and
`client` like this: the same `Counter` type, the same `Next` rule and the
generated encoders the program decodes, with no copied offsets. Only the
program's own package has to fit goSVM's Go subset; off-chain code can use any
Go library, including a full Solana SDK in place of `solrpc`. The
[token-swap service](../examples/full-swap-service/README.md) shows the same
pattern with a quote calculation.


## 7. What's next

- [Supported Go](../README.md#what-go-is-supported): generics, interfaces,
  closures, maps, strings, `defer`, package variables and much of the standard
  library, with Go's results; unsupported code is rejected at compile time.
- [Real Go libraries on chain](../examples/real-libraries/README.md):
  `holiman/uint256` and Uniswap v3 math as ordinary `go.mod` dependencies, and
  [trimming](GO_PACKAGES.md#trimming) unused library code.
- [Bend libraries](BEND_LIBRARIES.md) and [Rust libraries](RUST_LIBRARIES.md)
  join a Go program with `gosvm library add`.
- Larger programs: the [SOL escrow](../examples/escrow/README.md) (deposits and
  closing accounts), the [token swap](../examples/full-swap/README.md) (SPL Token
  transfers and signed CPIs) and its [Go service](../examples/full-swap-service/README.md).
- Reference: [schema 2](SCHEMA2.md), [SDK 2](SDK2.md),
  [SVM fixtures](SVM_FIXTURES.md), [targets, deployment and upgrades](TARGET_PROFILES.md).

goSVM is an experimental, unaudited alpha. Keep to devnets and testnets.
