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
