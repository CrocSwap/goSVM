package compiler

import (
	"encoding/binary"
	"testing"

	"gosvm/sdk/fixture"
)

// feeConfigValue is a TransferFeeConfig extension value (108 bytes).
func feeConfigValue(authority fixture.Key, olderEpoch, olderMax uint64, olderPoints uint16, newerEpoch, newerMax uint64, newerPoints uint16) []byte {
	b := make([]byte, 108)
	copy(b, authority[:])
	binary.LittleEndian.PutUint64(b[72:], olderEpoch)
	binary.LittleEndian.PutUint64(b[80:], olderMax)
	binary.LittleEndian.PutUint16(b[88:], olderPoints)
	binary.LittleEndian.PutUint64(b[90:], newerEpoch)
	binary.LittleEndian.PutUint64(b[98:], newerMax)
	binary.LittleEndian.PutUint16(b[106:], newerPoints)
	return b
}

// tokenExtensionsFixture drives examples/token-extensions against real
// Token-2022: it first has Token-2022 create a transfer-fee mint and account
// and checks their exact bytes against the layouts the other scenarios preload,
// then decodes Hyperlane's 234-byte metadata-pointer mint and 170-byte ATA
// layout, and compares the SDK's fee with what Token-2022 withholds across an
// epoch change.
func tokenExtensionsFixture() ([]byte, error) {
	f := fixture.New("gosvm sdk token extensions")
	t22 := splToken2022Image.declare(f)
	classic := fixture.MustParseKey(splTokenImage.address)
	funder := f.Signer("funder", 10_000_000_000)
	issuer := f.Signer("issuer", 1_000_000_000)
	user := f.Signer("user", 1_000_000_000)
	other := f.Signer("other", 1_000_000_000)
	newMint := f.Signer("new-mint", 0)
	newAccount := f.Signer("new-account", 0)
	result := f.Account("result", f.Owned(make([]byte, 80)))
	feeMintData := func(key fixture.Key, supply uint64, config []byte) []byte {
		return withExtensions(splMintData(&issuer.Key, supply, 6), 1, tlvEntry(1, config))
	}
	feeAccountData := func(mint, owner fixture.Key, amount, fee uint64) []byte {
		value := make([]byte, 8)
		binary.LittleEndian.PutUint64(value, fee)
		return withExtensions(splAccountData(mint, owner, amount), 2, tlvEntry(2, value))
	}
	owned := func(data []byte, owner fixture.Key) fixture.State {
		return fixture.State{Data: data, Owner: owner, Lamports: f.MinimumBalance(len(data))}
	}
	feeMintKey := f.KeyFor("fee-mint")
	feeMint := f.AccountAt("fee-mint", feeMintKey, func() *fixture.State {
		s := owned(feeMintData(feeMintKey, 1_000_000, feeConfigValue(issuer.Key, 0, 1_000_000, 50, 5, 7, 300)), t22.Key)
		return &s
	}())
	source := f.Account("source", owned(feeAccountData(feeMintKey, user.Key, 1_000_000, 0), t22.Key))
	destination := f.Account("destination", owned(feeAccountData(feeMintKey, other.Key, 0, 0), t22.Key))
	pointerKey := f.KeyFor("pointer-mint")
	pointer := append(append([]byte(nil), issuer.Key[:]...), pointerKey[:]...)
	pointerMint := f.AccountAt("pointer-mint", pointerKey, func() *fixture.State {
		s := owned(withExtensions(splMintData(&pointerKey, 123, 9), 1, tlvEntry(18, pointer)), t22.Key)
		return &s
	}())
	ata := f.Account("ata", owned(withExtensions(splAccountData(pointerKey, user.Key, 77), 2, tlvEntry(7, nil)), t22.Key))
	classicMint := f.Account("classic-mint", owned(splMintData(nil, 5, 2), classic))
	classicAccount := f.Account("classic-account", owned(splAccountData(classicMint.Key, user.Key, 4), classic))

	type acct struct {
		a                fixture.Account
		signer, writable bool
	}
	// probe encodes opcode, result plus five more account indices, argument and decimals.
	probe := func(op byte, idx [5]byte, value uint64, decimals byte, accounts ...acct) fixture.Instruction {
		data := make([]byte, 16)
		data[0], data[1] = op, 0
		copy(data[2:7], idx[:])
		binary.LittleEndian.PutUint64(data[7:], value)
		data[15] = decimals
		out := fixture.Instruction{Data: data, Accounts: []fixture.AccountMeta{{Key: result.Key, Writable: true}}}
		for _, a := range accounts {
			out.Accounts = append(out.Accounts, fixture.AccountMeta{Key: a.a.Key, Signer: a.signer, Writable: a.writable})
		}
		return out
	}
	r := func(a fixture.Account) acct { return acct{a: a} }
	w := func(a fixture.Account) acct { return acct{a: a, writable: true} }
	s := func(a fixture.Account) acct { return acct{a: a, signer: true} }
	words := func(values ...uint64) []byte {
		b := make([]byte, 80)
		for j, v := range values {
			binary.LittleEndian.PutUint64(b[8*j:], v)
		}
		return b
	}
	mintResult := func(supply uint64, decimals byte, authority *fixture.Key) []byte {
		b := words(supply)
		b[8] = decimals
		if authority != nil {
			b[9] = 1
			copy(b[10:], authority[:])
		}
		return b
	}
	accountResult := func(amount uint64, mint, owner fixture.Key) []byte {
		b := words(amount)
		copy(b[8:], mint[:])
		copy(b[40:], owner[:])
		return b
	}
	systemCreate := func(target fixture.Account, space uint64) fixture.Instruction {
		data := make([]byte, 52)
		binary.LittleEndian.PutUint64(data[4:], f.MinimumBalance(int(space)))
		binary.LittleEndian.PutUint64(data[12:], space)
		copy(data[20:], t22.Key[:])
		return fixture.Instruction{Data: data, Accounts: []fixture.AccountMeta{{Key: funder.Key, Signer: true, Writable: true}, {Key: target.Key, Signer: true, Writable: true}}}
	}
	const funds = 10_000_000_000

	sc := f.Scenario("token-2022-writes-the-preloaded-layouts")
	feeInit := append([]byte{26, 0, 1}, issuer.Key[:]...)
	feeInit = append(append(feeInit, 0, 50, 0), 0x88, 0x13, 0, 0, 0, 0, 0, 0) // no withdraw authority, 50 bp, max 5000
	mint2 := append(append([]byte{20, 6}, issuer.Key[:]...), 0)
	sc.Step().
		Invoke(fixture.SystemProgram, systemCreate(newMint, 278)).
		Invoke(t22.Key, fixture.Instruction{Data: feeInit, Accounts: []fixture.AccountMeta{{Key: newMint.Key, Writable: true}}}).
		Invoke(t22.Key, fixture.Instruction{Data: mint2, Accounts: []fixture.AccountMeta{{Key: newMint.Key, Writable: true}}}).
		Expect(newMint, owned(feeMintData(newMint.Key, 0, feeConfigValue(issuer.Key, 0, 5000, 50, 0, 5000, 50)), t22.Key)).
		ExpectLamports(funder, funds-f.MinimumBalance(278))
	sc.Step().
		Invoke(fixture.SystemProgram, systemCreate(newAccount, 178)).
		Invoke(t22.Key, fixture.Instruction{Data: append([]byte{18}, user.Key[:]...), Accounts: []fixture.AccountMeta{{Key: newAccount.Key, Writable: true}, {Key: newMint.Key}}}).
		Expect(newAccount, owned(feeAccountData(newMint.Key, user.Key, 0, 0), t22.Key)).
		ExpectLamports(funder, funds-f.MinimumBalance(278)-f.MinimumBalance(178))
	sc.Step(probe(0, [5]byte{1}, 0, 0, r(newMint))).ExpectData(result, mintResult(0, 6, &issuer.Key))
	sc.Step(probe(1, [5]byte{1}, 0, 0, r(newAccount))).ExpectData(result, accountResult(0, newMint.Key, user.Key))

	sc = f.Scenario("transfer-fee-across-epochs")
	// Epoch 4 uses the older fee: ceil(10001 * 50 / 10000) = 51.
	sc.Step(probe(2, [5]byte{1, 2, 3, 4, 5}, 10_001, 6, r(feeMint), r(t22), w(source), w(destination), s(user))).
		Clock(fixture.Clock{Epoch: 4, LeaderScheduleEpoch: 4}).
		ExpectData(result, words(51, 9950, 51, 4)).
		ExpectData(source, feeAccountData(feeMintKey, user.Key, 1_000_000-10_001, 0)).
		ExpectData(destination, feeAccountData(feeMintKey, other.Key, 9950, 51))
	// From epoch 5 the newer fee applies: ceil(10001 * 300 / 10000) = 301, capped at 7.
	sc.Step(probe(2, [5]byte{1, 2, 3, 4, 5}, 10_001, 6, r(feeMint), r(t22), w(source), w(destination), s(user))).
		Clock(fixture.Clock{Epoch: 5, LeaderScheduleEpoch: 5}).FreshBlockhash().
		ExpectData(result, words(7, 9950+9994, 58, 5)).
		ExpectData(source, feeAccountData(feeMintKey, user.Key, 1_000_000-20_002, 0)).
		ExpectData(destination, feeAccountData(feeMintKey, other.Key, 9950+9994, 58))

	sc = f.Scenario("decode-hyperlane-layouts")
	sc.Step(probe(0, [5]byte{1}, 0, 0, r(pointerMint))).ExpectData(result, mintResult(123, 9, &pointerKey))
	sc.Step(probe(1, [5]byte{1}, 0, 0, r(ata))).ExpectData(result, accountResult(77, pointerKey, user.Key))
	found := words(170, 64)
	copy(found[16:], pointer)
	sc.Step(probe(3, [5]byte{1}, 18, 0, r(pointerMint))).ExpectData(result, found)
	sc.Step(probe(3, [5]byte{1}, 7, 0, r(ata))).ExpectData(result, words(170, 0))
	sc.Step(probe(0, [5]byte{1}, 0, 0, r(classicMint))).ExpectData(result, mintResult(5, 2, nil))
	sc.Step(probe(1, [5]byte{1}, 0, 0, r(classicAccount))).ExpectData(result, accountResult(4, classicMint.Key, user.Key))
	sc.Step(probe(0, [5]byte{1}, 0, 0, r(ata))).Fails(3012)
	sc.Step(probe(1, [5]byte{1}, 0, 0, r(pointerMint))).Fails(3010)
	sc.Step(probe(3, [5]byte{1}, 1, 0, r(pointerMint))).Fails(3015)
	sc.Step(probe(3, [5]byte{1}, 7, 0, r(classicAccount))).Fails(3010)

	sc = f.Scenario("reject-malformed-extensions")
	bad := withExtensions(splMintData(&pointerKey, 123, 9), 1, tlvEntry(18, pointer))
	bad[168] = 65 // the entry claims one byte more than the account holds
	sc.Override(pointerMint, owned(bad, t22.Key))
	sc.Step(probe(0, [5]byte{1}, 0, 0, r(pointerMint))).Fails(3012)
	sc.Step(probe(3, [5]byte{1}, 18, 0, r(pointerMint))).Fails(3010)
	return f.JSON()
}

// TestSDKTokenExtensionsSBF runs examples/token-extensions against real Token-2022.
func TestSDKTokenExtensionsSBF(t *testing.T) {
	runSDKProbe(t, "token-extensions", tokenExtensionsFixture)
}
