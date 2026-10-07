// Package sbftest runs local-only, byte-exact SBF integration fixtures.
package sbftest

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gosvm/internal/testvm"
)

const alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

func b58(b []byte) string {
	n := new(big.Int).SetBytes(b)
	base := big.NewInt(58)
	rem := new(big.Int)
	out := []byte{}
	for n.Sign() > 0 {
		n.QuoRem(n, base, rem)
		out = append(out, alphabet[rem.Int64()])
	}
	for _, v := range b {
		if v != 0 {
			break
		}
		out = append(out, '1')
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return string(out)
}
func un58(s string) []byte {
	n := new(big.Int)
	for _, c := range []byte(s) {
		i := bytes.IndexByte([]byte(alphabet), c)
		if i < 0 {
			panic("bad base58")
		}
		n.Mul(n, big.NewInt(58))
		n.Add(n, big.NewInt(int64(i)))
	}
	b := n.Bytes()
	for i := 0; i < len(s) && s[i] == '1'; i++ {
		b = append([]byte{0}, b...)
	}
	return b
}

type rpcClient struct {
	ctx    context.Context
	url    string
	client http.Client
}

func (c *rpcClient) call(method string, params any, out any) error {
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	req, err := http.NewRequestWithContext(c.ctx, "POST", c.url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	r, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	var envelope struct {
		Result json.RawMessage
		Error  json.RawMessage
	}
	if err = json.NewDecoder(r.Body).Decode(&envelope); err != nil {
		return err
	}
	if len(envelope.Error) > 0 && string(envelope.Error) != "null" {
		return fmt.Errorf("%s: %s", method, envelope.Error)
	}
	return json.Unmarshal(envelope.Result, out)
}

type account struct {
	Data       []string `json:"data"`
	Owner      string   `json:"owner"`
	Lamports   uint64   `json:"lamports"`
	Executable bool     `json:"executable"`
	RentEpoch  uint64   `json:"rentEpoch"`
}

func compact(n int) []byte {
	b := []byte{}
	for {
		v := byte(n & 127)
		n >>= 7
		if n != 0 {
			v |= 128
		}
		b = append(b, v)
		if n == 0 {
			return b
		}
	}
}

type Case struct {
	Name         string   `json:"name"`
	Initial      string   `json:"initial"`
	Instructions []string `json:"instructions"`
	Expected     string   `json:"expected"`
	Error        uint64   `json:"error"`
	ErrorIndex   int      `json:"error_index"`
	Readonly     bool     `json:"readonly"`
	WrongOwner   bool     `json:"wrong_owner"`
	OmitAccount  bool     `json:"omit_account"`
}
type Suite struct {
	Schema int    `json:"schema"`
	Cases  []Case `json:"cases"`
}
type Result struct {
	Name      string `json:"name"`
	CU        uint64 `json:"cu"`
	Error     uint64 `json:"error"`
	Committed bool   `json:"committed"`
}

func key(s string) []byte { h := sha256.Sum256([]byte(s)); return h[:] }
func Load(path string) (Suite, error) {
	var s Suite
	b, e := os.ReadFile(path)
	if e != nil {
		return s, e
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if e = d.Decode(&s); e != nil {
		return s, e
	}
	if e = d.Decode(new(any)); e != io.EOF {
		return s, fmt.Errorf("fixture file must contain exactly one JSON object")
	}
	if s.Schema != 1 || len(s.Cases) == 0 {
		return s, fmt.Errorf("require fixture schema 1 and at least one case")
	}
	seen := map[string]bool{}
	for _, c := range s.Cases {
		if c.Name == "" || seen[c.Name] {
			return s, fmt.Errorf("fixture names must be nonempty and unique")
		}
		seen[c.Name] = true
		if len(c.Instructions) == 0 || len(c.Instructions) > 8 || c.ErrorIndex < 0 || c.ErrorIndex >= len(c.Instructions) || c.Error > 4294967295 {
			return s, fmt.Errorf("%s: invalid instruction count/error", c.Name)
		}
		for _, v := range append([]string{c.Initial, c.Expected}, c.Instructions...) {
			b, e := hex.DecodeString(v)
			if e != nil || len(b) > 1024 {
				return s, fmt.Errorf("%s: invalid hex or fixture over 1024 bytes", c.Name)
			}
		}
	}
	return s, nil
}

func transaction(payer ed25519.PrivateKey, pool, program, hash []byte, c Case) string {
	ro := byte(1)
	if c.Readonly {
		ro = 2
	}
	msg := []byte{1, 0, ro, 3}
	msg = append(msg, payer.Public().(ed25519.PublicKey)...)
	msg = append(msg, pool...)
	msg = append(msg, program...)
	msg = append(msg, hash...)
	msg = append(msg, compact(len(c.Instructions))...)
	for _, encoded := range c.Instructions {
		ix, _ := hex.DecodeString(encoded)
		msg = append(msg, 2)
		if c.OmitAccount {
			msg = append(msg, 0)
		} else {
			msg = append(msg, 1, 1)
		}
		msg = append(msg, compact(len(ix))...)
		msg = append(msg, ix...)
	}
	wire := append([]byte{1}, ed25519.Sign(payer, msg)...)
	wire = append(wire, msg...)
	return base64.StdEncoding.EncodeToString(wire)
}
func wantError(c Case) string {
	if c.Error == 0 {
		return "null"
	}
	return fmt.Sprintf(`{"InstructionError":[%d,{"Custom":%d}]}`, c.ErrorIndex, c.Error)
}
func checkError(got json.RawMessage, c Case) error {
	var compacted bytes.Buffer
	if e := json.Compact(&compacted, got); e != nil {
		return e
	}
	if compacted.String() != wantError(c) {
		return fmt.Errorf("transaction error %s; expected %s", got, wantError(c))
	}
	return nil
}
func stateBytes(a *account) ([]byte, error) {
	if a == nil || len(a.Data) != 2 || a.Data[1] != "base64" {
		return nil, fmt.Errorf("missing base64 account")
	}
	return base64.StdEncoding.DecodeString(a.Data[0])
}
func pause(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

// Run never contacts a remote cluster. All transaction keys are deterministic test
// keys, and all state is seeded into a new temporary validator ledger.
func Run(ctx context.Context, dir, elf string, out io.Writer) error {
	started := time.Now()
	suite, e := Load(filepath.Join(dir, "testdata/sbf.json"))
	if e != nil {
		return e
	}
	version, e := testvm.Command("--version").CombinedOutput()
	if e != nil {
		return fmt.Errorf("cannot start %s for SBF tests: %w", testvm.Engine(), e)
	}
	if e = testvm.CheckVersion(string(version)); e != nil {
		return e
	}
	elf, e = filepath.Abs(elf)
	if e != nil {
		return e
	}
	program := key("gosvm starter program; local tests only")
	payer := ed25519.NewKeyFromSeed(key("gosvm starter fee payer; never fund"))
	tmp, e := os.MkdirTemp("", "gosvm-validator-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(tmp)
	accounts := filepath.Join(tmp, "accounts")
	if e = os.Mkdir(accounts, 0700); e != nil {
		return e
	}
	for i, c := range suite.Cases {
		data, _ := hex.DecodeString(c.Initial)
		owner := b58(program)
		if c.WrongOwner {
			owner = b58(make([]byte, 32))
		}
		a := account{Data: []string{base64.StdEncoding.EncodeToString(data), "base64"}, Owner: owner, Lamports: 10000000}
		addr := b58(key(fmt.Sprintf("gosvm state %d", i)))
		b, _ := json.Marshal(map[string]any{"pubkey": addr, "account": a})
		if e = os.WriteFile(filepath.Join(accounts, addr+".json"), b, 0600); e != nil {
			return e
		}
	}
	build := filepath.Join(dir, "build")
	if e = os.MkdirAll(build, 0755); e != nil {
		return e
	}
	// Never leave a previous passing report behind after a failed new test run.
	if e = os.Remove(filepath.Join(build, "sbf-results.json")); e != nil && !os.IsNotExist(e) {
		return e
	}
	port := 0
	for n := 0; n < 100; n++ {
		l, e := net.Listen("tcp", "127.0.0.1:0")
		if e != nil {
			return e
		}
		p := l.Addr().(*net.TCPAddr).Port
		other, e := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p+1))
		l.Close()
		if e == nil {
			other.Close()
			port = p
			break
		}
	}
	if port == 0 {
		return fmt.Errorf("no free local RPC port pair")
	}
	log, e := os.Create(filepath.Join(build, "validator.log"))
	if e != nil {
		return e
	}
	defer log.Close()
	startup := time.Now()
	cmd := testvm.Command("--ledger", filepath.Join(tmp, "ledger"), "--rpc-port", fmt.Sprint(port), "--faucet-port", "0", "--bind-address", "127.0.0.1", "--mint", b58(payer.Public().(ed25519.PublicKey)), "--account-dir", accounts, "--bpf-program", b58(program), elf, "--quiet")
	cmd.Stdout, cmd.Stderr = log, log
	if e = cmd.Start(); e != nil {
		return e
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer func() {
		cmd.Process.Signal(os.Interrupt)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			cmd.Process.Kill()
			<-done
		}
		if b, e := os.ReadFile(filepath.Join(tmp, "ledger/validator.log")); e == nil {
			os.WriteFile(filepath.Join(build, "validator-detail.log"), b, 0644)
		}
	}()
	rpc := rpcClient{ctx: ctx, url: fmt.Sprintf("http://127.0.0.1:%d", port), client: http.Client{Timeout: 3 * time.Second}}
	ready := false
	fmt.Fprintf(out, "Starting isolated local %s...\n", testvm.Engine())
	for deadline := time.Now().Add(50 * time.Second); time.Now().Before(deadline); {
		var slot uint64
		if rpc.call("getSlot", []any{map[string]any{"commitment": "confirmed"}}, &slot) == nil && slot >= 2 {
			ready = true
			break
		}
		if e = pause(ctx, testvm.ReadyPoll()); e != nil {
			return e
		}
	}
	if !ready {
		return fmt.Errorf("validator startup failed; see build/validator-detail.log")
	}
	startupSeconds := time.Since(startup).Seconds()
	execution := time.Now()
	var results []Result
	for i, c := range suite.Cases {
		var h struct{ Value struct{ Blockhash string } }
		if e = rpc.call("getLatestBlockhash", []any{map[string]any{"commitment": "confirmed"}}, &h); e != nil {
			return e
		}
		pool := key(fmt.Sprintf("gosvm state %d", i))
		tx := transaction(payer, pool, program, un58(h.Value.Blockhash), c)
		var sim struct {
			Value struct {
				Err           json.RawMessage
				UnitsConsumed uint64
				Accounts      []*account
				Logs          []string
			}
		}
		if e = rpc.call("simulateTransaction", []any{tx, map[string]any{"encoding": "base64", "sigVerify": true, "commitment": "confirmed", "accounts": map[string]any{"encoding": "base64", "addresses": []string{b58(pool)}}}}, &sim); e != nil {
			return e
		}
		if e = checkError(sim.Value.Err, c); e != nil {
			return fmt.Errorf("%s simulation: %w\n%s", c.Name, e, strings.Join(sim.Value.Logs, "\n"))
		}
		if sim.Value.UnitsConsumed == 0 {
			return fmt.Errorf("%s: no SBF compute recorded", c.Name)
		}
		expected, _ := hex.DecodeString(c.Expected)
		// Failed simulation account snapshots need not be returned by all validators;
		// committed state below checks both successes and failures, including rollback.
		if c.Error == 0 {
			if len(sim.Value.Accounts) != 1 {
				return fmt.Errorf("%s: missing simulated state", c.Name)
			}
			b, e := stateBytes(sim.Value.Accounts[0])
			if e != nil || !bytes.Equal(b, expected) {
				return fmt.Errorf("%s: simulated state mismatch (%v)", c.Name, e)
			}
		}
		var sig string
		if e = rpc.call("sendTransaction", []any{tx, map[string]any{"encoding": "base64", "skipPreflight": true}}, &sig); e != nil {
			return e
		}
		confirmed := false
		for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); {
			var status struct {
				Value []*struct {
					Err                json.RawMessage
					ConfirmationStatus string
				}
			}
			if e = rpc.call("getSignatureStatuses", []any{[]string{sig}}, &status); e != nil {
				return e
			}
			if len(status.Value) == 1 && status.Value[0] != nil && (status.Value[0].ConfirmationStatus == "confirmed" || status.Value[0].ConfirmationStatus == "finalized") {
				if e = checkError(status.Value[0].Err, c); e != nil {
					return fmt.Errorf("%s commit: %w", c.Name, e)
				}
				confirmed = true
				break
			}
			if e = pause(ctx, 100*time.Millisecond); e != nil {
				return e
			}
		}
		if !confirmed {
			return fmt.Errorf("%s: confirmation timeout", c.Name)
		}
		var persisted struct{ Value *account }
		if e = rpc.call("getAccountInfo", []any{b58(pool), map[string]any{"encoding": "base64", "commitment": "confirmed"}}, &persisted); e != nil {
			return e
		}
		b, e := stateBytes(persisted.Value)
		if e != nil || !bytes.Equal(b, expected) {
			return fmt.Errorf("%s: committed state/rollback mismatch (%v)", c.Name, e)
		}
		results = append(results, Result{c.Name, sim.Value.UnitsConsumed, c.Error, true})
		fmt.Fprintf(out, "PASS %-28s %d CU\n", c.Name, sim.Value.UnitsConsumed)
	}
	elfBytes, e := os.ReadFile(elf)
	if e != nil {
		return e
	}
	fixtures, e := os.ReadFile(filepath.Join(dir, "testdata/sbf.json"))
	if e != nil {
		return e
	}
	metadata := map[string]any{"schema": 1, "elf_sha256": fmt.Sprintf("%x", sha256.Sum256(elfBytes)), "fixtures_sha256": fmt.Sprintf("%x", sha256.Sum256(fixtures)), "runtime_engine": testvm.Engine(), "runtime_version": strings.TrimSpace(string(version)), "cases": results,
		"startup_seconds": startupSeconds, "fixture_seconds": time.Since(execution).Seconds(), "total_seconds": time.Since(started).Seconds()}
	if testvm.IsRunner() {
		var info any
		if e = rpc.call("gosvmRuntimeInfo", []any{}, &info); e != nil {
			return e
		}
		metadata["runner"] = info
	} else {
		metadata["validator"] = strings.TrimSpace(string(version))
	}
	report, e := json.MarshalIndent(metadata, "", "  ")
	if e != nil {
		return e
	}
	return os.WriteFile(filepath.Join(build, "sbf-results.json"), append(report, '\n'), 0644)
}
