package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gosvm/internal/network"
)

// captureCall runs gosvm call with stdout captured. No test reaches a network:
// each uses -list, -dry-run or fails before any RPC.
func captureCall(t *testing.T, args ...string) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = w
	done := make(chan []byte)
	go func() { b, _ := io.ReadAll(r); done <- b }()
	err = callCommand(args)
	w.Close()
	os.Stdout = stdout
	return string(<-done), err
}

// testKeypair writes a fresh keypair file in a temporary directory.
func testKeypair(t *testing.T) (string, ed25519.PrivateKey) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "payer.json")
	key, _, err := network.EnsureKeypair(path)
	if err != nil {
		t.Fatal(err)
	}
	return path, key
}

const tutorialProgram = "A4bd41Uazko5AD5syV6owBKns4v9HdFuCYPW2U5w5Ari"

func TestCallList(t *testing.T) {
	out, err := captureCall(t, "-dir", "../../examples/tutorial")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Create -limit uint64 [-bump byte]", "Increment -amount uint64", "Preview -count uint64 -limit uint64 -amount uint64"} {
		if !strings.Contains(out, want) {
			t.Errorf("listing lacks %q:\n%s", want, out)
		}
	}
	if again, err := captureCall(t, "-list", "-dir", "../../examples/tutorial"); err != nil || again != out {
		t.Errorf("-list differs: %v", err)
	}
}

func TestCallDryRun(t *testing.T) {
	keypair, key := testKeypair(t)
	base := []string{"-dir", "../../examples/tutorial", "-program", tutorialProgram, "-keypair", keypair}
	// Call flags may follow the instruction's arguments.
	out, err := captureCall(t, append(base, "Increment", "-amount=3", "-dry-run", "-json")...)
	if err != nil {
		t.Fatal(err)
	}
	var plan struct {
		Instruction string            `json:"instruction"`
		Args        map[string]string `json:"args"`
		InputHex    string            `json:"input_hex"`
		Accounts    []struct {
			Name, Address, Source string
			Signer, Writable      bool
		} `json:"accounts"`
	}
	if err = json.Unmarshal([]byte(out), &plan); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	payer := network.Address(key)
	if plan.Instruction != "Increment" || plan.Args["amount"] != "3" || plan.InputHex != "7a818aa208ac54d40300000000000000" || len(plan.Accounts) != 2 ||
		!plan.Accounts[0].Writable || plan.Accounts[1].Address != payer || !plan.Accounts[1].Signer || plan.Accounts[1].Source != "fee payer (-keypair)" {
		t.Fatalf("%+v", plan)
	}
	text, err := captureCall(t, append(base, "-dry-run", "Create", "-limit", "10")...)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Create on no network (dry run)", "  canonical bump of Counter\n", "System   readonly         11111111111111111111111111111111              System program"} {
		if !strings.Contains(text, want) {
			t.Errorf("dry run lacks %q:\n%s", want, text)
		}
	}
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"-dry-run", "Increment"}, "Increment needs -amount uint64"},
		{[]string{"-dry-run", "Increment", "-amount", "1", "-limit", "3"}, "Increment has no argument -limit; expected: -amount uint64"},
		{[]string{"-dry-run", "Increment", "-amount"}, "-amount needs a value"},
		{[]string{"-dry-run", "Increment", "-amount", "1", "extra"}, `unexpected argument "extra"`},
		{[]string{"-dry-run", "Increment", "-amount", "1", "-account", "Owner"}, "named instructions take Name=value"},
		{[]string{"-dry-run", "Increment", "-amount", "1", "-account", "Counter=nope"}, "want a base58 address, payer or program"},
		{[]string{"-dry-run", "-data", "00", "Increment"}, "give an instruction name or -data, not both"},
		{[]string{"-send", "-dry-run", "Preview", "-count", "1", "-limit", "2", "-amount", "1"}, "choose -send or -dry-run"},
		{[]string{"Preview", "-count", "1", "-limit", "2", "-amount", "1"}, "pass -network"},
	} {
		if _, err := captureCall(t, append(base, tc.args...)...); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v: got %v, want %q", tc.args, err, tc.want)
		}
	}
}

// Raw -data calls keep the payer as the only account unless -account/-signer
// list the accounts explicitly, in order.
func TestCallRawAccounts(t *testing.T) {
	keypair, key := testKeypair(t)
	other, otherKey := testKeypair(t)
	base := []string{"-program", tutorialProgram, "-keypair", keypair, "-dry-run", "-data", "f52cd645"}
	decode := func(out string) []map[string]any {
		var m struct {
			Accounts []map[string]any `json:"accounts"`
		}
		if err := json.Unmarshal([]byte(out), &m); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		return m.Accounts
	}
	out, err := captureCall(t, base...)
	if err != nil {
		t.Fatal(err)
	}
	if a := decode(out); len(a) != 1 || a[0]["address"] != network.Address(key) || a[0]["signer"] != true {
		t.Fatalf("default raw accounts %v", a)
	}
	out, err = captureCall(t, append(base, "-account", "11111111111111111111111111111111:w", "-signer", other, "-account", "payer:s", "-account", "program")...)
	if err != nil {
		t.Fatal(err)
	}
	want := []map[string]any{
		{"address": "11111111111111111111111111111111", "signer": false, "writable": true},
		{"address": network.Address(otherKey), "signer": true, "writable": false},
		{"address": network.Address(key), "signer": true, "writable": false},
		{"address": tutorialProgram, "signer": false, "writable": false},
	}
	if got, _ := json.Marshal(decode(out)); !bytes.Equal(got, mustJSON(t, want)) {
		t.Fatalf("raw accounts %s", got)
	}
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"-account", "payer:x"}, "flags are :w (writable), :s (signer) or :ws"},
		{[]string{"-account", "Owner=payer"}, "Name=value accounts need an instruction name"},
		{[]string{"-seed", "A.B=1"}, "-seed applies to named instructions"},
	} {
		if _, err := captureCall(t, append(base, tc.args...)...); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v: got %v, want %q", tc.args, err, tc.want)
		}
	}
}

func mustJSON(t *testing.T, v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
