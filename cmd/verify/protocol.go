package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type protocolCase struct {
	Name, Mutation                                string
	State, Config, Payload, Instruction, Expected []byte
	Code                                          uint64
	Accounts                                      int
}

func protocolFixture(prefix string, program, user []byte, v protocolCase) tokenFixture {
	f := tokenFixture{keys: [][]byte{key(prefix + "state"), user, key(prefix + "config"), key(prefix + "data")}, data: [][]byte{v.State, {}, v.Config, v.Payload}, accounts: make([]account, 4), writable: []bool{true, false, false, false}, userSigner: true, count: v.Accounts, ix: v.Instruction}
	for i := range f.accounts {
		f.accounts[i] = account{Owner: b58(program), Lamports: 10000000}
	}
	f.accounts[1].Owner = b58(make([]byte, 32))
	switch v.Mutation {
	case "wrong-owner":
		f.accounts[0].Owner = b58(key("foreign-owner"))
	case "config-owner":
		f.accounts[2].Owner = b58(key("foreign-owner"))
	case "payload-owner":
		f.accounts[3].Owner = b58(key("foreign-owner"))
	case "readonly":
		f.writable[0] = false
	case "no-signer":
		f.userSigner = false
	case "alias":
		f.keys[3] = f.keys[2]
	}
	return f
}

