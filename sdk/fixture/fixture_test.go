package fixture

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gosvm/internal/sbftest"
)

func TestBase58Keys(t *testing.T) {
	if Base58(SystemProgram[:]) != "11111111111111111111111111111111" {
		t.Fatal(Base58(SystemProgram[:]))
	}
	for _, s := range []string{"ComputeBudget111111111111111111111111111111", "SysvarC1ock11111111111111111111111111111111", "A4bd41Uazko5AD5syV6owBKns4v9HdFuCYPW2U5w5Ari"} {
		k, err := ParseKey(s)
		if err != nil || Base58(k[:]) != s {
			t.Fatal(s, err)
		}
	}
	for _, bad := range []string{"0OIl", "1111", ""} {
		if _, err := ParseKey(bad); err == nil {
			t.Fatal("accepted", bad)
		}
	}
}

// Vectors from an independent implementation (filippo.io/edwards25519).
func TestFindProgramAddressVectors(t *testing.T) {
	program := Key(sha256.Sum256([]byte("vector program")))
	if Base58(program[:]) != "8K85Gam2VSE8bwGT514bjHt93xT6BifymZqJBpQM76KY" {
		t.Fatal(Base58(program[:]))
	}
	for _, v := range []struct {
		seed, key string
		bump      byte
	}{
		{"seed-14", "CHoJwbTi1r4JzSDovg6sqToXWyABc61GDjcfE9MmDzAn", 253},
		{"seed-16", "GY9cEgGwafCNiAC2FzcWoDdthN956tEkGYKU9be1m9YH", 253},
		{"seed-19", "FauvxejasXYYKr9Ugftoqt9RZhyww3MtuHxEai1vNw8R", 252},
		{"seed-24", "7Ra2CFbBKy1GC22pbDuNvoVaNEfv6xobYrYP6JBi4C1Q", 253},
	} {
		k, bump, err := FindProgramAddress(program, []byte(v.seed))
		if err != nil || Base58(k[:]) != v.key || bump != v.bump {
			t.Fatalf("%s: %s %d %v", v.seed, Base58(k[:]), bump, err)
		}
	}
	if _, _, err := FindProgramAddress(program, make([]byte, 33)); err == nil {
		t.Fatal("accepted a 33-byte seed")
	}
}

func TestOnCurve(t *testing.T) {
	for _, h := range []string{"ecca0d03e45b7d66fc6006f616843b68b54de2f7b3b982a0e2162a8ec09d9e8f", "4e91ab46b2b3ffbec715a8157c23b64c990f031eb4f9627faacf1c916b3f80aa", "d923af25bb2eed8818c3f14084ef50aefbd2d0820d1c50ff6ec737d4acefa78d", "39345548d3284aa0b6e121a144fe2814f88a9c747048b048c3ece98f771efedc"} {
		if !OnCurve(hexKey(t, h)) {
			t.Fatal("off curve:", h)
		}
	}
	for _, h := range []string{"4151b6fec9f94505ab4030c2d43b8c995cb690d568037cd9e7615f44f60d4fc5", "c9d3e67c5d843cecc2a613c2269c7417b65fad5817c9d82b66997b3b5b2cbeb5", "282cb89e2e7265a835d9bf5d5cf7ebfbe5e925554416745abdae24a60c9d56b5", "872eb4c82ca96a4c1aeb182388bc48c5cbab1bb92d42c65615afb7f83019a8bd"} {
		if OnCurve(hexKey(t, h)) {
			t.Fatal("on curve:", h)
		}
	}
	for i := 0; i < 64; i++ {
		seed := sha256.Sum256([]byte{byte(i)})
		var k Key
		copy(k[:], ed25519.NewKeyFromSeed(seed[:]).Public().(ed25519.PublicKey))
		if !OnCurve(k) {
			t.Fatal("public key off curve")
		}
	}
}

