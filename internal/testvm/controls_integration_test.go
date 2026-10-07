package testvm

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"math"
	"math/big"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Explicit opt-in: ordinary root tests do not require a runner or SBF toolchain.
// scripts/svm_controls.py builds the real syscall probe and runs this test.
func TestSVMControlsIntegration(t *testing.T) {
	runner, probe := os.Getenv("GOSVM_CONTROLS_RUNNER"), os.Getenv("GOSVM_CONTROLS_ELF")
	if runner == "" || probe == "" {
		t.Skip("requires explicit controls runner and SBF probe")
	}
	elf, err := os.ReadFile(probe)
	if err != nil {
		t.Fatal(err)
	}
	features, err := Features()
	if err != nil {
		t.Fatal(err)
	}
	key := func(label string) []byte { v := sha256.Sum256([]byte(label)); return v[:] }
	payer := ed25519.NewKeyFromSeed(key("controls payer; never fund"))
	pool, program := key("controls state"), key("controls probe")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	s, err := Start(ctx, runner)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := Sysvars{Clock: &Clock{Slot: 9007199254740993, EpochStartTimestamp: -23, Epoch: 7, LeaderScheduleEpoch: 9, UnixTimestamp: -123456}, Rent: &Rent{777, 1.5, 17}}
	err = s.Init(map[string]any{"features": features, "payer": control58(payer.Public().(ed25519.PublicKey)), "sysvars": a,
		"programs": []any{map[string]any{"id": control58(program), "elf": base64.StdEncoding.EncodeToString(elf)}},
		"accounts": []any{map[string]any{"pubkey": control58(pool), "account": map[string]any{"data": []string{base64.StdEncoding.EncodeToString(make([]byte, 72)), "base64"}, "owner": control58(program), "lamports": 10000000}}}})
	if err != nil {
		t.Fatal(err)
	}
	call := func(method string, params any) json.RawMessage {
		t.Helper()
		result, err := s.Batch([]Call{{JSONRPC: "2.0", ID: 0, Method: method, Params: params}})
		if err != nil {
			t.Fatal(err)
		}
		return result[0]
	}
	account := func(address string) ([]byte, uint64) {
		t.Helper()
		var result struct {
			Value struct {
				Data     []string
				Lamports uint64
			}
		}
		if err := json.Unmarshal(call("getAccountInfo", []any{address}), &result); err != nil {
			t.Fatal(err)
		}
		data, err := base64.StdEncoding.DecodeString(result.Value.Data[0])
		if err != nil {
			t.Fatal(err)
		}
		return data, result.Value.Lamports
	}
	blockhash := func() string {
		var result struct{ Value struct{ Blockhash string } }
		if err := json.Unmarshal(call("getLatestBlockhash", []any{}), &result); err != nil {
			t.Fatal(err)
		}
		return result.Value.Blockhash
	}
	status := func(signature string) string {
		var result struct{ Value []json.RawMessage }
		if err := json.Unmarshal(call("getSignatureStatuses", []any{[]string{signature}}), &result); err != nil {
			t.Fatal(err)
		}
		return string(result.Value[0])
	}
	checkSysvars := func(want Sysvars) {
		t.Helper()
		got, err := s.Sysvars()
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("sysvars: got %+v, err %v", got, err)
		}
		// Compare serialized sysvar accounts separately from the cached syscall view.
		clock, _ := account("SysvarC1ock11111111111111111111111111111111")
		if !bytes.Equal(clock, controlState(want, 0)[:40]) {
			t.Fatal("clock account bytes differ")
		}
		rent, _ := account("SysvarRent111111111111111111111111111111111")
		wantRent := controlState(want, 0)[40:57]
		if !bytes.Equal(rent, wantRent) {
			t.Fatalf("rent account bytes differ: %x / %x", rent, wantRent)
		}
	}
	checkSysvars(a)
	initialHash := blockhash()
	_, initialBalance := account(control58(payer.Public().(ed25519.PublicKey)))
	makeTx := func(fail byte) (string, string) {
		msg := []byte{1, 0, 1, 3}
		msg = append(msg, payer.Public().(ed25519.PublicKey)...)
		msg = append(msg, pool...)
		msg = append(msg, program...)
		msg = append(msg, controlUn58(initialHash)...)
		msg = append(msg, 1, 2, 1, 1, 1, fail)
		sig := ed25519.Sign(payer, msg)
		wire := append([]byte{1}, sig...)
		wire = append(wire, msg...)
		return base64.StdEncoding.EncodeToString(wire), control58(sig)
	}
	good, goodSig := makeTx(0)
	bad, badSig := makeTx(1)
	simulate := func(tx string, errorText string, expected []byte) uint64 {
		t.Helper()
		var result struct {
			Value struct {
				Err           json.RawMessage
				UnitsConsumed uint64
				Accounts      []struct{ Data []string }
			}
		}
		if err := json.Unmarshal(call("simulateTransaction", []any{tx, map[string]any{"encoding": "base64", "sigVerify": true, "accounts": map[string]any{"addresses": []string{control58(pool), "SysvarC1ock11111111111111111111111111111111"}}}}), &result); err != nil {
			t.Fatal(err)
		}
		if string(result.Value.Err) != errorText {
			t.Fatalf("simulation error %s, want %s", result.Value.Err, errorText)
		}
		if expected != nil {
			if len(result.Value.Accounts) != 2 || len(result.Value.Accounts[0].Data) != 2 {
				t.Fatal("missing simulated data")
			}
			data, err := base64.StdEncoding.DecodeString(result.Value.Accounts[0].Data[0])
			if err != nil || !bytes.Equal(data, expected) {
				t.Fatalf("probe output mismatch: %x", data)
			}
			if len(result.Value.Accounts[1].Data) != 2 {
				t.Fatal("missing unchanged account outside transaction")
			}
			clock, err := base64.StdEncoding.DecodeString(result.Value.Accounts[1].Data[0])
			if err != nil || !bytes.Equal(clock, expected[:40]) {
				t.Fatal("unchanged clock account snapshot differs")
			}
		}
		return result.Value.UnitsConsumed
	}
	send := func(tx, signature, errorText string) {
		t.Helper()
		call("sendTransaction", []any{tx, map[string]any{"encoding": "base64", "skipPreflight": true}})
		var result struct{ Err json.RawMessage }
		if err := json.Unmarshal([]byte(status(signature)), &result); err != nil || string(result.Err) != errorText {
			t.Fatalf("submitted status %s, want %s", status(signature), errorText)
		}
	}
	for i := 0; i < 2; i++ {
		if err := s.Reset(); err != nil {
			t.Fatal(err)
		}
		cu := simulate(good, "null", controlState(a, 1))
		if cu == 0 {
			t.Fatal("no SBF compute")
		}
		send(good, goodSig, "null")
		data, balance := account(control58(pool))
		if !bytes.Equal(data, controlState(a, 1)) || balance != 10000000 {
			t.Fatal("probe committed incorrect state")
		}
		_, balance = account(control58(payer.Public().(ed25519.PublicKey)))
		if balance >= initialBalance {
			t.Fatal("fees did not change payer")
		}
		// LiteSVM simulations do not consult submission history; duplicate
		// detection is checked by resubmitting the signed transaction.
		send(good, goodSig, `"AlreadyProcessed"`)
		if err := s.ExpireBlockhash(); err != nil {
			t.Fatal(err)
		}
		if blockhash() == initialHash {
			t.Fatal("blockhash did not expire")
		}
		simulate(good, `"BlockhashNotFound"`, nil)
		if err := s.Reset(); err != nil {
			t.Fatal(err)
		}
		if blockhash() != initialHash || status(goodSig) != "null" {
			t.Fatal("reset did not restore blockhash/statuses")
		}
		_, balance = account(control58(payer.Public().(ed25519.PublicKey)))
		if balance != initialBalance {
			t.Fatal("reset did not restore fees")
		}
		data, _ = account(control58(pool))
		if !bytes.Equal(data, make([]byte, 72)) {
			t.Fatal("reset did not restore state")
		}
		t.Logf("initial checkpoint replay %d: %d CU, identical signature succeeds after reset", i+1, cu)
	}
	// Capture a non-genesis state, including successful transaction history/status.
	send(good, goodSig, "null")
	_, paidBalance := account(control58(payer.Public().(ed25519.PublicKey)))
	if err := s.Snapshot(); err != nil {
		t.Fatal(err)
	}
	b := Sysvars{Clock: &Clock{Slot: 42, UnixTimestamp: 876543, Epoch: 11, EpochStartTimestamp: 765432, LeaderScheduleEpoch: 12}, Rent: &Rent{999, 2.5, 19}}
	if err := s.SetSysvars(b); err != nil {
		t.Fatal(err)
	}
	checkSysvars(b)
	// Observe updated sysvars through real SBF syscall output with a fresh signature.
	cu := simulate(bad, `{"InstructionError":[0,{"Custom":9}]}`, nil)
	if cu == 0 {
		t.Fatal("failed probe did not execute")
	}
	send(bad, badSig, `{"InstructionError":[0,{"Custom":9}]}`)
	data, _ := account(control58(pool))
	if !bytes.Equal(data, controlState(a, 1)) {
		t.Fatal("failed mutation did not roll back")
	}
	// An unused transaction avoids the restored duplicate signature. The probe
	// treats any value other than 1 as success.
	changed, changedSig := makeTx(2)
	simulate(changed, "null", controlState(b, 2))
	send(changed, changedSig, "null")
	for i := 0; i < 2; i++ {
		if err := s.Reset(); err != nil {
			t.Fatal(err)
		}
		checkSysvars(a)
		data, _ := account(control58(pool))
		if !bytes.Equal(data, controlState(a, 1)) {
			t.Fatal("snapshot state not restored")
		}
		_, balance := account(control58(payer.Public().(ed25519.PublicKey)))
		if balance != paidBalance {
			t.Fatal("snapshot payer not restored")
		}
		if !strings.Contains(status(goodSig), `"err":null`) || status(badSig) != "null" || status(changedSig) != "null" {
			t.Fatal("snapshot statuses not restored")
		}
		send(good, goodSig, `"AlreadyProcessed"`)
		simulate(changed, "null", controlState(a, 2))
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	t.Log("PASS: typed sysvars/account/cache views, successful and failed SBF mutations, fees, statuses, history, blockhash, reusable snapshots")
}