func runProtocol() error {
	build, err := filepath.Abs("build/protocol")
	if err != nil {
		return err
	}
	b, err := os.ReadFile(filepath.Join(build, "fixtures.json"))
	if err != nil {
		return err
	}
	var fixtures struct {
		Authority []byte
		Cases     []protocolCase
		Lifecycle []int
	}
	if err = json.Unmarshal(b, &fixtures); err != nil {
		return err
	}
	if len(fixtures.Cases) != 116 || len(fixtures.Lifecycle) != 20 {
		return fmt.Errorf("unexpected protocol fixture coverage")
	}
	files := []string{"protocol_go", "protocol_bench"}
	programs := [][]byte{key("protocol go"), key("protocol rust")}
	hashes := map[string]string{}
	for _, name := range files {
		data, e := os.ReadFile(filepath.Join(build, name+".so"))
		if e != nil {
			return e
		}
		if len(data) < 64 || !bytes.Equal(data[:4], []byte{127, 'E', 'L', 'F'}) || binary.LittleEndian.Uint32(data[48:52]) != 3 {
			return fmt.Errorf("%s must be v3 ELF", name)
		}
		hashes[name] = fmt.Sprintf("%x", sha256.Sum256(data))
	}
	seed, _ := hex.DecodeString("9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60")
	user := ed25519.NewKeyFromSeed(seed)
	if !bytes.Equal(user.Public().(ed25519.PublicKey), fixtures.Authority) {
		return fmt.Errorf("operator fixture mismatch")
	}
	payer := ed25519.NewKeyFromSeed(key("protocol local payer never fund"))
	tmp, err := os.MkdirTemp(build, "validator-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	dir := filepath.Join(tmp, "accounts")
	must(os.Mkdir(dir, 0755))
	written := map[string]bool{}
	write := func(f tokenFixture) {
		for i, k := range f.keys {
			address := b58(k)
			if written[address] {
				continue
			}
			written[address] = true
			a := f.accounts[i]
			a.Data = []string{base64.StdEncoding.EncodeToString(f.data[i]), "base64"}
			data, _ := json.Marshal(map[string]any{"pubkey": address, "account": a})
			must(os.WriteFile(filepath.Join(dir, address+".json"), data, 0644))
		}
	}
	cases := make([][]tokenFixture, 2)
	lifecycle := make([]tokenFixture, 2)
	rollback := make([]tokenFixture, 2)
	for backend, program := range programs {
		for i, v := range fixtures.Cases {
			f := protocolFixture(fmt.Sprintf("case-%d-%d-", backend, i), program, fixtures.Authority, v)
			cases[backend] = append(cases[backend], f)
			write(f)
		}
		lifecycle[backend] = protocolFixture(fmt.Sprintf("sequence-%d-", backend), program, fixtures.Authority, fixtures.Cases[0])
		write(lifecycle[backend])
		rollback[backend] = protocolFixture(fmt.Sprintf("rollback-%d-", backend), program, fixtures.Authority, fixtures.Cases[0])
		write(rollback[backend])
	}
	args := []string{"--ledger", filepath.Join(tmp, "ledger"), "--faucet-port", "0", "--bind-address", "127.0.0.1", "--mint", b58(payer.Public().(ed25519.PublicKey)), "--account-dir", dir, "--quiet"}
	for i, name := range files {
		args = append(args, "--bpf-program", b58(programs[i]), filepath.Join(build, name+".so"))
	}
	c, stop, err := startProtocolValidator(args, tmp, build)
	if err != nil {
		return err
	}
	defer stop()
	blockhash := func() []byte {
		var latest struct{ Value struct{ Blockhash string } }
		must(c.call("getLatestBlockhash", []any{map[string]any{"commitment": "confirmed"}}, &latest))
		return un58(latest.Value.Blockhash)
	}
	hash := blockhash()
	rows := []result{}
	for backend, program := range programs {
		name := []string{"go", "rust"}[backend]
		for i, v := range fixtures.Cases {
			f := cases[backend][i]
			tx := tokenTransaction(payer, user, program, hash, f, [][]byte{v.Instruction})
			var sim struct {
				Value struct {
					Err           json.RawMessage
					UnitsConsumed uint64
					Accounts      []*account
					Logs          []string
				}
			}
			must(c.call("simulateTransaction", []any{tx, map[string]any{"encoding": "base64", "sigVerify": true, "commitment": "confirmed", "accounts": map[string]any{"encoding": "base64", "addresses": []string{b58(f.keys[0])}}}}, &sim))
			want := "null"
			if v.Code != 0 {
				want = fmt.Sprintf(`{"InstructionError":[0,{"Custom":%d}]}`, v.Code)
			}
			if string(sim.Value.Err) != want {
				return fmt.Errorf("%s/%s expected %s got %s logs=%v", name, v.Name, want, sim.Value.Err, sim.Value.Logs)
			}
			if v.Code == 0 {
				if len(sim.Value.Accounts) != 1 || sim.Value.Accounts[0] == nil {
					return fmt.Errorf("missing simulated state")
				}
				actual, e := base64.StdEncoding.DecodeString(sim.Value.Accounts[0].Data[0])
				if e != nil {
					return e
				}
				if !bytes.Equal(actual, v.Expected) {
					return fmt.Errorf("%s/%s reference state mismatch", name, v.Name)
				}
			}
			rows = append(rows, result{Name: v.Name, Backend: name, CU: sim.Value.UnitsConsumed, Code: v.Code})
		}
		f := lifecycle[backend]
		for _, i := range fixtures.Lifecycle {
			v := fixtures.Cases[i]
			tx := tokenTransaction(payer, user, program, blockhash(), f, [][]byte{v.Instruction})
			if err := submitToken(c, tx, "null"); err != nil {
				return fmt.Errorf("%s lifecycle %s: %w", name, v.Name, err)
			}
			if err := protocolState(c, f.keys[0], v.Expected); err != nil {
				return err
			}
		}
		f = rollback[backend]
		bad := bytes.Clone(fixtures.Cases[1].Instruction)
		binary.LittleEndian.PutUint64(bad[8:], 8)
		tx := tokenTransaction(payer, user, program, blockhash(), f, [][]byte{fixtures.Cases[0].Instruction, bad})
		if err := submitToken(c, tx, `{"InstructionError":[1,{"Custom":201}]}`); err != nil {
			return err
		}
		if err := protocolState(c, f.keys[0], fixtures.Cases[0].State); err != nil {
			return err
		}
	}
	version, _ := exec.Command("solana-test-validator", "--version").Output()
	report := map[string]any{"validator": string(bytes.TrimSpace(version)), "target": "sBPF v3", "deactivated_features": []string{}, "elf_sha256": hashes, "vectors_per_backend": len(fixtures.Cases), "committed_lifecycle_instructions_per_backend": len(fixtures.Lifecycle), "atomic_rollback_per_backend": true, "reference": "Python hashlib and uint64 arithmetic; all 64 kernels exercised", "results": rows}
	out, _ := json.MarshalIndent(report, "", "  ")
	must(os.WriteFile(filepath.Join(build, "verification.json"), append(out, '\n'), 0644))
	fmt.Printf("PASS: %d protocol simulations, %d committed lifecycle instructions, 2 rollback checks\n", len(rows), len(fixtures.Lifecycle)*2)
	return nil
}

func protocolState(c *rpcClient, k, expected []byte) error {
	var got struct{ Value account }
	if err := c.call("getAccountInfo", []any{b58(k), map[string]any{"encoding": "base64", "commitment": "confirmed"}}, &got); err != nil {
		return err
	}
	actual, err := base64.StdEncoding.DecodeString(got.Value.Data[0])
	if err != nil {
		return err
	}
	if !bytes.Equal(actual, expected) {
		return fmt.Errorf("persistent state/rollback mismatch")
	}
	return nil
}

func startProtocolValidator(args []string, tmp, build string) (*rpcClient, func(), error) {
	port := 0
	for attempt := 0; attempt < 100; attempt++ {
		l, e := net.Listen("tcp", "127.0.0.1:0")
		if e != nil {
			return nil, nil, e
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
		return nil, nil, fmt.Errorf("no RPC port pair")
	}
	args = append(args, "--rpc-port", fmt.Sprint(port))
	log, err := os.Create(filepath.Join(build, "validator.log"))
	if err != nil {
		return nil, nil, err
	}
	cmd := exec.Command("solana-test-validator", args...)
	cmd.Stdout, cmd.Stderr = log, log
	if err = cmd.Start(); err != nil {
		log.Close()
		return nil, nil, err
	}
	stop := func() {
		cmd.Process.Signal(os.Interrupt)
		done := make(chan struct{})
		go func() { cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			cmd.Process.Kill()
			<-done
		}
		log.Close()
		if b, e := os.ReadFile(filepath.Join(tmp, "ledger", "validator.log")); e == nil {
			os.WriteFile(filepath.Join(build, "validator-detail.log"), b, 0644)
		}
	}
	c := &rpcClient{url: fmt.Sprintf("http://127.0.0.1:%d", port), client: http.Client{Timeout: 5 * time.Second}}
	for deadline := time.Now().Add(50 * time.Second); time.Now().Before(deadline); {
		var slot uint64
		if c.call("getSlot", []any{map[string]any{"commitment": "confirmed"}}, &slot) == nil && slot >= 2 {
			return c, stop, nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	stop()
	return nil, nil, fmt.Errorf("validator startup timeout; see build/protocol/validator-detail.log")
}
