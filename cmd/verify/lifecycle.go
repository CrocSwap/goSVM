package main

// This is an independent lifecycle oracle: expected bytes and balances are
// constructed from the classic SPL layouts and big-integer swap reference.
// No expected account state is copied from simulation output.
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
	"sort"
	"strings"
	"time"

	"gosvm/internal/sbftest"
	"gosvm/internal/testvm"
)

const systemAddress = "11111111111111111111111111111111"
const exemptEpoch = ^uint64(0)

func lifecycleSuite(tokenHash string, createdEpoch uint64) sbftest.FixtureSuite {
	program := key("tokenswap Go")
	s := sbftest.FixtureSuite{Format: sbftest.FixtureFormat, ProgramID: b58(program), PayerSeed: hex.EncodeToString(key("lifecycle test payer - never fund")),
		Programs:       []sbftest.FixtureProgram{{Name: "token", Address: tokenAddress, ELF: "spl-token.so", SHA256: tokenHash}},
		NativePrograms: []sbftest.FixtureNativeProgram{{Name: "system", Address: systemAddress}}}
	// Default Rent: (data bytes + 128) * 3480 * 2, independently checked by RPC.
	rent := func(size uint64) uint64 { return (size + 128) * 3480 * 2 }
	for _, prefix := range []string{"flow", "badsize", "badinitrent", "badrent", "nosigner", "split"} {
		addresses := map[string][]byte{}
		states := map[string]*sbftest.FixtureState{}
		name := func(n string) string { return prefix + "_" + n }
		for _, n := range []string{"funder", "user", "wrong", "mintx", "minty", "ux", "vx", "vy", "uy", "receiver"} {
			seed := key("lifecycle " + name(n) + " - never fund")
			private := ed25519.NewKeyFromSeed(seed)
			addresses[n] = private.Public().(ed25519.PublicKey)
			s.Signers = append(s.Signers, sbftest.FixtureSigner{Name: name(n), Seed: hex.EncodeToString(seed)})
			var initial *sbftest.FixtureState
			if n == "funder" || n == "user" || n == "wrong" || n == "receiver" {
				balance := uint64(10000000)
				if n == "funder" {
					balance = 1000000000
				}
				initial = &sbftest.FixtureState{Owner: systemAddress, Lamports: balance, RentEpoch: exemptEpoch}
			}
			states[n] = initial
			s.Accounts = append(s.Accounts, sbftest.FixtureAccount{Name: name(n), Address: b58(addresses[n]), Initial: initial})
		}
		states["pool"] = nil
		addresses["pool"] = key("lifecycle " + name("pool"))
		addresses["pda"], _ = derivePDA(addresses["pool"], program)
		s.Accounts = append(s.Accounts, sbftest.FixtureAccount{Name: name("pda"), Address: b58(addresses["pda"])})
		pool := make([]byte, 137)
		copy(pool, addresses["vx"])
		copy(pool[32:], addresses["vy"])
		copy(pool[64:], addresses["mintx"])
		copy(pool[96:], addresses["minty"])
		_, pool[128] = derivePDA(addresses["pool"], program)
		ps := sbftest.FixtureState{Data: hex.EncodeToString(pool), Owner: s.ProgramID, Lamports: rent(137), RentEpoch: exemptEpoch}
		states["pool"] = &ps
		s.Accounts = append(s.Accounts, sbftest.FixtureAccount{Name: name("pool"), Address: b58(addresses["pool"]), Initial: &ps})
		meta := func(n string, signer, writable bool) sbftest.FixtureMeta {
			return sbftest.FixtureMeta{Account: name(n), Signer: signer, Writable: writable}
		}
		ix := func(program string, data []byte, metas ...sbftest.FixtureMeta) sbftest.FixtureInstruction {
			return sbftest.FixtureInstruction{Program: program, Data: hex.EncodeToString(data), Accounts: metas}
		}
		clone := func(n string) *sbftest.FixtureState {
			if states[n] == nil {
				return nil
			}
			v := *states[n]
			return &v
		}
		checks := func(names ...string) []sbftest.FixtureCheck {
			var out []sbftest.FixtureCheck
			for _, n := range names {
				v := clone(n)
				out = append(out, sbftest.FixtureCheck{Account: name(n), State: v, Absent: v == nil})
			}
			return out
		}
		c := sbftest.FixtureCase{Name: prefix}
		step := func(errorJSON string, instructions []sbftest.FixtureInstruction, checked ...string) {
			c.Steps = append(c.Steps, sbftest.FixtureStep{Instructions: instructions, Expect: sbftest.FixtureExpectation{Error: json.RawMessage(errorJSON), Accounts: checks(checked...)}})
		}
		create := func(n string, size, balance uint64, signer bool) sbftest.FixtureInstruction {
			data := append([]byte{0, 0, 0, 0}, words(balance, size)...)
			data = append(data, un58(tokenAddress)...)
			return ix("system", data, meta("funder", true, true), meta(n, signer, true))
		}
		initMint := func(n string) sbftest.FixtureInstruction {
			data := append([]byte{20, 6}, addresses["user"]...)
			data = append(data, 0)
			return ix("token", data, meta(n, false, true))
		}
		initAccount := func(n, mint, auth string) sbftest.FixtureInstruction {
			return ix("token", append([]byte{18}, addresses[auth]...), meta(n, false, true), meta(mint, false, false))
		}
		spend := func(balance uint64) { v := clone("funder"); v.Lamports -= balance; states["funder"] = v }
		blank := func(size, balance uint64) *sbftest.FixtureState {
			return &sbftest.FixtureState{Data: strings.Repeat("00", int(size)), Owner: tokenAddress, Lamports: balance, RentEpoch: createdEpoch}
		}
		mintState := func(balance uint64) *sbftest.FixtureState {
			v := blank(82, balance)
			data := make([]byte, 82)
			binary.LittleEndian.PutUint32(data, 1)
			copy(data[4:], addresses["user"])
			data[44], data[45] = 6, 1
			v.Data = hex.EncodeToString(data)
			return v
		}
		tokenState := func(mint, auth string, balance uint64) *sbftest.FixtureState {
			v := blank(165, balance)
			data := make([]byte, 165)
			copy(data, addresses[mint])
			copy(data[32:], addresses[auth])
			data[108] = 1
			v.Data = hex.EncodeToString(data)
			return v
		}
		amount := func(n string, offset int, value uint64) {
			v := clone(n)
			data, _ := hex.DecodeString(v.Data)
			binary.LittleEndian.PutUint64(data[offset:], value)
			v.Data = hex.EncodeToString(data)
			states[n] = v
		}
		if prefix == "nosigner" {
			step(`{"InstructionError":[0,"MissingRequiredSignature"]}`, []sbftest.FixtureInstruction{create("ux", 165, rent(165), false)}, "funder", "ux")
		} else if prefix == "badrent" {
			// A rent-paying new account is rejected by the transaction runtime.
			step(`{"InsufficientFundsForRent":{"account_index":2}}`, []sbftest.FixtureInstruction{create("ux", 165, rent(165)-1, true)}, "funder", "ux")
		} else if prefix == "badsize" || prefix == "badinitrent" {
			states["mintx"] = mintState(rent(82))
			spend(rent(82))
			step("null", []sbftest.FixtureInstruction{create("mintx", 82, rent(82), true), initMint("mintx")}, "funder", "mintx")
			if prefix == "badsize" {
				step(`{"InstructionError":[1,"InvalidAccountData"]}`, []sbftest.FixtureInstruction{create("ux", 164, rent(164), true), initAccount("ux", "mintx", "user")}, "funder", "ux", "mintx")
			} else {
				step(`{"InstructionError":[1,{"Custom":0}]}`, []sbftest.FixtureInstruction{create("ux", 165, rent(165)-1, true), initAccount("ux", "mintx", "user")}, "funder", "ux", "mintx")
			}
		} else if prefix == "split" {
			// Cover native transfer, allocate, and assign separately. Ordinary authoring
			// should create+initialize in one transaction; this is a runtime probe.
			transfer := ix("system", append([]byte{2, 0, 0, 0}, words(rent(165))...), meta("funder", true, true), meta("ux", false, true))
			spend(rent(165))
			states["ux"] = &sbftest.FixtureState{Owner: systemAddress, Lamports: rent(165), RentEpoch: createdEpoch}
			step("null", []sbftest.FixtureInstruction{transfer}, "funder", "ux")
			v := clone("ux")
			v.Data = strings.Repeat("00", 165)
			states["ux"] = v
			step("null", []sbftest.FixtureInstruction{ix("system", append([]byte{8, 0, 0, 0}, words(165)...), meta("ux", true, true))}, "ux")
			v = clone("ux")
			v.Owner = tokenAddress
			states["ux"] = v
			step("null", []sbftest.FixtureInstruction{ix("system", append([]byte{1, 0, 0, 0}, un58(tokenAddress)...), meta("ux", true, true))}, "ux")
		} else {
			c.Name = "create-mint-swap-drain-close-recreate"
			for _, n := range []string{"mintx", "minty"} {
				states[n] = mintState(rent(82))
				spend(rent(82))
				step("null", []sbftest.FixtureInstruction{create(n, 82, rent(82), true), initMint(n)}, "funder", n)
			}
			for _, spec := range [][3]string{{"ux", "mintx", "user"}, {"vx", "mintx", "pda"}, {"vy", "minty", "pda"}, {"uy", "minty", "user"}} {
				n, mint, auth := spec[0], spec[1], spec[2]
				states[n] = tokenState(mint, auth, rent(165))
				spend(rent(165))
				step("null", []sbftest.FixtureInstruction{create(n, 165, rent(165), true), initAccount(n, mint, auth)}, "funder", n, mint)
			}
			step(`{"InstructionError":[0,{"Custom":0}]}`, []sbftest.FixtureInstruction{create("ux", 165, rent(165), true)}, "funder", "ux")
			step(`{"InstructionError":[0,{"Custom":6}]}`, []sbftest.FixtureInstruction{initAccount("ux", "mintx", "user")}, "ux", "mintx")
			mintTo := func(mint, dest, auth string, value uint64) sbftest.FixtureInstruction {
				return ix("token", append([]byte{7}, words(value)...), meta(mint, false, true), meta(dest, false, true), meta(auth, true, false))
			}
			step(`{"InstructionError":[0,{"Custom":4}]}`, []sbftest.FixtureInstruction{mintTo("mintx", "ux", "wrong", 1)}, "mintx", "ux")
			amount("mintx", 36, 2000000)
			amount("minty", 36, 2000000)
			amount("ux", 64, 1000000)
			amount("vx", 64, 1000000)
			amount("vy", 64, 2000000)
			step("null", []sbftest.FixtureInstruction{mintTo("mintx", "ux", "user", 1000000), mintTo("mintx", "vx", "user", 1000000), mintTo("minty", "vy", "user", 2000000)}, "mintx", "minty", "ux", "vx", "vy")
			out := wideReference(1000000, 2000000, 10000)
			amount("ux", 64, 990000)
			amount("vx", 64, 1010000)
			amount("vy", 64, 2000000-out)
			amount("uy", 64, out)
			amount("pool", 129, 1)
			swap := ix("program", words(10000, 0), meta("pool", false, true), meta("user", true, false), meta("ux", false, true), meta("vx", false, true), meta("vy", false, true), meta("uy", false, true), meta("pda", false, false), sbftest.FixtureMeta{Account: "token"})
			step("null", []sbftest.FixtureInstruction{swap}, "pool", "ux", "vx", "vy", "uy", "mintx", "minty")
			c.Steps[len(c.Steps)-1].Expect.LogCounts = map[string]int{"Program " + tokenAddress + " invoke [2]": 2, "Program " + tokenAddress + " success": 2}
			close := func(auth string) sbftest.FixtureInstruction {
				return ix("token", []byte{9}, meta("uy", false, true), meta("receiver", false, true), meta(auth, true, false))
			}
			step(`{"InstructionError":[0,{"Custom":11}]}`, []sbftest.FixtureInstruction{close("user")}, "uy", "receiver")
			drain := ix("token", append([]byte{3}, words(out)...), meta("uy", false, true), meta("vy", false, true), meta("user", true, false))
			amount("uy", 64, 0)
			amount("vy", 64, 2000000)
			step("null", []sbftest.FixtureInstruction{drain}, "uy", "vy")
			step(`{"InstructionError":[0,{"Custom":4}]}`, []sbftest.FixtureInstruction{close("wrong")}, "uy", "receiver")
			// A close followed by failing token instruction must restore account data,
			// ownership and refunded lamports, not only its token balance.
			step(`{"InstructionError":[1,"InvalidAccountData"]}`, []sbftest.FixtureInstruction{close("user"), drain}, "uy", "receiver", "vy")
			refund := states["uy"].Lamports
			states["uy"] = nil
			v := clone("receiver")
			v.Lamports += refund
			states["receiver"] = v
			step("null", []sbftest.FixtureInstruction{close("user")}, "uy", "receiver")
			// This repeats the earlier rejected close after state changes; give
			// it a new signature explicitly instead of hiding duplicate checks.
			c.Steps[len(c.Steps)-1].FreshBlockhash = true
			// The validator returns a tombstone in simulation, then removes the
			// zero-lamport account on commit. Both phases are checked exactly.
			c.Steps[len(c.Steps)-1].Expect.SimulationAccounts = []sbftest.FixtureCheck{{Account: name("uy"), State: &sbftest.FixtureState{Owner: systemAddress, RentEpoch: createdEpoch}}}
			// Extra lamport makes recreation distinct under an unchanged blockhash.
			states["uy"] = tokenState("minty", "user", rent(165)+1)
			spend(rent(165) + 1)
			step("null", []sbftest.FixtureInstruction{create("uy", 165, rent(165)+1, true), initAccount("uy", "minty", "user")}, "funder", "uy", "minty", "receiver")
		}
		s.Cases = append(s.Cases, c)
	}
	return s
}

