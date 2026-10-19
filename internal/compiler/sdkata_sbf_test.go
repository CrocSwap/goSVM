package compiler

import (
	"testing"

	"gosvm/sdk/fixture"
)

// ataCreateFixture drives examples/ata-create against the real ATA 1.1.1,
// Token-2022 and Token programs. It follows Hyperlane's transfer_out: the
// recipient's associated account is created idempotently, paid by a data-less
// System-owned PDA, for a 234-byte metadata-pointer Token-2022 mint (a 170-byte
// account with ImmutableOwner) and for a classic mint, and derives addresses
// and PDA bumps on chain.
func ataCreateFixture() ([]byte, error) {
	f := fixture.New("gosvm sdk ata create")
	ataProgram := splATAImage.declare(f)
	t22 := splToken2022Image.declare(f)
	classic := splTokenImage.declare(f)
	funder := f.Signer("funder", 10_000_000_000)
	wallet := f.Signer("wallet", 1_000_000_000)
	payerKey, payerBump, err := fixture.FindProgramAddress(f.ProgramID(), []byte("ata_payer"))
	if err != nil {
		return nil, err
	}
	payerFunds := uint64(1_000_000_000)
	payerState := fixture.Wallet(payerFunds)
	ataPayer := f.AccountAt("ata-payer", payerKey, &payerState)
	owned := func(data []byte, owner fixture.Key) fixture.State {
		return fixture.State{Data: data, Owner: owner, Lamports: f.MinimumBalance(len(data))}
	}
	mintKey := f.KeyFor("mint")
	pointer := append(append([]byte(nil), funder.Key[:]...), mintKey[:]...)
	mint := f.Account("mint", owned(withExtensions(splMintData(&mintKey, 0, 6), 1, tlvEntry(18, pointer)), t22.Key))
	classicMint := f.Account("classic-mint", owned(splMintData(&funder.Key, 0, 6), classic.Key))
	address := func(program, m fixture.Key) (fixture.Key, byte) {
		k, bump, err := fixture.FindProgramAddress(ataProgram.Key, wallet.Key[:], program[:], m[:])
		if err != nil {
			panic(err)
		}
		return k, bump
	}
	ata22Key, ata22Bump := address(t22.Key, mint.Key)
	ataClassicKey, _ := address(classic.Key, classicMint.Key)
	ata22 := f.AccountAt("ata-2022", ata22Key, nil)
	ataClassic := f.AccountAt("ata-classic", ataClassicKey, nil)
	result := f.Account("result", f.Owned(make([]byte, 40)))
	system := fixture.Account{Name: "system", Key: fixture.SystemProgram}

	wrong := byte(0)
	for b := int(payerBump) - 1; b >= 0; b-- {
		if _, ok := probeAddress(f.ProgramID(), []byte("ata_payer"), []byte{byte(b)}); ok {
			wrong = byte(b)
			break
		}
	}
	type acct struct {
		a                fixture.Account
		writable, signer bool
	}
	r := func(a fixture.Account) acct { return acct{a: a} }
	w := func(a fixture.Account) acct { return acct{a: a, writable: true} }
	ws := func(a fixture.Account) acct { return acct{a: a, writable: true, signer: true} }
	ix := func(op byte, mode, bump byte, accounts ...acct) fixture.Instruction {
		data := make([]byte, 10)
		data[0] = op
		for j := range accounts {
			if j < 7 {
				data[1+j] = byte(j)
			}
		}
		data[8], data[9] = mode, bump
		out := fixture.Instruction{Data: data}
		for _, a := range accounts {
			out.Accounts = append(out.Accounts, fixture.AccountMeta{Key: a.a.Key, Signer: a.signer, Writable: a.writable})
		}
		return out
	}
	// create lists the ATA accounts in the SDK's order, paid by the PDA (mode 1) or a signer.
	create := func(op byte, payer acct, mode, bump byte, account, m, program fixture.Account) fixture.Instruction {
		return ix(op, mode, bump, r(ataProgram), payer, w(account), r(wallet), r(m), r(system), r(program))
	}
	ata22State := owned(withExtensions(splAccountData(mint.Key, wallet.Key, 0), 2, tlvEntry(7, nil)), t22.Key)
	rent170 := f.MinimumBalance(170)

	sc := f.Scenario("pda-payer-token-2022")
	sc.Step(create(0, w(ataPayer), 1, payerBump, ata22, mint, t22)).
		Expect(ata22, ata22State).ExpectLamports(ataPayer, payerFunds-rent170).ExpectInvocations(ataProgram.Key, 1)
	// Idempotent creation of an existing account changes nothing.
	sc.Step(create(0, w(ataPayer), 1, payerBump, ata22, mint, t22)).FreshBlockhash().ExpectInvocations(ataProgram.Key, 1)
	// Non-idempotent Create of an existing account fails in the ATA program.
	sc.Step(create(1, w(ataPayer), 1, payerBump, ata22, mint, t22)).FailsWith(fixture.InstructionError(0, "IllegalOwner"))

	sc = f.Scenario("signer-payer-classic")
	sc.Step(create(1, ws(funder), 0, 0, ataClassic, classicMint, classic)).
		Expect(ataClassic, owned(splAccountData(classicMint.Key, wallet.Key, 0), classic.Key)).
		ExpectLamports(funder, 10_000_000_000-f.MinimumBalance(165))

	sc = f.Scenario("derive-on-chain")
	derived := make([]byte, 40)
	copy(derived, ata22Key[:])
	derived[32] = ata22Bump
	sc.Step(ix(2, 0, 0, w(result), r(wallet), r(t22), r(mint))).ExpectData(result, derived)
	vault, vaultBump, err := fixture.FindProgramAddress(f.ProgramID(), []byte("vault"))
	if err != nil {
		return nil, err
	}
	found := make([]byte, 40)
	copy(found, vault[:])
	found[32] = vaultBump
	sc.Step(ix(3, 0, 0, w(result))).ExpectData(result, found)

	sc = f.Scenario("sdk-rejections")
	sc.Step(create(0, w(ataPayer), 1, payerBump, ataClassic, mint, t22)).FailsWith(fixture.InstructionError(0, "InvalidSeeds")) // ATA rejects the substituted address
	sc.Step(create(0, w(ataPayer), 1, payerBump, ata22, classicMint, t22)).Fails(3031)                                          // mint of another program
	sc.Step(create(0, w(ataPayer), 1, wrong, ata22, mint, t22)).Fails(3032)                                                     // wrong PDA payer seeds
	sc.Step(create(0, w(funder), 0, 0, ata22, mint, t22)).Fails(3032)                                                           // unsigned payer
	sc.Step(create(0, w(result), 0, 0, ata22, mint, t22)).Fails(3031)                                                           // program-owned payer
	sc.Step(ix(0, 0, 0, r(t22), ws(funder), w(ata22), r(wallet), r(mint), r(system), r(t22))).Fails(3030)                       // not the ATA program
	return f.JSON()
}

// TestSDKATACreateSBF runs examples/ata-create against real ATA and token programs.
func TestSDKATACreateSBF(t *testing.T) {
	runSDKProbe(t, "ata-create", ataCreateFixture)
}
