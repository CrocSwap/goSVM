package program_test

import (
	"bytes"
	"crypto/sha256"
	"testing"

	program "example.org/gosvm/find-address"
	"example.org/gosvm/find-address/client"
	"example.org/gosvm/find-address/model"
	"gosvm/sdk/fixture"
	"gosvm/solana"
)

var programID = fixture.Key{7, 7, 7}

// deriveAddress is sol_create_program_address: SHA-256 of the seeds, the
// program and "ProgramDerivedAddress", status 1 for an on-curve result.
func deriveAddress(seeds, program, output []byte) uint64 {
	h := sha256.New()
	off := 1
	for j := 0; j < int(seeds[0]); j++ {
		n := int(seeds[off])
		h.Write(seeds[off+1 : off+1+n])
		off += 1 + n
	}
	h.Write(program)
	h.Write([]byte("ProgramDerivedAddress"))
	var k fixture.Key
	copy(k[:], h.Sum(nil))
	if fixture.OnCurve(k) {
		return 1
	}
	copy(output, k[:])
	return 0
}

func derive(t *testing.T, args model.DeriveArgs) ([]byte, uint64) {
	t.Helper()
	var returned []byte
	c := solana.Context{ID: programID[:], InstructionData: client.EncodeInstructionDerive(args), DeriveAddress: deriveAddress,
		WriteReturnData: func(b []byte) { returned = b },
		Accounts:        []solana.Account{{Key: bytes.Repeat([]byte{1}, 32), Owner: make([]byte, 32), Signer: true}}}
	return returned, program.Process(c)
}

func TestDeriveMatchesFindProgramAddress(t *testing.T) {
	skipped := 0
	for n := 0; n < 64; n++ {
		args := model.DeriveArgs{Length: byte(n % 33), Extra: byte(n % 5)}
		for j := range args.Seed {
			args.Seed[j] = byte(n*31 + j)
		}
		seeds := [][]byte{args.Seed[:args.Length]}
		for j := byte(0); j < args.Extra; j++ {
			seeds = append(seeds, []byte{j})
		}
		address, bump, err := fixture.FindProgramAddress(programID, seeds...)
		if err != nil {
			t.Fatal(err)
		}
		got, code := derive(t, args)
		if code != 0 || !bytes.Equal(got, append(address[:], bump)) {
			t.Fatalf("%d: %x %d, want %x %d", n, got, code, address, bump)
		}
		skipped += 255 - int(bump)
	}
	if skipped == 0 {
		t.Fatal("no vector skips an on-curve bump")
	}
}

func TestDeriveRejects(t *testing.T) {
	if _, code := derive(t, model.DeriveArgs{Length: 33}); code != program.ErrLength {
		t.Fatal(code)
	}
	// One seed plus 15 more fills the builder: no room for the bump.
	if _, code := derive(t, model.DeriveArgs{Length: 1, Extra: 15}); code != 3001 {
		t.Fatal(code)
	}
}

func TestCreateRequiresTheCanonicalBump(t *testing.T) {
	authority := fixture.Key{4}
	_, canonical, err := fixture.FindProgramAddress(programID, []byte("record"), authority[:])
	if err != nil {
		t.Fatal(err)
	}
	c := solana.Context{ID: programID[:], DeriveAddress: deriveAddress}
	a := program.CreateAccounts{Authority: program.GosvmAccountRef{Index: 1, Key: authority}}
	got, code := program.Create(c, a, model.CreateArgs{Bump: canonical})
	if code != 0 || got.Record != (model.Record{Authority: authority, Bump: canonical}) {
		t.Fatal(code, got.Record)
	}
	if _, code = program.Create(c, a, model.CreateArgs{Bump: canonical - 1}); code != program.ErrNotCanonical {
		t.Fatal(code)
	}
}