func runLifecycle() error {
	if testvmRunner := os.Getenv("GOSVM_TEST_RUNNER"); testvmRunner != "" {
		return fmt.Errorf("lifecycle oracle requires the real local validator; unset GOSVM_TEST_RUNNER")
	}
	build, e := filepath.Abs(verificationBuild())
	if e != nil {
		return e
	}
	if e = os.MkdirAll(build, 0755); e != nil {
		return e
	}
	version, e := exec.Command("solana-test-validator", "--version").Output()
	if e != nil {
		return e
	}
	if e = testvm.CheckVersion(string(version)); e != nil {
		return e
	}
	mainELF := filepath.Join(build, "go-token.so")
	image, e := os.ReadFile(mainELF)
	if e != nil {
		return e
	}
	if len(image) < 64 || !bytes.Equal(image[:4], []byte{127, 'E', 'L', 'F'}) || binary.LittleEndian.Uint32(image[48:52]) != 3 {
		return fmt.Errorf("main program must be SBF v3")
	}
	tmp, e := os.MkdirTemp(build, "lifecycle-validator-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(tmp)
	seedDir := filepath.Join(tmp, "accounts")
	if e = os.Mkdir(seedDir, 0700); e != nil {
		return e
	}
	suite := lifecycleSuite(strings.Repeat("00", 32), exemptEpoch)
	for _, a := range suite.Accounts {
		if a.Initial == nil {
			continue
		}
		data, _ := hex.DecodeString(a.Initial.Data)
		state := a.Initial
		b, _ := json.Marshal(map[string]any{"pubkey": a.Address, "account": account{Data: []string{base64.StdEncoding.EncodeToString(data), "base64"}, Owner: state.Owner, Lamports: state.Lamports, Executable: state.Executable, RentEpoch: state.RentEpoch}})
		if e = os.WriteFile(filepath.Join(seedDir, a.Name+".json"), b, 0600); e != nil {
			return e
		}
	}
	port := 0
	for i := 0; i < 100; i++ {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return err
		}
		p := l.Addr().(*net.TCPAddr).Port
		other, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p+1))
		l.Close()
		if err == nil {
			other.Close()
			port = p
			break
		}
	}
	if port == 0 {
		return fmt.Errorf("no free RPC port pair")
	}
	payer := ed25519.NewKeyFromSeed(key("lifecycle test payer - never fund"))
	log, e := os.Create(filepath.Join(build, "validator.log"))
	if e != nil {
		return e
	}
	defer log.Close()
	cmd := exec.Command("solana-test-validator", "--ledger", filepath.Join(tmp, "ledger"), "--rpc-port", fmt.Sprint(port), "--faucet-port", "0", "--bind-address", "127.0.0.1", "--mint", b58(payer.Public().(ed25519.PublicKey)), "--account-dir", seedDir, "--bpf-program", suite.ProgramID, mainELF, "--quiet")
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
		if b, err := os.ReadFile(filepath.Join(tmp, "ledger/validator.log")); err == nil {
			os.WriteFile(filepath.Join(build, "validator-detail.log"), b, 0644)
		}
	}()
	rpc := rpcClient{url: fmt.Sprintf("http://127.0.0.1:%d", port), client: http.Client{Timeout: 3 * time.Second}}
	ready := false
	for deadline := time.Now().Add(50 * time.Second); time.Now().Before(deadline); {
		var slot uint64
		if rpc.call("getSlot", []any{map[string]any{"commitment": "confirmed"}}, &slot) == nil && slot >= 2 {
			ready = true
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !ready {
		return fmt.Errorf("validator readiness timeout")
	}
	// Capture exactly the builtin SPL image and active feature IDs for LiteSVM.
	var token struct{ Value *account }
	if e = rpc.call("getAccountInfo", []any{tokenAddress, map[string]any{"encoding": "base64", "commitment": "confirmed"}}, &token); e != nil {
		return e
	}
	if token.Value == nil || !token.Value.Executable {
		return fmt.Errorf("missing SPL Token")
	}
	tokenImage, e := base64.StdEncoding.DecodeString(token.Value.Data[0])
	if e != nil {
		return e
	}
	if token.Value.Owner == "BPFLoaderUpgradeab1e11111111111111111111111" {
		if len(tokenImage) != 36 || binary.LittleEndian.Uint32(tokenImage) != 2 {
			return fmt.Errorf("bad token program account")
		}
		var pd struct{ Value *account }
		if e = rpc.call("getAccountInfo", []any{b58(tokenImage[4:]), map[string]any{"encoding": "base64", "commitment": "confirmed"}}, &pd); e != nil {
			return e
		}
		if pd.Value == nil {
			return fmt.Errorf("missing token ProgramData")
		}
		tokenImage, e = base64.StdEncoding.DecodeString(pd.Value.Data[0])
		if e != nil || len(tokenImage) < 45 {
			return fmt.Errorf("bad token ProgramData")
		}
		tokenImage = tokenImage[45:]
	}
	if len(tokenImage) < 4 || !bytes.Equal(tokenImage[:4], []byte{127, 'E', 'L', 'F'}) {
		return fmt.Errorf("bad token ELF")
	}
	if e = exclusiveFixtureFile(filepath.Join(build, "spl-token.so"), tokenImage); e != nil {
		return e
	}
	suite = lifecycleSuite(fmt.Sprintf("%x", sha256.Sum256(tokenImage)), exemptEpoch)
	var features []struct {
		Pubkey  string
		Account account
	}
	if e = rpc.call("getProgramAccounts", []any{"Feature111111111111111111111111111111111111", map[string]any{"encoding": "base64", "commitment": "confirmed"}}, &features); e != nil {
		return e
	}
	active := []string{}
	for _, f := range features {
		d, err := base64.StdEncoding.DecodeString(f.Account.Data[0])
		if err != nil {
			return err
		}
		if len(d) == 9 && d[0] == 1 {
			active = append(active, f.Pubkey)
		}
	}
	sort.Strings(active)
	for _, size := range []uint64{82, 137, 165} {
		var balance uint64
		if e = rpc.call("getMinimumBalanceForRentExemption", []any{size}, &balance); e != nil {
			return e
		}
		if balance != (size+128)*3480*2 {
			return fmt.Errorf("unexpected rent for %d: %d", size, balance)
		}
	}
	addresses := map[string]string{}
	for _, a := range suite.Accounts {
		addresses[a.Name] = a.Address
	}
	var rows []sbftest.FixtureCaseResult
	for ci := range suite.Cases {
		c := &suite.Cases[ci]
		row := sbftest.FixtureCaseResult{Name: c.Name}
		previousHash := ""
		for si := range c.Steps {
			step := &c.Steps[si]
			label := fmt.Sprintf("%s step %d", c.Name, si+1)
			var latest struct{ Value struct{ Blockhash string } }
			if e = rpc.call("getLatestBlockhash", []any{map[string]any{"commitment": "confirmed"}}, &latest); e != nil {
				return e
			}
			if step.FreshBlockhash {
				deadline := time.Now().Add(20 * time.Second)
				for latest.Value.Blockhash == previousHash {
					if time.Now().After(deadline) {
						return fmt.Errorf("%s fresh blockhash timeout", label)
					}
					time.Sleep(100 * time.Millisecond)
					if e = rpc.call("getLatestBlockhash", []any{map[string]any{"commitment": "confirmed"}}, &latest); e != nil {
						return e
					}
				}
			}
			previousHash = latest.Value.Blockhash
			tx, _, err := suite.Transaction(latest.Value.Blockhash, step.Instructions)
			if err != nil {
				return err
			}
			watched := []string{}
			for _, check := range step.Expect.Accounts {
				watched = append(watched, addresses[check.Account])
			}
			var sim struct {
				Value struct {
					Err           json.RawMessage
					UnitsConsumed uint64
					Accounts      []*account
					Logs          []string
				}
			}
			if e = rpc.call("simulateTransaction", []any{tx, map[string]any{"encoding": "base64", "sigVerify": true, "commitment": "confirmed", "accounts": map[string]any{"encoding": "base64", "addresses": watched}}}, &sim); e != nil {
				return e
			}
			if string(sim.Value.Err) != string(step.Expect.Error) {
				return fmt.Errorf("%s: error got %s want %s logs=%v", label, sim.Value.Err, step.Expect.Error, sim.Value.Logs)
			}
			for fragment, want := range step.Expect.LogCounts {
				got := 0
				for _, line := range sim.Value.Logs {
					if strings.Contains(line, fragment) {
						got++
					}
				}
				if got != want {
					return fmt.Errorf("%s: log %q count %d want %d", label, fragment, got, want)
				}
			}
			if string(step.Expect.Error) == "null" {
				if len(sim.Value.Accounts) != len(step.Expect.Accounts) {
					return fmt.Errorf("%s missing simulated accounts", label)
				}
				for i, check := range step.Expect.Accounts {
					if e = lifecycleCheck(label+" simulation", step.Expect.SimulationCheck(check), sim.Value.Accounts[i]); e != nil {
						return e
					}
				}
			}
			if e = submitToken(&rpc, tx, string(step.Expect.Error)); e != nil {
				return fmt.Errorf("%s submit: %w", label, e)
			}
			for _, check := range step.Expect.Accounts {
				var got struct{ Value *account }
				if e = rpc.call("getAccountInfo", []any{addresses[check.Account], map[string]any{"encoding": "base64", "commitment": "confirmed"}}, &got); e != nil {
					return e
				}
				if e = lifecycleCheck(label+" committed", check, got.Value); e != nil {
					return e
				}
			}
			compute := sim.Value.UnitsConsumed
			step.Expect.CU = &compute
			row.Steps = append(row.Steps, sbftest.FixtureStepResult{CU: compute, Error: sim.Value.Err, Logs: sim.Value.Logs, Committed: true})
			fmt.Printf("PASS %-48s %d CU\n", label, compute)
		}
		rows = append(rows, row)
	}
	// LiteSVM initializes missing account rent_epoch to zero, whereas the bank
	// uses u64::MAX. Keep this measured metadata difference explicit; both
	// fixtures check every byte and field rather than ignoring the field.
	lite := lifecycleSuite(suite.Programs[0].SHA256, 0)
	for ci := range lite.Cases {
		for si := range lite.Cases[ci].Steps {
			lite.Cases[ci].Steps[si].Expect.CU = suite.Cases[ci].Steps[si].Expect.CU
		}
	}
	report := map[string]any{"schema": 1, "passed": true, "validator": strings.TrimSpace(string(version)), "cases": rows, "active_features": active, "elf_sha256": fmt.Sprintf("%x", sha256.Sum256(image)), "token_sha256": suite.Programs[0].SHA256, "reference": "independent classic SPL layouts, default Rent checked via RPC, and big-integer swap math; full state and metadata",
		"known_metadata_difference": map[string]any{"field": "rent_epoch", "new_account_validator": exemptEpoch, "new_account_litesvm": uint64(0)}}
	for filename, value := range map[string]any{"fixtures.json": lite, "validator-fixtures.json": suite, "validator-results.json": report, "validator-features.json": active} {
		b, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return err
		}
		if e = exclusiveFixtureFile(filepath.Join(build, filename), append(b, '\n')); e != nil {
			return e
		}
	}
	fmt.Printf("PASS: %d lifecycle scenarios; fresh validator simulation, committed state and rollback checked.\n", len(rows))
	return nil
}

func lifecycleCheck(label string, check sbftest.FixtureCheck, got *account) error {
	if check.Absent {
		if got != nil {
			return fmt.Errorf("%s %s: expected absent, got %+v", label, check.Account, *got)
		}
		return nil
	}
	if got == nil || check.State == nil {
		return fmt.Errorf("%s %s: missing account/state", label, check.Account)
	}
	want := check.State
	if len(got.Data) != 2 || got.Data[1] != "base64" {
		return fmt.Errorf("%s %s: require base64 account data", label, check.Account)
	}
	data, e := base64.StdEncoding.DecodeString(got.Data[0])
	if e != nil {
		return e
	}
	if hex.EncodeToString(data) != want.Data || got.Owner != want.Owner || got.Lamports != want.Lamports || got.Executable != want.Executable || got.RentEpoch != want.RentEpoch {
		return fmt.Errorf("%s %s: full state mismatch\ngot %+v\nwant %+v", label, check.Account, *got, *want)
	}
	return nil
}