func hexKey(t *testing.T, s string) Key {
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 32 {
		t.Fatal(s)
	}
	return Key(b)
}

func TestRent(t *testing.T) {
	if got := DefaultRent.MinimumBalance(57); got != 1_287_600 {
		t.Fatal(got)
	}
	if got := (Rent{LamportsPerByteYear: 1, ExemptionThreshold: 0.5}).MinimumBalance(1); got != 64 {
		t.Fatal(got)
	}
}

// A suite using every feature, checked by the runner's own fixture loader.
func TestSuiteLoadsInRunnerValidation(t *testing.T) {
	f := New("unit")
	owner := f.Signer("owner", 1_000_000_000)
	other := f.Signer("other", 0)
	state := f.PDA("state", []byte("state"), owner.Key[:])
	ledger := f.Account("ledger", f.Owned([]byte{1, 2, 3}))
	external := f.Program("external", f.KeyFor("external"), "external.so", strings.Repeat("ab", 32))
	ix := func(data byte, metas ...AccountMeta) Instruction {
		return Instruction{Accounts: metas, Data: []byte{data}}
	}
	create := ix(1, AccountMeta{Key: state.Key, Writable: true}, AccountMeta{Key: owner.Key, Signer: true, Writable: true}, AccountMeta{Key: SystemProgram}, AccountMeta{Key: ClockSysvar})

	s := f.Scenario("lifecycle")
	s.Step(create).ExpectCreated(state, []byte{9, 9}, owner).ExpectInvocations(SystemProgram, 1).CU(1000)
	s.Step(ix(2, AccountMeta{Key: state.Key, Writable: true}, AccountMeta{Key: owner.Key, Signer: true, Writable: true})).
		ExpectClosed(state, owner).FreshBlockhash()
	s.Step(ix(3, AccountMeta{Key: ledger.Key, Writable: true})).Fails(7)
	s.Step(create).Invoke(ComputeBudgetProgram, RequestHeapFrame(64*1024)).Invoke(external.Key, ix(4)).
		Clock(Clock{Slot: 5, UnixTimestamp: 6}).Rent(Rent{LamportsPerByteYear: 1, ExemptionThreshold: 1}).
		ExpectCreated(state, []byte{8}, owner).ExpectReturn([]byte{1})

	f.Scenario("override").Override(ledger, Wallet(5)).
		Step(ix(5, AccountMeta{Key: ledger.Key, Writable: true}, AccountMeta{Key: f.Payer().Key, Signer: true, Writable: true})).
		ExpectLamports(ledger, 4).ExpectLog("hello", 2)
	f.Scenario("raw-error").Step(ix(6), ix(7)).FailsWith(InstructionError(1, "MissingRequiredSignature"))
	_ = other

	b, err := f.JSON()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "svm.json")
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	suite, err := sbftest.LoadGeneral(path)
	if err != nil {
		t.Fatalf("runner rejected the fixture: %v\n%s", err, b)
	}
	if len(suite.NativePrograms) != 2 || suite.NativePrograms[0].Name != "system" || suite.NativePrograms[1].Name != "compute-budget" {
		t.Fatalf("native programs: %+v", suite.NativePrograms)
	}
	if n := len(suite.Accounts); n != 5 || suite.Accounts[4].Name != "clock-sysvar" || suite.Accounts[1].Initial != nil {
		t.Fatalf("accounts: %+v", suite.Accounts)
	}
	steps := suite.Cases[0].Steps
	rent := DefaultRent.MinimumBalance(2)
	if got := steps[0].Expect.Accounts[0].State.Lamports; got != 1_000_000_000-rent {
		t.Fatal("payer not debited", got)
	}
	if st := steps[0].Expect.Accounts[2].State; st == nil || st.Data != "0909" || st.Lamports != rent || st.Owner != Base58(f.program[:]) {
		t.Fatalf("created: %+v", st)
	}
	// Closing refunds the owner, and simulation shows the closed record.
	if steps[1].Expect.Accounts[0].State.Lamports != 1_000_000_000 || !steps[1].Expect.Accounts[2].Absent || len(steps[1].Expect.SimulationAccounts) != 1 || !steps[1].FreshBlockhash {
		t.Fatalf("closed: %+v", steps[1])
	}
	// A failure keeps every state and expects the instruction's custom code.
	var failure any
	json.Unmarshal(steps[2].Expect.Error, &failure)
	if b, _ := json.Marshal(failure); string(b) != `{"InstructionError":[0,{"Custom":7}]}` {
		t.Fatal(string(b))
	}
	if steps[2].Expect.Accounts[0].State.Lamports != 1_000_000_000 || !steps[2].Expect.Accounts[2].Absent {
		t.Fatal("failure changed state")
	}
	// The step's Rent applies to the account it creates.
	if st := steps[3].Expect.Accounts[2].State; st.Lamports != 129 || len(steps[3].Instructions) != 3 || steps[3].Instructions[2].Program != "external" {
		t.Fatalf("rent/invoke: %+v", steps[3])
	}
	if steps[3].Expect.LogCounts["Program return: "+Base58(f.program[:])+" AQ=="] != 1 {
		t.Fatal(steps[3].Expect.LogCounts)
	}
	if suite.Cases[1].Overrides[0].State.Lamports != 5 || suite.Cases[1].Steps[0].Expect.Accounts[3].State.Lamports != 4 {
		t.Fatal("override")
	}
}

