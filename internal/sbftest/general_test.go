package sbftest

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testGeneralSuite() FixtureSuite {
	owner := b58(key("fixture program"))
	state := FixtureState{Data: "00", Owner: owner, Lamports: 10000000}
	return FixtureSuite{Format: FixtureFormat, ProgramID: owner, PayerSeed: hex.EncodeToString(key("fixture payer")),
		Signers:  []FixtureSigner{{Name: "user", Seed: hex.EncodeToString(key("fixture user"))}},
		Accounts: []FixtureAccount{{Name: "pool", Address: b58(key("fixture pool")), Initial: &state}},
		Cases:    []FixtureCase{{Name: "multi-account", Steps: []FixtureStep{{Instructions: []FixtureInstruction{{Program: "program", Accounts: []FixtureMeta{{Account: "user", Signer: true}, {Account: "pool"}, {Account: "pool", Writable: true}}, Data: "abcd"}}, Expect: FixtureExpectation{Error: json.RawMessage("null"), Accounts: []FixtureCheck{{Account: "pool", State: &state}}}}}}}}
}
func TestFixtureTransactionMergesKeysAndPreservesReferences(t *testing.T) {
	suite := testGeneralSuite()
	k, e := suite.validate()
	if e != nil {
		t.Fatal(e)
	}
	encoded, signature, e := fixtureTransaction(k, make([]byte, 32), suite.Cases[0].Steps[0].Instructions)
	if e != nil {
		t.Fatal(e)
	}
	wire, e := base64.StdEncoding.DecodeString(encoded)
	if e != nil {
		t.Fatal(e)
	}
	// Independently construct the expected legacy message: payer + readonly
	// signer + writable pool + readonly program, with duplicate pool indices.
	expected := []byte{2, 1, 1, 4}
	for _, name := range []string{"payer", "user", "pool", "program"} {
		expected = append(expected, k.addresses[name]...)
	}
	expected = append(expected, make([]byte, 32)...)
	expected = append(expected, 1, 3, 3, 1, 2, 2, 2, 0xab, 0xcd)
	if wire[0] != 2 || !bytes.Equal(wire[129:], expected) {
		t.Fatalf("message ordering/privileges differ: %x", wire[129:])
	}
	if signature != b58(wire[1:65]) {
		t.Fatal("fee payer signature differs")
	}
	for i, name := range []string{"payer", "user"} {
		if !ed25519.Verify(k.addresses[name], expected, wire[1+i*64:65+i*64]) {
			t.Fatalf("invalid signature for %s", name)
		}
	}
}
func TestGeneralFixturesRejectAmbiguousAndUncheckedInputs(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*FixtureSuite)
	}{
		{"missing-signer", func(s *FixtureSuite) { s.Signers = nil }},
		{"unknown-account", func(s *FixtureSuite) { s.Cases[0].Steps[0].Instructions[0].Accounts[0].Account = "unknown" }},
		{"unchecked-writable", func(s *FixtureSuite) {
			s.Cases[0].Steps[0].Expect.Accounts = []FixtureCheck{{Account: "payer", Absent: true}}
		}},
		{"duplicate-alias", func(s *FixtureSuite) { s.Accounts = append(s.Accounts, s.Accounts[0]) }},
		{"missing-error", func(s *FixtureSuite) { s.Cases[0].Steps[0].Expect.Error = nil }},
		{"state-and-absent", func(s *FixtureSuite) { s.Cases[0].Steps[0].Expect.Accounts[0].Absent = true }},
		{"bad-seed", func(s *FixtureSuite) { s.PayerSeed = "01" }},
		{"bad-address", func(s *FixtureSuite) { s.ProgramID = "not-base58!" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := testGeneralSuite()
			test.change(&s)
			if _, e := s.validate(); e == nil {
				t.Fatal("invalid fixture accepted")
			}
		})
	}
	data, _ := json.Marshal(testGeneralSuite())
	path := filepath.Join(t.TempDir(), "fixtures.json")
	data = bytes.Replace(data, []byte(`"format":`), []byte(`"unknown":true,"format":`), 1)
	if e := os.WriteFile(path, data, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := LoadGeneral(path); e == nil {
		t.Fatal("unknown field accepted")
	}
}
func TestGeneralReportsAreClearedWithoutOverwritingInputs(t *testing.T) {
	dir := t.TempDir()
	report := filepath.Join(dir, "results.json")
	old := []byte(`{"schema":1,"runtime_engine":"litesvm","cases":[]}`)
	if e := os.WriteFile(report, old, 0600); e != nil {
		t.Fatal(e)
	}
	if e := RunGeneral(context.Background(), "absent.so", "absent.json", report, io.Discard, FastOptions{}); e == nil {
		t.Fatal("missing fixtures accepted")
	}
	if _, e := os.Stat(report); !os.IsNotExist(e) {
		t.Fatal("stale report retained")
	}
	if e := os.WriteFile(report, old, 0600); e != nil {
		t.Fatal(e)
	}
	if e := clearGeneralReport(report, report); e == nil {
		t.Fatal("input overwritten")
	}
	alias := filepath.Join(dir, "same.json")
	if e := os.Link(report, alias); e != nil {
		t.Fatal(e)
	}
	if e := clearGeneralReport(report, alias); e == nil {
		t.Fatal("hard-linked input overwritten")
	}
	if e := os.WriteFile(report, []byte("user file"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := clearGeneralReport(report); e == nil {
		t.Fatal("unrelated file overwritten")
	}
}
func TestGeneralChecksMetadataAndExactErrorCategories(t *testing.T) {
	state := *testGeneralSuite().Accounts[0].Initial
	got := state.account()
	got.Lamports--
	if e := fixtureAccount("swap", "submitted", FixtureCheck{Account: "pool", State: &state}, &got); e == nil || !strings.Contains(e.Error(), "metadata") {
		t.Fatal("lamport difference missed")
	}
	if e := fixtureError(json.RawMessage(`{"InstructionError":[0,"InvalidAccountData"]}`), json.RawMessage(`{"InstructionError":[0,{"Custom":1}]}`)); e == nil {
		t.Fatal("builtin/custom category mismatch missed")
	}
	if e := fixtureError(json.RawMessage(`{"InstructionError":[1,{"Custom":17}]}`), json.RawMessage(`{"InstructionError":[0,{"Custom":17}]}`)); e == nil {
		t.Fatal("instruction error index mismatch missed")
	}
}

func TestNativeProgramAliasesAndDeclaredTransactions(t *testing.T) {
	s := testGeneralSuite()
	s.NativePrograms = []FixtureNativeProgram{{Name: "system", Address: "11111111111111111111111111111111"}}
	s.Cases[0].Steps[0].Instructions[0].Program = "system"
	hash := b58(make([]byte, 32))
	if _, _, e := s.Transaction(hash, s.Cases[0].Steps[0].Instructions); e != nil {
		t.Fatal(e)
	}
	if _, _, e := s.Transaction(hash, []FixtureInstruction{{Program: "unknown"}}); e == nil {
		t.Fatal("undeclared transaction accepted")
	}
	s.NativePrograms[0].Address = s.ProgramID
	if _, e := s.validate(); e == nil {
		t.Fatal("arbitrary native program accepted")
	}
	s.NativePrograms[0].Address = "11111111111111111111111111111111"
	s.NativePrograms = append(s.NativePrograms, FixtureNativeProgram{Name: "other", Address: s.NativePrograms[0].Address})
	if _, e := s.validate(); e == nil {
		t.Fatal("duplicate native address accepted")
	}
}

func TestSimulationStateOverridesKeepCommittedChecks(t *testing.T) {
	s := testGeneralSuite()
	expect := &s.Cases[0].Steps[0].Expect
	shared := expect.Accounts[0]
	expect.SimulationAccounts = []FixtureCheck{{Account: "pool", Absent: true}}
	if _, e := s.validate(); e != nil {
		t.Fatal(e)
	}
	if !expect.SimulationCheck(shared).Absent || expect.Accounts[0].State == nil {
		t.Fatal("phase expectations conflated")
	}
	expect.SimulationAccounts[0].Account = "user"
	if _, e := s.validate(); e == nil {
		t.Fatal("unwatched simulation override accepted")
	}
	expect.SimulationAccounts[0].Account = "pool"
	expect.SimulationAccounts = append(expect.SimulationAccounts, expect.SimulationAccounts[0])
	if _, e := s.validate(); e == nil {
		t.Fatal("duplicate simulation override accepted")
	}
	expect.SimulationAccounts = expect.SimulationAccounts[:1]
	expect.Error = json.RawMessage(`{"InstructionError":[0,"InvalidAccountData"]}`)
	if _, e := s.validate(); e == nil {
		t.Fatal("unchecked failed simulation state accepted")
	}
}

func TestGeneralSysvarControlsValidateBeforeAnySelectedScenario(t *testing.T) {
	clock := json.RawMessage(`{"clock":{"slot":9007199254740993,"epoch_start_timestamp":-23,"epoch":7,"leader_schedule_epoch":9,"unix_timestamp":-123456}}`)
	s := testGeneralSuite()
	s.Sysvars = clock
	s.Cases[0].Steps[0].Sysvars = clock
	if _, err := s.validate(); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`null`, `{}`, `{"clock":{"slot":1}}`, `{"rent":{"lamports_per_byte_year":1,"exemption_threshold":2,"burn_percent":101}}`} {
		s.Cases[0].Steps[0].Sysvars = json.RawMessage(raw)
		if _, err := s.validate(); err == nil {
			t.Fatalf("accepted step sysvars %s", raw)
		}
	}
	// An invalid unselected case cannot be skipped or leave an earlier passing
	// report. Reject the suite before trying to load an ELF or launch a runner.
	s.Cases[0].Steps[0].Sysvars = nil
	bad := s.Cases[0]
	bad.Name = "unselected"
	bad.Steps = append([]FixtureStep(nil), bad.Steps...)
	bad.Steps[0].Sysvars = json.RawMessage(`{"clock":{"slot":1}}`)
	s.Cases = append(s.Cases, bad)
	dir := t.TempDir()
	fixture := filepath.Join(dir, "fixtures.json")
	data, _ := json.Marshal(s)
	if err := os.WriteFile(fixture, data, 0600); err != nil {
		t.Fatal(err)
	}
	report := filepath.Join(dir, "report.json")
	if err := os.WriteFile(report, []byte(`{"schema":1,"runtime_engine":"litesvm","cases":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	err := RunGeneral(context.Background(), "absent.so", fixture, report, io.Discard, FastOptions{Pattern: "^multi-account$"})
	if err == nil || !strings.Contains(err.Error(), "sysvars") {
		t.Fatalf("unselected malformed controls escaped validation: %v", err)
	}
	if _, err := os.Stat(report); !os.IsNotExist(err) {
		t.Fatal("malformed controls retained passing report")
	}
}
