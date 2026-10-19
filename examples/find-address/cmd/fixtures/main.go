// Command fixtures writes testdata/svm.json, the local SVM scenarios for
// gosvm test --svm, using the generated client's instructions and encoders.
//
//	go run ./cmd/fixtures > testdata/svm.json
//
// main_test.go fails while the committed file differs from this output.
package main

import (
	"crypto/sha256"
	"fmt"
	"os"

	"example.org/gosvm/find-address/client"
	"example.org/gosvm/find-address/model"
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

// programAddress is create_program_address: ok is false on the curve.
func programAddress(program fixture.Key, bump byte, seeds ...[]byte) (fixture.Key, bool) {
	h := sha256.New()
	for _, s := range seeds {
		h.Write(s)
	}
	h.Write([]byte{bump})
	h.Write(program[:])
	h.Write([]byte("ProgramDerivedAddress"))
	var k fixture.Key
	copy(k[:], h.Sum(nil))
	return k, !fixture.OnCurve(k)
}

// suiteJSON returns the fixture file's contents.
func suiteJSON() ([]byte, error) {
	// Keys derive from this namespace and each name: local test identities only.
	f := fixture.New("find address")
	program := f.ProgramID()
	// The first authority whose record's canonical bump is at most 252, so the
	// on-chain search skips at least three on-curve addresses.
	name := ""
	for j := 0; name == ""; j++ {
		candidate := fmt.Sprintf("authority-%d", j)
		key := f.KeyFor(candidate)
		if _, bump, err := fixture.FindProgramAddress(program, []byte("record"), key[:]); err == nil && bump <= 252 {
			name = candidate
		}
	}
	authority := f.Signer(name, 1_000_000_000)
	record := f.PDA("record", []byte("record"), authority.Key[:])
	// The next valid bump below the canonical one: a PDA Create must refuse.
	var other fixture.Key
	otherBump := record.Bump - 1
	for ; ; otherBump-- {
		if k, ok := programAddress(program, otherBump, []byte("record"), authority.Key[:]); ok {
			other = k
			break
		}
	}
	noncanonical := f.AccountAt("noncanonical", other, nil)

	create := func(target fixture.Account, bump byte) fixture.Instruction {
		return client.CreateInstruction(client.CreateKeys{Record: target.Key, Authority: authority.Key}, model.CreateArgs{Bump: bump})
	}
	derive := func(args model.DeriveArgs) fixture.Instruction {
		return client.DeriveInstruction(client.DeriveKeys{Caller: authority.Key}, args)
	}
	// expected is find_program_address(seeds, program) as Derive returns it.
	expected := func(args model.DeriveArgs) []byte {
		seeds := [][]byte{args.Seed[:args.Length]}
		for j := byte(0); j < args.Extra; j++ {
			seeds = append(seeds, []byte{j})
		}
		address, bump, err := fixture.FindProgramAddress(program, seeds...)
		if err != nil {
			panic(err)
		}
		return append(address[:], bump)
	}

	f.Scenario(fmt.Sprintf("create-at-the-canonical-bump-%d", record.Bump)).
		Step(create(record, record.Bump)).
		ExpectCreated(record, client.EncodeAccountRecord(model.Record{Authority: authority.Key, Bump: record.Bump}), authority).
		ExpectInvocations(fixture.SystemProgram, 1)

	// The generated check accepts any valid bump; the handler's search refuses
	// one below the canonical bump, and the creation rolls back.
	f.Scenario("noncanonical-bump-fails").
		Step(create(noncanonical, otherBump)).Fails(2)

	// Derive returns the address and bump; its cost grows by one syscall per
	// skipped on-curve bump.
	seeds := []string{"a", "fees", "outbox", "dispatched_message", "igp"}
	// And the first "seed-N" whose canonical bump is at most 250.
	for j := 0; ; j++ {
		seed := fmt.Sprintf("seed-%d", j)
		if _, bump, err := fixture.FindProgramAddress(program, []byte(seed)); err == nil && bump <= 250 {
			seeds = append(seeds, seed)
			break
		}
	}
	for _, seed := range seeds {
		args := model.DeriveArgs{Length: byte(len(seed))}
		copy(args.Seed[:], seed)
		want := expected(args)
		f.Scenario(fmt.Sprintf("derive-%s-bump-%d", seed, want[32])).
			Step(derive(args)).ExpectReturn(want)
	}
	deep := model.DeriveArgs{Length: 32, Extra: 14}
	for j := range deep.Seed {
		deep.Seed[j] = byte(j)
	}
	f.Scenario("derive-fifteen-seeds").Step(derive(deep)).ExpectReturn(expected(deep))
	f.Scenario("derive-sixteen-seeds-fails").Step(derive(model.DeriveArgs{Length: 1, Extra: 15})).Fails(3001)

	return f.JSON()
}
