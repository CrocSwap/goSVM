package compiler

import (
	"encoding/binary"
	"testing"

	"gosvm/sdk/fixture"
)

// systemPDAFixture drives examples/system-pda against the real System program:
// pre-funded PDA creation, a 0-byte System-owned vault (the Hyperlane
// native-collateral and ATA-payer shape), signed transfers out of it, separate
// allocate/assign and atomic rollback.
func systemPDAFixture() ([]byte, error) {
	f := fixture.New("gosvm sdk system pda")
	funder := f.Signer("funder", 10_000_000_000)
	vault := f.PDA("vault", []byte("vault"))
	plain := f.Signer("plain", 1_000_000_000)
	wrong := byte(0)
	for b := int(vault.Bump) - 1; b >= 0; b-- {
		if _, ok := probeAddress(f.ProgramID(), []byte("vault"), []byte{byte(b)}); ok {
			wrong = byte(b)
			break
		}
	}
	ix := func(op, bump byte, value uint64) fixture.Instruction {
		data := make([]byte, 10)
		data[0], data[1] = op, bump
		binary.LittleEndian.PutUint64(data[2:], value)
		return fixture.Instruction{Data: data, Accounts: []fixture.AccountMeta{
			{Key: fixture.SystemProgram}, {Key: funder.Key, Signer: true, Writable: true},
			{Key: vault.Key, Writable: op != 4 && op != 5}, {Key: plain.Key, Signer: true, Writable: true}}}
	}
	program := f.ProgramID()
	rent57, rent64, rent0 := f.MinimumBalance(57), f.MinimumBalance(64), f.MinimumBalance(0)
	const funds = 10_000_000_000

	s := f.Scenario("create-unfunded")
	s.Step(ix(0, vault.Bump, 57)).ExpectCreated(vault, make([]byte, 57), funder).ExpectInvocations(fixture.SystemProgram, 1)

	s = f.Scenario("create-partly-funded")
	s.Override(vault, fixture.Wallet(1000))
	s.Step(ix(0, vault.Bump, 57)).
		Expect(vault, fixture.State{Data: make([]byte, 57), Owner: program, Lamports: rent57}).
		ExpectLamports(funder, funds-(rent57-1000)).ExpectInvocations(fixture.SystemProgram, 3)

	s = f.Scenario("create-over-funded")
	s.Override(vault, fixture.Wallet(rent57+5000))
	s.Step(ix(0, vault.Bump, 57)).
		Expect(vault, fixture.State{Data: make([]byte, 57), Owner: program, Lamports: rent57 + 5000}).
		ExpectInvocations(fixture.SystemProgram, 2)

	s = f.Scenario("create-wrong-seeds")
	s.Override(vault, fixture.Wallet(1000))
	s.Step(ix(0, wrong, 57)).Fails(3023)

	s = f.Scenario("system-vault-transfers")
	s.Override(vault, fixture.Wallet(rent0+5_000_000))
	s.Step(ix(1, vault.Bump, 1_000_000)).
		ExpectLamports(vault, rent0+4_000_000).ExpectLamports(plain, 1_001_000_000)
	s.Step(ix(1, vault.Bump, 5_000_000)).Fails(1) // System ResultWithNegativeLamports
	s.Step(ix(1, wrong, 1)).Fails(3023)
	// Draining the vault deletes it; simulation reports the empty System record.
	s.Step(ix(1, vault.Bump, rent0+4_000_000)).ExpectClosed(vault, plain)

	s = f.Scenario("create-system-vault")
	s.Step(ix(6, vault.Bump, 0)).Expect(vault, fixture.Wallet(rent0)).ExpectLamports(funder, funds-rent0).ExpectInvocations(fixture.SystemProgram, 1)
	// Repeating it takes the funded path: no top-up, allocate(0) and assign(System).
	s.Step(ix(6, vault.Bump, 0)).FreshBlockhash().ExpectInvocations(fixture.SystemProgram, 2)

	s = f.Scenario("pda-allocate-assign")
	s.Override(vault, fixture.Wallet(rent64))
	s.Step(ix(2, vault.Bump, 64), ix(3, vault.Bump, 0)).
		Expect(vault, fixture.State{Data: make([]byte, 64), Owner: program, Lamports: rent64})
	s.Step(ix(2, vault.Bump, 1)).Fails(3022) // no longer an empty System account

	s = f.Scenario("signer-allocate-assign")
	s.Step(ix(4, 0, 32), ix(5, 0, 0)).
		Expect(plain, fixture.State{Data: make([]byte, 32), Owner: program, Lamports: 1_000_000_000})

	s = f.Scenario("create-then-failed-transfer-rolls-back")
	// The created vault is program-owned, so the second instruction's signed
	// System transfer is rejected and the whole transaction rolls back.
	s.Step(ix(0, vault.Bump, 57), ix(1, vault.Bump, 1)).FailsAt(1, 3022)
	return f.JSON()
}

// TestSDKSystemPDASBF runs examples/system-pda against the real System program.
func TestSDKSystemPDASBF(t *testing.T) {
	runSDKProbe(t, "system-pda", systemPDAFixture)
}