func TestBuildErrors(t *testing.T) {
	for name, build := range map[string]func(f *Fixture){
		"undeclared": func(f *Fixture) {
			f.Scenario("s").Step(Instruction{Accounts: []AccountMeta{{Key: f.KeyFor("x")}}})
		},
		"unsigned seed": func(f *Fixture) {
			a := f.Account("a", Wallet(1))
			f.Scenario("s").Step(Instruction{Accounts: []AccountMeta{{Key: a.Key, Signer: true}}})
		},
		"failing with changes": func(f *Fixture) {
			a := f.Account("a", Wallet(1))
			f.Scenario("s").Step(Instruction{}).Fails(1).ExpectLamports(a, 2)
		},
		"ambiguous failure": func(f *Fixture) {
			f.Account("a", Wallet(1))
			f.Scenario("s").Step(Instruction{}, Instruction{}).Fails(1)
		},
		"duplicate alias": func(f *Fixture) {
			f.Signer("a", 1)
			f.Signer("a", 1)
		},
		"builtin address": func(f *Fixture) { f.AccountAt("sys", SystemProgram, nil) },
		"missing data": func(f *Fixture) {
			a := f.AccountAt("a", f.KeyFor("a"), nil)
			f.Scenario("s").Step(Instruction{}).ExpectData(a, []byte{1})
		},
		"created twice": func(f *Fixture) {
			p := f.Signer("p", 1_000_000_000)
			a := f.Account("a", Wallet(1))
			f.Scenario("s").Step(Instruction{}).ExpectCreated(a, nil, p)
		},
		"duplicate scenario": func(f *Fixture) {
			f.Account("a", Wallet(1))
			f.Scenario("s").Step(Instruction{})
			f.Scenario("s").Step(Instruction{})
		},
		"no scenarios": func(f *Fixture) { f.Account("a", Wallet(1)) },
		"late program ID": func(f *Fixture) {
			f.PDA("p", []byte("x"))
			f.SetProgramID(Key{1})
		},
	} {
		f := New("errors")
		build(f)
		if _, err := f.JSON(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestDeterministicKeys(t *testing.T) {
	a, b := New("same"), New("same")
	if a.ProgramID() != b.ProgramID() || a.KeyFor("x") != b.KeyFor("x") || a.Payer() != b.Payer() || New("other").ProgramID() == a.ProgramID() {
		t.Fatal("keys are not derived from namespace and name")
	}
}
