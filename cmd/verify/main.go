// verify runs both ELFs on an isolated local Agave validator. No external cluster.
package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math/big"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"gosvm/examples/amm"
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
func key(s string) []byte { v := sha256.Sum256([]byte(s)); return v[:] }
func words(v ...uint64) []byte {
	b := make([]byte, len(v)*8)
	for i, x := range v {
		binary.LittleEndian.PutUint64(b[i*8:], x)
	}
	return b
}
func must(err error) {
	if err != nil {
		panic(err)
	}
}

type rpcClient struct {
	url    string
	client http.Client
}

func (c *rpcClient) call(method string, params any, out any) error {
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	r, err := c.client.Post(c.url, "application/json", bytes.NewReader(b))
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
type vector struct {
	name                                        string
	state, instruction                          []byte
	readonly, wrongOwner, noAccounts, duplicate bool
	code                                        uint64
}
type result struct {
	Name    string `json:"name"`
	Backend string `json:"backend"`
	CU      uint64 `json:"cu"`
	Code    uint64 `json:"code"`
	TokenCU uint64 `json:"token_cu,omitempty"`
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
func transaction(payer ed25519.PrivateKey, pool, program, hash []byte, v vector, instructions [][]byte) string {
	readonly := byte(1)
	if v.readonly {
		readonly = 2
	}
	msg := []byte{1, 0, readonly, 3}
	msg = append(msg, payer.Public().(ed25519.PublicKey)...)
	msg = append(msg, pool...)
	msg = append(msg, program...)
	msg = append(msg, hash...)
	msg = append(msg, compact(len(instructions))...)
	for _, data := range instructions {
		msg = append(msg, 2)
		if v.noAccounts {
			msg = append(msg, 0)
		} else if v.duplicate {
			msg = append(msg, 2, 1, 1)
		} else {
			msg = append(msg, 1, 1)
		}
		msg = append(msg, compact(len(data))...)
		msg = append(msg, data...)
	}
	wire := append([]byte{1}, ed25519.Sign(payer, msg)...)
	wire = append(wire, msg...)
	return base64.StdEncoding.EncodeToString(wire)
}

func main() {
	if len(os.Args) == 2 && os.Args[1] == "lifecycle" {
		if err := runLifecycle(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "anchor-bounded" {
		must(runAnchorBounded())
		return
	}
	runner := run
	if len(os.Args) == 2 && os.Args[1] == "anchor" {
		must(runTokenComparison(true))
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "tokens" {
		runner = runTokens
	}
	if len(os.Args) == 2 && os.Args[1] == "scale" {
		runner = runScaling
	}
	if len(os.Args) == 2 && os.Args[1] == "protocol" {
		runner = runProtocol
	}
	if err := runner(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	arch := os.Getenv("SBF_ARCH")
	if arch == "" {
		arch = "v3"
	}
	var elfVersion uint32
	switch arch {
	case "v0":
		elfVersion = 0
	case "v3":
		elfVersion = 3
	default:
		return fmt.Errorf("unsupported target %s", arch)
	}
	build, err := filepath.Abs("build")
	if err != nil {
		return err
	}
	must(os.MkdirAll(build, 0755))
	elfHashes := map[string]string{}
	for _, name := range []string{"amm-go.so", "amm_rust.so"} {
		b, err := os.ReadFile(filepath.Join(build, name))
		if err != nil {
			return err
		}
		if len(b) < 64 || !bytes.Equal(b[:4], []byte{127, 'E', 'L', 'F'}) || binary.LittleEndian.Uint32(b[48:52]) != elfVersion {
			return fmt.Errorf("%s is not an sBPF %s ELF; rebuild both artifacts", name, arch)
		}
		elfHashes[name] = fmt.Sprintf("%x", sha256.Sum256(b))
	}
	tmp, err := os.MkdirTemp(build, "validator-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	fixtures := filepath.Join(tmp, "accounts")
	must(os.Mkdir(fixtures, 0755))
	guardProgram, guardPool := key("gosvm runtime guards"), key("gosvm runtime guard account")
	guardSource := filepath.Join(tmp, "guard.go")
	must(os.WriteFile(guardSource, []byte(`package guards
func Process(s,i []byte) uint64 {
    if i[0]==0 {s[0]=s[uint64(i[1])]} else {s[0]=s[0]/i[1]}
    return 0
}`), 0644))
	guardELF := filepath.Join(tmp, "guard.so")
	guardArgs := []string{"-arch", arch, "-o", guardELF, guardSource}
	if llvm := os.Getenv("SBF_LLVM"); llvm != "" {
		guardArgs = append([]string{"-llvm", llvm}, guardArgs...)
	} else if tools := os.Getenv("SBF_TOOLS"); tools != "" {
		guardArgs = append([]string{"-llvm", filepath.Join(tools, "llvm")}, guardArgs...)
	}
	if out, err := exec.Command(filepath.Join(build, "gosvm"), guardArgs...).CombinedOutput(); err != nil {
		return fmt.Errorf("guard compilation: %w: %s", err, out)
	}
	guardAccount := account{Data: []string{base64.StdEncoding.EncodeToString(make([]byte, 24)), "base64"}, Owner: b58(guardProgram), Lamports: 10000000}
	guardJSON, _ := json.Marshal(map[string]any{"pubkey": b58(guardPool), "account": guardAccount})
	must(os.WriteFile(filepath.Join(fixtures, "guards.json"), guardJSON, 0644))
	payer := ed25519.NewKeyFromSeed(key("gosvm local test payer - never fund"))
	programs := [][]byte{key("gosvm go program"), key("gosvm rust program")}
	base := words(1000000, 2000000, 0)
	ix := words(10000, 19000)
	vectors := []vector{
		{name: "swap", state: base, instruction: ix},
		{name: "max-input", state: words(1000000, 1000000000, 0), instruction: words(1000000, 0)},
		{name: "zero-input", state: base, instruction: words(0, 0), code: 3},
		{name: "input-cap", state: base, instruction: words(1000001, 0), code: 3},
		{name: "reserve-cap", state: words(999999999, 2000000, 0), instruction: words(2, 0), code: 3},
		{name: "empty-pool", state: words(0, 2000000, 0), instruction: ix, code: 2},
		{name: "invalid-reserve", state: words(^uint64(0), 2000000, 0), instruction: ix, code: 2},
		{name: "counter-overflow", state: words(1000000, 2000000, ^uint64(0)), instruction: ix, code: 4},
		{name: "slippage", state: base, instruction: words(10000, 20000), code: 5},
		{name: "rounds-to-zero", state: words(100000000, 1, 0), instruction: words(1, 0), code: 5},
		{name: "short-state", state: words(1, 2), instruction: ix, code: 12},
		{name: "short-instruction", state: base, instruction: words(1), code: 13},
		{name: "long-instruction", state: base, instruction: append(ix, 0), code: 13},
		{name: "readonly", state: base, instruction: ix, readonly: true, code: 11},
		{name: "wrong-owner", state: base, instruction: ix, wrongOwner: true, code: 14},
		{name: "no-accounts", state: base, instruction: ix, noAccounts: true, code: 10},
		{name: "duplicate-account", state: base, instruction: ix, duplicate: true, code: 10},
	}
	rng := rand.New(rand.NewSource(42))
	for i := 0; i < 64; i++ {
		state := words(uint64(rng.Intn(900000000)+1), uint64(rng.Intn(1000000000)+1), uint64(i))
		instruction := words(uint64(rng.Intn(1000000)+1), 0)
		copyState := bytes.Clone(state)
		code := amm.Process(copyState, instruction)
		vectors = append(vectors, vector{name: fmt.Sprintf("random-%02d", i), state: state, instruction: instruction, code: code})
	}
	for backend, program := range programs {
		for i, v := range vectors {
			pool := key(fmt.Sprintf("pool-%d-%d", backend, i))
			owner := b58(program)
			if v.wrongOwner {
				owner = b58(key("other owner"))
			}
			a := account{Data: []string{base64.StdEncoding.EncodeToString(v.state), "base64"}, Owner: owner, Lamports: 10000000}
			b, _ := json.Marshal(map[string]any{"pubkey": b58(pool), "account": a})
			must(os.WriteFile(filepath.Join(fixtures, fmt.Sprintf("%d-%d.json", backend, i)), b, 0644))
		}
	}
	// Reserve an available RPC port and its adjacent websocket port.
	port := 0
	for attempt := 0; attempt < 100; attempt++ {
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
		return fmt.Errorf("no free RPC port pair")
	}
	log, err := os.Create(filepath.Join(build, "validator.log"))
	if err != nil {
		return err
	}
	defer log.Close()
	cmd := exec.Command("solana-test-validator", "--ledger", filepath.Join(tmp, "ledger"), "--rpc-port", fmt.Sprint(port), "--faucet-port", "0", "--bind-address", "127.0.0.1", "--mint", b58(payer.Public().(ed25519.PublicKey)), "--account-dir", fixtures, "--bpf-program", b58(programs[0]), filepath.Join(build, "amm-go.so"), "--bpf-program", b58(programs[1]), filepath.Join(build, "amm_rust.so"), "--quiet")
	cmd.Args = append(cmd.Args, "--bpf-program", b58(guardProgram), guardELF)
	disabledFeatures := []string{}
	if arch == "v0" {
		cmd.Args = append(cmd.Args, "--deactivate-feature", "TestFeature11111111111111111111111111111111")
		disabledFeatures = append(disabledFeatures, "TestFeature11111111111111111111111111111111 (disable_sbpf_v0_execution)")
	}
	cmd.Stdout, cmd.Stderr = log, log
	if err = cmd.Start(); err != nil {
		return err
	}
	defer func() {
		cmd.Process.Signal(os.Interrupt)
		done := make(chan struct{})
		go func() { cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			cmd.Process.Kill()
			<-done
		}
		if b, e := os.ReadFile(filepath.Join(tmp, "ledger", "validator.log")); e == nil {
			_ = os.WriteFile(filepath.Join(build, "validator-detail.log"), b, 0644)
		}
	}()
	c := rpcClient{url: fmt.Sprintf("http://127.0.0.1:%d", port), client: http.Client{Timeout: 3 * time.Second}}
	ready := false
	for deadline := time.Now().Add(45 * time.Second); time.Now().Before(deadline); {
		var health string
		if c.call("getHealth", []any{}, &health) == nil {
			ready = true
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if !ready {
		b, _ := os.ReadFile(filepath.Join(build, "validator.log"))
		return fmt.Errorf("validator startup failed: %s", b)
	}
	// Genesis-preloaded programs are not executable in their deployment slot.
	// Health can become ready at slot zero, so wait for bank visibility explicitly.
	visible := false
	for deadline := time.Now().Add(45 * time.Second); time.Now().Before(deadline); {
		var slot uint64
		if c.call("getSlot", []any{map[string]any{"commitment": "confirmed"}}, &slot) == nil && slot >= 2 {
			visible = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !visible {
		return fmt.Errorf("validator did not advance beyond genesis")
	}
	var latest struct{ Value struct{ Blockhash string } }
	must(c.call("getLatestBlockhash", []any{map[string]any{"commitment": "confirmed"}}, &latest))
	hash := un58(latest.Value.Blockhash)
	rows := []result{}
	for _, guard := range []struct {
		name      string
		op, index byte
		fail      bool
	}{{"valid", 0, 0, false}, {"bounds", 0, 24, true}, {"division", 1, 0, true}} {
		data := make([]byte, 16)
		data[0], data[1] = guard.op, guard.index
		tx := transaction(payer, guardPool, guardProgram, hash, vector{}, [][]byte{data})
		var sim struct {
			Value struct {
				Err  json.RawMessage
				Logs []string
			}
		}
		must(c.call("simulateTransaction", []any{tx, map[string]any{"encoding": "base64", "sigVerify": true, "commitment": "confirmed"}}, &sim))
		if !guard.fail {
			if string(sim.Value.Err) != "null" {
				return fmt.Errorf("guard valid execution: %s %v", sim.Value.Err, sim.Value.Logs)
			}
		} else {
			var e struct{ InstructionError []json.RawMessage }
			must(json.Unmarshal(sim.Value.Err, &e))
			if len(e.InstructionError) != 2 || string(e.InstructionError[1]) != `"ProgramFailedToComplete"` {
				return fmt.Errorf("guard %s did not abort: %s %v", guard.name, sim.Value.Err, sim.Value.Logs)
			}
		}
	}
	for backend, program := range programs {
		name := []string{"go", "rust"}[backend]
		for i, v := range vectors {
			pool := key(fmt.Sprintf("pool-%d-%d", backend, i))
			tx := transaction(payer, pool, program, hash, v, [][]byte{v.instruction})
			var sim struct {
				Value struct {
					Err           json.RawMessage
					UnitsConsumed uint64
					Accounts      []*account
					Logs          []string
				}
			}
			must(c.call("simulateTransaction", []any{tx, map[string]any{"encoding": "base64", "sigVerify": true, "commitment": "confirmed", "accounts": map[string]any{"encoding": "base64", "addresses": []string{b58(pool)}}}}, &sim))
			if v.code == 0 {
				if string(sim.Value.Err) != "null" {
					return fmt.Errorf("%s/%s: %s logs=%v", name, v.name, sim.Value.Err, sim.Value.Logs)
				}
				expected := bytes.Clone(v.state)
				if code := amm.Process(expected, v.instruction); code != 0 {
					return fmt.Errorf("native reference failed: %d", code)
				}
				if len(sim.Value.Accounts) != 1 || sim.Value.Accounts[0] == nil {
					return fmt.Errorf("missing simulated account")
				}
				actual, e := base64.StdEncoding.DecodeString(sim.Value.Accounts[0].Data[0])
				must(e)
				if !bytes.Equal(actual, expected) {
					return fmt.Errorf("%s/%s state mismatch", name, v.name)
				}
			} else {
				var e struct{ InstructionError []json.RawMessage }
				must(json.Unmarshal(sim.Value.Err, &e))
				if len(e.InstructionError) != 2 {
					return fmt.Errorf("%s/%s unexpected error %s", name, v.name, sim.Value.Err)
				}
				var custom struct{ Custom uint64 }
				must(json.Unmarshal(e.InstructionError[1], &custom))
				if custom.Custom != v.code {
					return fmt.Errorf("%s/%s expected %d got %s", name, v.name, v.code, sim.Value.Err)
				}
			}
			rows = append(rows, result{Name: v.name, Backend: name, CU: sim.Value.UnitsConsumed, Code: v.code})
		}
	}
	// Commit two swaps, then submit an atomic [valid swap, failing swap] transaction.
	// Verify real persistence across transactions and rollback of the first instruction.
	for backend, program := range programs {
		pool := key(fmt.Sprintf("pool-%d-0", backend))
		expected := bytes.Clone(base)
		for step := 0; step < 3; step++ {
			must(c.call("getLatestBlockhash", []any{map[string]any{"commitment": "confirmed"}}, &latest))
			data := words(10000+uint64(step), 0)
			instructions := [][]byte{data}
			if step == 2 {
				instructions = append(instructions, words(0, 0))
			}
			tx := transaction(payer, pool, program, un58(latest.Value.Blockhash), vector{}, instructions)
			var signature string
			must(c.call("sendTransaction", []any{tx, map[string]any{"encoding": "base64", "skipPreflight": true}}, &signature))
			confirmed := false
			for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); {
				var status struct {
					Value []*struct {
						Err                json.RawMessage
						ConfirmationStatus string
					}
				}
				must(c.call("getSignatureStatuses", []any{[]string{signature}}, &status))
				if len(status.Value) > 0 && status.Value[0] != nil && status.Value[0].ConfirmationStatus == "confirmed" {
					failed := string(status.Value[0].Err) != "null"
					if failed != (step == 2) {
						return fmt.Errorf("persistence step %d: %s", step, status.Value[0].Err)
					}
					confirmed = true
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
			if !confirmed {
				return fmt.Errorf("transaction confirmation timeout")
			}
			if step < 2 {
				if amm.Process(expected, data) != 0 {
					return fmt.Errorf("native persistence failed")
				}
			}
			var got struct{ Value account }
			must(c.call("getAccountInfo", []any{b58(pool), map[string]any{"encoding": "base64", "commitment": "confirmed"}}, &got))
			actual, e := base64.StdEncoding.DecodeString(got.Value.Data[0])
			must(e)
			if !bytes.Equal(actual, expected) {
				return fmt.Errorf("backend %d persistence/rollback mismatch at step %d", backend, step)
			}
		}
	}
	version, _ := exec.Command("solana-test-validator", "--version").Output()
	report := map[string]any{"validator": string(bytes.TrimSpace(version)), "target": "sBPF " + arch, "elf_sha256": elfHashes, "deactivated_features": disabledFeatures, "runtime_guard_checks": 3, "vectors_per_backend": len(vectors), "persistent_swaps_per_backend": 2, "atomic_rollback_per_backend": true, "results": rows}
	b, _ := json.MarshalIndent(report, "", "  ")
	must(os.WriteFile(filepath.Join(build, "verification.json"), append(b, '\n'), 0644))
	fmt.Printf("PASS: %d AMM simulations, 3 runtime guard checks, 4 committed swaps, 2 atomic rollback checks\n", len(rows))
	for _, r := range rows {
		if r.Name == "swap" || r.Name == "max-input" {
			fmt.Printf("%s %-12s %d CU\n", r.Backend, r.Name, r.CU)
		}
	}
	return nil
}