func controlState(v Sysvars, counter uint64) []byte {
	values := []uint64{v.Clock.Slot, uint64(v.Clock.EpochStartTimestamp), v.Clock.Epoch, v.Clock.LeaderScheduleEpoch, uint64(v.Clock.UnixTimestamp), v.Rent.LamportsPerByteYear, math.Float64bits(v.Rent.ExemptionThreshold), uint64(v.Rent.BurnPercent), counter}
	data := make([]byte, 72)
	for i, value := range values {
		binary.LittleEndian.PutUint64(data[i*8:], value)
	}
	return data
}

const controlAlphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

func control58(data []byte) string {
	n := new(big.Int).SetBytes(data)
	base := big.NewInt(58)
	var out []byte
	for n.Sign() > 0 {
		r := new(big.Int)
		n.QuoRem(n, base, r)
		out = append(out, controlAlphabet[r.Int64()])
	}
	for _, b := range data {
		if b != 0 {
			break
		}
		out = append(out, '1')
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return string(out)
}
func controlUn58(value string) []byte {
	n := new(big.Int)
	for _, b := range value {
		n.Mul(n, big.NewInt(58))
		n.Add(n, big.NewInt(int64(strings.IndexRune(controlAlphabet, b))))
	}
	data := n.Bytes()
	for _, b := range value {
		if b != '1' {
			break
		}
		data = append([]byte{0}, data...)
	}
	return data
}
