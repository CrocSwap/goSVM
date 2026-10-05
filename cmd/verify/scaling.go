package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func runScaling() error {
	build, err := filepath.Abs("build")
	if err != nil {
		return err
	}
	b, err := os.ReadFile(filepath.Join(build, "scaling-benchmark.json"))
	if err != nil {
		return fmt.Errorf("run make scale first: %w", err)
	}
	var bench struct {
		Rows []struct {
			Functions int
			ELFSHA256 map[string]string `json:"elf_sha256"`
			Vectors   []struct{ Seed, Expected uint64 }
		}
	}
	if err = json.Unmarshal(b, &bench); err != nil {
		return err
	}
	if len(bench.Rows) != 4 {
		return fmt.Errorf("expected four scaling workloads")
	}
	tmp, err := os.MkdirTemp(build, "scale-validator-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	dir := filepath.Join(tmp, "accounts")
	must(os.Mkdir(dir, 0755))
	payer := ed25519.NewKeyFromSeed(key("scaling local payer - never fund"))
	args := []string{"--ledger", filepath.Join(tmp, "ledger"), "--faucet-port", "0", "--bind-address", "127.0.0.1", "--mint", b58(payer.Public().(ed25519.PublicKey)), "--account-dir", dir, "--quiet"}
	for _, row := range bench.Rows {
		for _, backend := range []string{"go", "rust"} {
			id := fmt.Sprintf("scale-%d-%s", row.Functions, backend)
			program, pool := key(id), key(id+" pool")
			elf := filepath.Join(build, "scaling", fmt.Sprint(row.Functions), "go.so")
			if backend == "rust" {
				elf = filepath.Join(build, "scaling", fmt.Sprint(row.Functions), "rust-out", "amm_rust.so")
			}
			data, e := os.ReadFile(elf)
			if e != nil {
				return e
			}
			if len(data) < 64 || !bytes.Equal(data[:4], []byte{127, 'E', 'L', 'F'}) || binary.LittleEndian.Uint32(data[48:52]) != 3 {
				return fmt.Errorf("bad v3 ELF %s", elf)
			}
			if fmt.Sprintf("%x", sha256.Sum256(data)) != row.ELFSHA256[backend] {
				return fmt.Errorf("stale scaling ELF %s", elf)
			}
			args = append(args, "--bpf-program", b58(program), elf)
			a := account{Data: []string{base64.StdEncoding.EncodeToString(make([]byte, 24)), "base64"}, Owner: b58(program), Lamports: 10000000}
			j, _ := json.Marshal(map[string]any{"pubkey": b58(pool), "account": a})
			must(os.WriteFile(filepath.Join(dir, id+".json"), j, 0644))
		}
	}
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
		return fmt.Errorf("no RPC port pair")
	}
	args = append(args, "--rpc-port", fmt.Sprint(port))
	log, err := os.Create(filepath.Join(build, "scaling-validator.log"))
	if err != nil {
		return err
	}
	defer log.Close()
	cmd := exec.Command("solana-test-validator", args...)
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
			os.WriteFile(filepath.Join(build, "scaling-validator-detail.log"), b, 0644)
		}
	}()
	c := rpcClient{url: fmt.Sprintf("http://127.0.0.1:%d", port), client: http.Client{Timeout: 5 * time.Second}}
	ready := false
	for deadline := time.Now().Add(50 * time.Second); time.Now().Before(deadline); {
		var slot uint64
		if c.call("getSlot", []any{map[string]any{"commitment": "confirmed"}}, &slot) == nil && slot >= 2 {
			ready = true
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !ready {
		return fmt.Errorf("scaling validator startup failed; see build/scaling-validator-detail.log")
	}
	var latest struct{ Value struct{ Blockhash string } }
	must(c.call("getLatestBlockhash", []any{map[string]any{"commitment": "confirmed"}}, &latest))
	results := []map[string]any{}
	for _, row := range bench.Rows {
		for _, backend := range []string{"go", "rust"} {
			id := fmt.Sprintf("scale-%d-%s", row.Functions, backend)
			program, pool := key(id), key(id+" pool")
			for _, v := range row.Vectors {
				tx := transaction(payer, pool, program, un58(latest.Value.Blockhash), vector{}, [][]byte{words(v.Seed, 0)})
				var sim struct {
					Value struct {
						Err           json.RawMessage
						UnitsConsumed uint64
						Accounts      []*account
						Logs          []string
					}
				}
				must(c.call("simulateTransaction", []any{tx, map[string]any{"encoding": "base64", "sigVerify": true, "commitment": "confirmed", "accounts": map[string]any{"encoding": "base64", "addresses": []string{b58(pool)}}}}, &sim))
				if string(sim.Value.Err) != "null" {
					return fmt.Errorf("%s seed %d: %s %v", id, v.Seed, sim.Value.Err, sim.Value.Logs)
				}
				if len(sim.Value.Accounts) != 1 || sim.Value.Accounts[0] == nil {
					return fmt.Errorf("missing account")
				}
				actual, e := base64.StdEncoding.DecodeString(sim.Value.Accounts[0].Data[0])
				if e != nil {
					return e
				}
				if !bytes.Equal(actual, words(v.Expected, 0, 0)) {
					return fmt.Errorf("%s seed %d checksum differs from Python", id, v.Seed)
				}
				results = append(results, map[string]any{"functions": row.Functions, "backend": backend, "seed": v.Seed, "cu": sim.Value.UnitsConsumed, "elf_sha256": row.ELFSHA256[backend]})
			}
		}
	}
	version, _ := exec.Command("solana-test-validator", "--version").Output()
	report := map[string]any{"validator": string(bytes.TrimSpace(version)), "target": "sBPF v3", "deactivated_features": []string{}, "reference": "independent Python uint64 arithmetic, five input seeds at each size", "results": results}
	out, _ := json.MarshalIndent(report, "", "  ")
	must(os.WriteFile(filepath.Join(build, "scaling-verification.json"), append(out, '\n'), 0644))
	fmt.Printf("PASS: %d scaling VM checks across four sizes and two backends\n", len(results))
	return nil
}
