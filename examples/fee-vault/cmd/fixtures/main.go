// Command fixtures writes testdata/svm.json, the local SVM scenarios for
// gosvm test --svm, using the generated client's instructions and encoders.
//
//	go run ./cmd/fixtures > testdata/svm.json
//
// main_test.go fails while the committed file differs from this output.
package main

import (
	"os"

	"example.org/gosvm/fee-vault/client"
	"example.org/gosvm/fee-vault/model"
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
	f := fixture.New("fee vault")
	authority := f.Signer("authority", 1_000_000_000)
	intruder := f.Signer("intruder", 1_000_000_000)
	beneficiary := f.Account("beneficiary", fixture.Wallet(2_000_000))
	recipient := f.Account("recipient", fixture.Wallet(3_000_000))
	observer := f.Account("observer", fixture.Wallet(4_000_000))
	fresh := f.AccountAt("fresh", f.KeyFor("fresh"), nil)
	vault := f.PDA("vault", []byte("vault"), authority.Key[:])

	data := client.EncodeAccountVault(model.Vault{Authority: authority.Key, Beneficiary: beneficiary.Key, Bump: vault.Bump})
	minimum := f.MinimumBalance(len(data))
	// funded is the vault holding fees above its rent-exempt minimum.
	funded := func(fees uint64) fixture.State {
		st := f.Owned(data)
		st.Lamports += fees
		return st
	}
	create := client.CreateInstruction(client.CreateKeys{Vault: vault.Key, Authority: authority.Key},
		model.CreateArgs{Beneficiary: beneficiary.Key, Bump: vault.Bump})
	claim := func(to fixture.Account) fixture.Instruction {
		return client.ClaimInstruction(client.ClaimKeys{Vault: vault.Key, Beneficiary: to.Key}, model.ClaimArgs{})
	}
	// Move's accounts by position: 0 vault, 1 authority, 2 recipient, 3 observer.
	move := func(signer, to fixture.Account, from, index byte, amount uint64) fixture.Instruction {
		return client.MoveInstruction(client.MoveKeys{Vault: vault.Key, Authority: signer.Key, Recipient: to.Key, Observer: observer.Key},
			model.MoveArgs{From: from, To: index, Amount: amount})
	}

	// Claim moves only what exceeds the rent-exempt minimum; a second claim
	// moves nothing. A fresh vault has nothing to claim.
	s := f.Scenario("claim-fees-above-rent").Override(vault, funded(5_000_000))
	s.Step(claim(beneficiary)).ExpectLamports(vault, minimum).ExpectLamports(beneficiary, 7_000_000)
	s.Step(claim(beneficiary)).FreshBlockhash()

	s = f.Scenario("create-then-claim-nothing")
	s.Step(create).ExpectCreated(vault, data, authority)
	s.Step(claim(beneficiary))

	f.Scenario("claim-to-another-account-fails").Override(vault, funded(5_000_000)).
		Step(claim(recipient)).Fails(client.ErrAccountRelation)

	// A program-owned source can credit any writable account: a wallet it
	// does not own, the signer, or an address with no account yet.
	s = f.Scenario("move-to-any-writable").Override(vault, funded(5_000_000))
	s.Step(move(authority, recipient, 0, 2, 1_000_000)).ExpectLamports(vault, minimum+4_000_000).ExpectLamports(recipient, 4_000_000)
	s.Step(move(authority, recipient, 0, 1, 1_000_000)).ExpectLamports(vault, minimum+3_000_000).ExpectLamports(authority, 1_001_000_000)

	f.Scenario("move-creates-a-wallet").Override(vault, funded(5_000_000)).
		Step(move(authority, fresh, 0, 2, 1_000_000)).ExpectLamports(vault, minimum+4_000_000).Expect(fresh, fixture.Wallet(1_000_000))

	f.Scenario("move-within-vault").Override(vault, funded(5_000_000)).
		Step(move(authority, recipient, 0, 0, 1_000_000))

	// MoveLamports checks the SDK rules before changing either balance.
	f.Scenario("move-more-than-balance-fails").Override(vault, funded(5_000_000)).
		Step(move(authority, recipient, 0, 2, minimum+5_000_001)).Fails(2020)
	f.Scenario("move-from-unowned-wallet-fails").Override(vault, funded(5_000_000)).
		Step(move(authority, recipient, 2, 0, 1)).Fails(2012)
	f.Scenario("move-from-signer-wallet-fails").Override(vault, funded(5_000_000)).
		Step(move(authority, recipient, 1, 0, 1)).Fails(2012)
	f.Scenario("move-to-readonly-fails").Override(vault, funded(5_000_000)).
		Step(move(authority, recipient, 0, 3, 1)).Fails(2011)
	f.Scenario("move-to-missing-index-fails").Override(vault, funded(5_000_000)).
		Step(move(authority, recipient, 0, 4, 1)).Fails(2005)
	f.Scenario("only-the-authority-moves").Override(vault, funded(5_000_000)).
		Step(move(intruder, recipient, 0, 2, 1)).Fails(client.ErrAccountRelation)

	// Rent exemption is left to the application: the runtime rejects the
	// transaction when the vault ends below it. (Crediting a new account below
	// its minimum is rejected by the solana-v3 runner but not by fogo-v0's, so
	// it is not asserted here.)
	f.Scenario("move-below-rent-fails").Override(vault, funded(5_000_000)).
		Step(move(authority, recipient, 0, 2, 5_000_001)).FailsWith(map[string]any{"InsufficientFundsForRent": map[string]int{"account_index": 2}})

	return f.JSON()
}
