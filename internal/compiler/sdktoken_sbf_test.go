package compiler

import (
	"encoding/base64"
	"encoding/binary"
	"testing"

	"gosvm/sdk/fixture"
)

// tokenCPIFixture drives examples/token-cpi against the real Token-2022 and
// classic Token programs. The Token-2022 scenario follows Hyperlane's synthetic
// warp route: a 234-byte PDA mint with a MetadataPointer that is its own mint
// authority, a self-owned escrow, MintToChecked/TransferChecked/BurnChecked
// and GetAccountDataSize. The classic scenario follows the collateral route.
func tokenCPIFixture() ([]byte, error) {
	f := fixture.New("gosvm sdk token cpi")
	t22 := splToken2022Image.declare(f)
	classic := splTokenImage.declare(f)
	funder := f.Signer("funder", 10_000_000_000)
	issuer := f.Signer("issuer", 1_000_000_000)
	user := f.Signer("user", 1_000_000_000)
	userToken := f.Signer("user-token", 0) // a new keypair account
	mint := f.PDA("mint", []byte("mint"))
	escrow := f.PDA("escrow", []byte("escrow"))
	classicMint := f.Account("classic-mint", fixture.State{Data: splMintData(&issuer.Key, 1000, 6), Owner: classic.Key, Lamports: f.MinimumBalance(82)})
	classicUser := f.Account("classic-user", fixture.State{Data: splAccountData(classicMint.Key, user.Key, 1000), Owner: classic.Key, Lamports: f.MinimumBalance(165)})

	wrong := byte(0) // an off-curve bump other than the mint's
	for b := int(mint.Bump) - 1; b >= 0; b-- {
		if _, ok := probeAddress(f.ProgramID(), []byte("mint"), []byte{byte(b)}); ok {
			wrong = byte(b)
			break
		}
	}
	type acct struct {
		a        fixture.Account
		writable bool
		signer   bool
	}
	w := func(a fixture.Account) acct { return acct{a, true, false} }
	r := func(a fixture.Account) acct { return acct{a, false, false} }
	s := func(a fixture.Account) acct { return acct{a, false, true} }
	ws := func(a fixture.Account) acct { return acct{a, true, true} }
	system := fixture.Account{Name: "system", Key: fixture.SystemProgram}
	rent := fixture.Account{Name: "rent-sysvar", Key: fixture.RentSysvar}
	// ix encodes opcode, account indices 0..4 into accounts, amount, decimals and seeds.
	ix := func(op byte, idx [5]byte, amount uint64, decimals byte, seeds byte, accounts ...acct) fixture.Instruction {
		data := make([]byte, 17)
		data[0] = op
		copy(data[1:6], idx[:])
		binary.LittleEndian.PutUint64(data[6:], amount)
		data[14], data[15] = decimals, seeds
		switch seeds {
		case 1:
			data[16] = mint.Bump
		case 2:
			data[16] = escrow.Bump
		case 3: // "mint" seeds with the wrong bump
			data[15], data[16] = 1, wrong
		}
		out := fixture.Instruction{Data: data}
		for _, a := range accounts {
			out.Accounts = append(out.Accounts, fixture.AccountMeta{Key: a.a.Key, Signer: a.signer, Writable: a.writable})
		}
		return out
	}
	const funds = 10_000_000_000
	rent234, rent165, rent82 := f.MinimumBalance(234), f.MinimumBalance(165), f.MinimumBalance(82)
	pointer := append(append([]byte(nil), funder.Key[:]...), mint.Key[:]...)
	mintState := func(supply uint64) fixture.State {
		return fixture.State{Data: withExtensions(splMintData(&mint.Key, supply, 6), 1, tlvEntry(18, pointer)), Owner: t22.Key, Lamports: rent234}
	}
	tokenState := func(m, owner fixture.Account, program fixture.Account, amount uint64) fixture.State {
		return fixture.State{Data: splAccountData(m.Key, owner.Key, amount), Owner: program.Key, Lamports: rent165}
	}
	returned := func(n uint64) string {
		b := make([]byte, 8)
		binary.LittleEndian.PutUint64(b, n)
		return "Program return: " + t22.String() + " " + base64.StdEncoding.EncodeToString(b)
	}

	sc := f.Scenario("synthetic-token-2022")
	// The program creates the PDA mint; the metadata pointer and mint
	// initialization are client instructions in Hyperlane, CPIs here.
	sc.Step(
		ix(8, [5]byte{0, 1, 2, 3}, 234, 0, 1, r(system), ws(funder), w(mint), r(t22)),
		ix(7, [5]byte{0, 1, 2, 1}, 0, 0, 0, r(t22), w(mint), r(funder)),
		ix(0, [5]byte{0, 1, 1}, 0, 6, 0, r(t22), w(mint)),
	).Expect(mint, mintState(0)).ExpectLamports(funder, funds-rent234)
	sc.Step(
		ix(8, [5]byte{0, 1, 2, 3}, 165, 0, 0, r(system), ws(funder), ws(userToken), r(t22)),
		ix(4, [5]byte{0, 1, 2, 3}, 0, 0, 0, r(t22), w(userToken), r(mint), r(user)),
	).Expect(userToken, tokenState(mint, user, t22, 0)).ExpectLamports(funder, funds-rent234-rent165)
	sc.Step(
		ix(8, [5]byte{0, 1, 2, 3}, 165, 0, 2, r(system), ws(funder), w(escrow), r(t22)),
		ix(5, [5]byte{0, 1, 2, 1, 3}, 0, 0, 0, r(t22), w(escrow), r(mint), r(rent)),
	).Expect(escrow, tokenState(mint, escrow, t22, 0)).ExpectLamports(funder, funds-rent234-2*rent165)
	// The mint signs as its own authority: the same account twice in the metas.
	sc.Step(ix(1, [5]byte{0, 1, 2, 1}, 1000, 6, 1, r(t22), w(mint), w(userToken))).
		Expect(mint, mintState(1000)).Expect(userToken, tokenState(mint, user, t22, 1000))
	sc.Step(ix(3, [5]byte{0, 1, 2, 3, 4}, 300, 6, 0, r(t22), w(userToken), r(mint), w(escrow), s(user))).
		Expect(userToken, tokenState(mint, user, t22, 700)).Expect(escrow, tokenState(mint, escrow, t22, 300))
	// The escrow signs as its own owner.
	sc.Step(ix(3, [5]byte{0, 1, 2, 3, 1}, 100, 6, 2, r(t22), w(escrow), r(mint), w(userToken))).
		Expect(userToken, tokenState(mint, user, t22, 800)).Expect(escrow, tokenState(mint, escrow, t22, 200))
	sc.Step(ix(2, [5]byte{0, 1, 2, 3}, 50, 6, 0, r(t22), w(userToken), w(mint), s(user))).
		Expect(mint, mintState(950)).Expect(userToken, tokenState(mint, user, t22, 750))
	sc.Step(ix(6, [5]byte{0, 1}, 0, 0, 0, r(t22), r(mint))).ExpectLog(returned(165), 1)
	sc.Step(ix(6, [5]byte{0, 1}, 0, 1, 0, r(t22), r(mint))).ExpectLog(returned(170), 1)
	// Token-2022 errors propagate: MintDecimalsMismatch, OwnerMismatch, InsufficientFunds.
	sc.Step(ix(3, [5]byte{0, 1, 2, 3, 4}, 1, 9, 0, r(t22), w(userToken), r(mint), w(escrow), s(user))).Fails(18)
	sc.Step(ix(1, [5]byte{0, 1, 2, 3}, 1, 6, 0, r(t22), w(mint), w(userToken), s(user))).Fails(4)
	sc.Step(ix(2, [5]byte{0, 1, 2, 3}, 751, 6, 0, r(t22), w(userToken), w(mint), s(user))).Fails(1)
	// SDK checks: wrong PDA seeds, unsigned authority, read-only mint.
	sc.Step(ix(1, [5]byte{0, 1, 2, 1}, 1, 6, 3, r(t22), w(mint), w(userToken))).Fails(3013)
	sc.Step(ix(2, [5]byte{0, 1, 2, 3}, 1, 6, 0, r(t22), w(userToken), w(mint), r(user))).Fails(3013)
	sc.Step(ix(2, [5]byte{0, 1, 2, 3}, 1, 6, 0, r(t22), w(userToken), r(mint), s(user))).Fails(3014)

	sc = f.Scenario("collateral-classic-token")
	sc.Step(
		ix(8, [5]byte{0, 1, 2, 3}, 165, 0, 2, r(system), ws(funder), w(escrow), r(classic)),
		ix(5, [5]byte{0, 1, 2, 1, 3}, 0, 0, 0, r(classic), w(escrow), r(classicMint), r(rent)),
	).Expect(escrow, tokenState(classicMint, escrow, classic, 0)).ExpectLamports(funder, funds-rent165)
	sc.Step(ix(3, [5]byte{0, 1, 2, 3, 4}, 400, 6, 0, r(classic), w(classicUser), r(classicMint), w(escrow), s(user))).
		Expect(classicUser, tokenState(classicMint, user, classic, 600)).Expect(escrow, tokenState(classicMint, escrow, classic, 400))
	sc.Step(ix(3, [5]byte{0, 1, 2, 3, 1}, 150, 6, 2, r(classic), w(escrow), r(classicMint), w(classicUser))).
		Expect(classicUser, tokenState(classicMint, user, classic, 750)).Expect(escrow, tokenState(classicMint, escrow, classic, 250))
	sc.Step(ix(1, [5]byte{0, 1, 2, 3}, 10, 6, 0, r(classic), w(classicMint), w(classicUser), s(issuer))).
		Expect(classicMint, fixture.State{Data: splMintData(&issuer.Key, 1010, 6), Owner: classic.Key, Lamports: rent82}).
		Expect(classicUser, tokenState(classicMint, user, classic, 760))
	sc.Step(ix(2, [5]byte{0, 1, 2, 3}, 60, 6, 0, r(classic), w(classicUser), w(classicMint), s(user))).
		Expect(classicMint, fixture.State{Data: splMintData(&issuer.Key, 950, 6), Owner: classic.Key, Lamports: rent82}).
		Expect(classicUser, tokenState(classicMint, user, classic, 700))
	sc.Step(ix(3, [5]byte{0, 1, 2, 3, 4}, 1, 2, 0, r(classic), w(classicUser), r(classicMint), w(escrow), s(user))).Fails(18)
	// Classic Token has no MetadataPointer; a Token-2022-owned mint is not its mint.
	sc.Step(ix(7, [5]byte{0, 1, 2, 1}, 0, 0, 0, r(classic), w(classicMint), r(funder))).Fails(2001)
	sc.Step(ix(3, [5]byte{0, 1, 2, 3, 4}, 1, 6, 0, r(t22), w(classicUser), r(classicMint), w(escrow), s(user))).Fails(3010)
	return f.JSON()
}

// TestSDKTokenCPISBF runs examples/token-cpi against real Token-2022 and Token.
func TestSDKTokenCPISBF(t *testing.T) {
	t.Parallel()
	runSDKProbe(t, "token-cpi", tokenCPIFixture)
}
