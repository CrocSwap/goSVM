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
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gosvm/internal/testvm"
)

const tokenAddress = "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA"

// PDA derivation for deterministic local fixtures. Decompression tests whether
// the compressed Edwards-Y hash encodes an on-curve point, using math/big.
func onCurve(b []byte) bool {
	yBytes := bytes.Clone(b)
	yBytes[31] &= 127
	for i, j := 0, 31; i < j; i, j = i+1, j-1 {
		yBytes[i], yBytes[j] = yBytes[j], yBytes[i]
	}
	y := new(big.Int).SetBytes(yBytes)
	p := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 255), big.NewInt(19))
	d := new(big.Int).ModInverse(big.NewInt(121666), p)
	d.Mul(d, big.NewInt(-121665)).Mod(d, p)
	y2 := new(big.Int).Mul(y, y)
	y2.Mod(y2, p)
	num := new(big.Int).Sub(y2, big.NewInt(1))
	den := new(big.Int).Mul(d, y2)
	den.Add(den, big.NewInt(1))
	inv := new(big.Int).ModInverse(den, p)
	if inv == nil {
		return false
	}
	x2 := new(big.Int).Mul(num, inv)
	x2.Mod(x2, p)
	if x2.Sign() == 0 {
		return true
	}
	e := new(big.Int).Rsh(new(big.Int).Sub(p, big.NewInt(1)), 1)
	return new(big.Int).Exp(x2, e, p).Cmp(big.NewInt(1)) == 0
}
func derivePDA(seed, program []byte) ([]byte, byte) {
	for bump := 255; bump >= 0; bump-- {
		data := append(bytes.Clone(seed), byte(bump))
		data = append(data, program...)
		data = append(data, []byte("ProgramDerivedAddress")...)
		h := sha256.Sum256(data)
		if !onCurve(h[:]) {
			return h[:], byte(bump)
		}
	}
	panic("no PDA bump")
}

type tokenVector struct {
	name                                         string
	x, y, amount, minimum, userX, userY, counter uint64
	mutation                                     string
	code                                         uint64
}
type tokenFixture struct {
	keys       [][]byte
	accounts   []account
	data       [][]byte
	writable   []bool
	userSigner bool
	ix         []byte
	count      int
}

func newTokenFixture(backend, index int, program, user []byte, v tokenVector) tokenFixture {
	f := tokenFixture{keys: make([][]byte, 8), accounts: make([]account, 8), data: make([][]byte, 8), writable: []bool{true, false, true, true, true, true, false, false}, userSigner: true, ix: words(v.amount, v.minimum), count: 8}
	for i := range f.keys {
		f.keys[i] = key(fmt.Sprintf("tokens-%d-%d-%d", backend, index, i))
	}
	f.keys[1] = user
	f.keys[7] = un58(tokenAddress)
	pda, bump := derivePDA(f.keys[0], program)
	f.keys[6] = pda
	mintX, mintY := key("experiment mint X"), key("experiment mint Y")
	state := make([]byte, 137)
	copy(state, f.keys[3])
	copy(state[32:], f.keys[4])
	copy(state[64:], mintX)
	copy(state[96:], mintY)
	state[128] = bump
	binary.LittleEndian.PutUint64(state[129:], v.counter)
	f.data[0] = state
	for i := 0; i < 8; i++ {
		f.accounts[i] = account{Owner: b58(make([]byte, 32)), Lamports: 10000000}
		f.data[i] = []byte{}
	}
	f.data[0] = state
	f.accounts[0].Owner = b58(program)
	for i := 2; i <= 5; i++ {
		d := make([]byte, 165)
		f.data[i] = d
		f.accounts[i].Owner = tokenAddress
		mint := mintX
		if i >= 4 {
			mint = mintY
		}
		copy(d, mint)
		auth := pda
		if i == 2 || i == 5 {
			auth = user
		}
		copy(d[32:], auth)
		d[108] = 1
	}
	for i, v := range map[int]uint64{2: v.userX, 3: v.x, 4: v.y, 5: v.userY} {
		binary.LittleEndian.PutUint64(f.data[i][64:], v)
	}
	switch v.mutation {
	case "no-accounts":
		f.count = 0
	case "missing-account":
		f.count = 7
	case "extra-account":
		f.count = 9
	case "account-limit":
		f.count = 17
	case "long-pool":
		f.data[0] = append(f.data[0], 0)
	case "pool-owner":
		f.accounts[0].Owner = b58(key("foreign program"))
	case "pool-readonly":
		f.writable[0] = false
	case "pool-length":
		f.data[0] = f.data[0][:136]
	case "no-signer":
		f.userSigner = false
	case "fake-program":
		f.keys[7] = key("fake token program")
	case "alias":
		f.keys[5] = f.keys[2]
	case "vault-substitute":
		f.keys[3] = key(fmt.Sprintf("substitute-%d-%d", backend, index))
	case "token-owner":
		f.accounts[2].Owner = b58(key("foreign program"))
	case "token-length":
		f.data[5] = f.data[5][:164]
	case "token-readonly":
		f.writable[4] = false
	case "uninitialized":
		f.data[2][108] = 0
	case "native-token":
		f.data[2][109] = 1
	case "mint":
		f.data[5][0] ^= 1
	case "same-mint":
		copy(f.data[0][96:128], f.data[0][64:96])
	case "user-authority":
		f.data[2][32] ^= 1
	case "vault-authority":
		f.data[4][32] ^= 1
	case "pda":
		f.keys[6] = key("wrong PDA")
		copy(f.data[3][32:], f.keys[6])
		copy(f.data[4][32:], f.keys[6])
	case "bump":
		f.data[0][128]--
	case "input-frozen":
		f.data[2][108] = 2
	case "output-frozen":
		f.data[5][108] = 2
	case "short-ix":
		f.ix = f.ix[:15]
	case "long-ix":
		f.ix = append(f.ix, 0)
	}
	return f
}

// Generic transaction construction merges duplicate account references just as a
// Solana client does, preserving duplicate references in the instruction itself.
func tokenTransaction(payer, user ed25519.PrivateKey, program, hash []byte, f tokenFixture, instructions [][]byte) string {
	type meta struct {
		k                []byte
		signer, writable bool
	}
	entries := []meta{{payer.Public().(ed25519.PublicKey), true, true}}
	add := func(k []byte, signer, writable bool) {
		for i := range entries {
			if bytes.Equal(entries[i].k, k) {
				entries[i].signer = entries[i].signer || signer
				entries[i].writable = entries[i].writable || writable
				return
			}
		}
		entries = append(entries, meta{k, signer, writable})
	}
	for i, k := range f.keys {
		add(k, i == 1 && f.userSigner, f.writable[i])
	}
	add(program, false, false)
	sort.SliceStable(entries[1:], func(i, j int) bool {
		a, b := entries[i+1], entries[j+1]
		if a.signer != b.signer {
			return a.signer
		}
		return a.writable && !b.writable
	})
	index := func(k []byte) byte {
		for i, m := range entries {
			if bytes.Equal(m.k, k) {
				return byte(i)
			}
		}
		panic("missing key")
	}
	nsig, roSig, roUnsigned := byte(0), byte(0), byte(0)
	for _, m := range entries {
		if m.signer {
			nsig++
			if !m.writable {
				roSig++
			}
		} else if !m.writable {
			roUnsigned++
		}
	}
	msg := []byte{nsig, roSig, roUnsigned}
	msg = append(msg, compact(len(entries))...)
	for _, m := range entries {
		msg = append(msg, m.k...)
	}
	msg = append(msg, hash...)
	msg = append(msg, compact(len(instructions))...)
	for _, ix := range instructions {
		msg = append(msg, index(program))
		msg = append(msg, compact(f.count)...)
		for j := 0; j < f.count; j++ {
			msg = append(msg, index(f.keys[j%len(f.keys)]))
		}
		msg = append(msg, compact(len(ix))...)
		msg = append(msg, ix...)
	}
	wire := compact(int(nsig))
	for _, m := range entries {
		if !m.signer {
			continue
		}
		signer := payer
		if bytes.Equal(m.k, user.Public().(ed25519.PublicKey)) {
			signer = user
		}
		wire = append(wire, ed25519.Sign(signer, msg)...)
	}
	wire = append(wire, msg...)
	return base64.StdEncoding.EncodeToString(wire)
}

func wideReference(x, y, amount uint64) uint64 {
	if x == 0 || y == 0 || amount == 0 || amount > ^uint64(0)-x {
		return 0
	}
	fee := new(big.Int).Mul(new(big.Int).SetUint64(amount), big.NewInt(30))
	fee.Add(fee, big.NewInt(9999)).Div(fee, big.NewInt(10000))
	net := new(big.Int).Sub(new(big.Int).SetUint64(amount), fee)
	num := new(big.Int).Mul(new(big.Int).SetUint64(y), net)
	den := new(big.Int).Add(new(big.Int).SetUint64(x), net)
	return num.Div(num, den).Uint64()
}

func runTokens() error { return runTokenComparison(false) }

func runTokenComparison(anchor bool) error {
	started := time.Now()
	version, err := testvm.Command("--version").CombinedOutput()
	if err != nil {
		return err
	}
	if err = testvm.CheckVersion(string(version)); err != nil {
		return err
	}
	build, err := filepath.Abs(verificationBuild())
	if err != nil {
		return err
	}
	programs := [][]byte{key("tokenswap Go"), key("tokenswap Rust")}
	names := []string{"go", "rust"}
	files := []string{"tokenswap-go.so", "tokenswap_rust.so"}
	if anchor {
		build = filepath.Join(build, "anchor")
		programs = append(programs, key("tokenswap Anchor"))
		names = []string{"go", "lean-rust", "anchor"}
		files = []string{"go-token.so", "anchor_lean_token.so", "anchor_token_bench.so"}
	}
	hashes := map[string]string{}
	for _, name := range files {
		b, e := os.ReadFile(filepath.Join(build, name))
		if e != nil {
			return e
		}
		if len(b) < 64 || !bytes.Equal(b[:4], []byte{127, 'E', 'L', 'F'}) || binary.LittleEndian.Uint32(b[48:52]) != 3 {
			return fmt.Errorf("%s must be SBF v3", name)
		}
		hashes[name] = fmt.Sprintf("%x", sha256.Sum256(b))
	}
	base := tokenVector{name: "wide-swap", x: 1e18, y: 2e18, amount: 1e15, userX: 1e18}
	vectors := []tokenVector{base}
	add := func(name, mutation string, code uint64) {
		v := base
		v.name, v.mutation, v.code = name, mutation, code
		vectors = append(vectors, v)
	}
	add("no-accounts", "no-accounts", 101)
	add("missing-account", "missing-account", 101)
	add("extra-account", "extra-account", 101)
	add("account-limit", "account-limit", 1001)
	add("long-pool", "long-pool", 102)
	if anchor {
		add("pool-discriminator", "pool-discriminator", 102)
		add("instruction-discriminator", "instruction-discriminator", 102)
	}
	for _, item := range []struct {
		mutation string
		code     uint64
	}{{"pool-owner", 103}, {"pool-readonly", 103}, {"pool-length", 102}, {"no-signer", 104}, {"fake-program", 105}, {"alias", 106}, {"vault-substitute", 107}, {"token-owner", 108}, {"token-length", 108}, {"token-readonly", 108}, {"uninitialized", 108}, {"native-token", 108}, {"mint", 109}, {"same-mint", 109}, {"user-authority", 110}, {"vault-authority", 110}, {"pda", 111}, {"bump", 111}, {"input-frozen", 17}, {"output-frozen", 17}, {"short-ix", 102}, {"long-ix", 102}} {
		add(item.mutation, item.mutation, item.code)
	}
	for _, v := range []tokenVector{
		{name: "zero-input", x: 1, y: 2, amount: 0, userX: 1, code: 112},
		{name: "empty-pool", x: 0, y: 2, amount: 1, userX: 1, code: 112},
		{name: "input-overflow", x: 1, y: 2, amount: ^uint64(0), userX: ^uint64(0), code: 112},
		{name: "insufficient-input", x: 10000, y: 20000, amount: 1000, userX: 999, code: 112},
		{name: "fee-rounding-zero", x: 1000, y: 2000, amount: 1, userX: 1, code: 113},
		{name: "slippage", x: 1e18, y: 2e18, amount: 1e15, userX: 1e18, minimum: ^uint64(0), code: 113},
		{name: "output-overflow", x: 1e18, y: 2e18, amount: 1e15, userX: 1e18, userY: ^uint64(0), code: 113},
		{name: "counter-overflow", x: 1e18, y: 2e18, amount: 1e15, userX: 1e18, counter: ^uint64(0), code: 114},
		{name: "maximum-input", x: 1, y: ^uint64(0), amount: ^uint64(0) - 1, userX: ^uint64(0) - 1},
		{name: "near-limit-reserve", x: ^uint64(0) - 100000, y: ^uint64(0), amount: 100000, userX: 100000},
		{name: "small-swap", x: 1000000, y: 2000000, amount: 10000, userX: 1000000},
	} {
		vectors = append(vectors, v)
	}
	rng := rand.New(rand.NewSource(20261004))
	for i := 0; i < 64; i++ {
		v := tokenVector{name: fmt.Sprintf("random-%02d", i), x: 1 + (rng.Uint64() >> 2), y: 1 + (rng.Uint64() >> 1), amount: 1 + (rng.Uint64() >> 3), userX: ^uint64(0)}
		if wideReference(v.x, v.y, v.amount) == 0 {
			v.code = 113
		}
		vectors = append(vectors, v)
	}
	tmp, err := os.MkdirTemp(build, "token-validator-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	dir := filepath.Join(tmp, "accounts")
	must(os.Mkdir(dir, 0755))
	payer := ed25519.NewKeyFromSeed(key("token test fee payer - never fund"))
	user := ed25519.NewKeyFromSeed(key("token test owner - never fund"))
	fixtures := make([][]tokenFixture, len(programs))
	written := map[string]bool{}
	write := func(k []byte, a account, data []byte) {
		address := b58(k)
		if written[address] {
			return
		}
		written[address] = true
		a.Data = []string{base64.StdEncoding.EncodeToString(data), "base64"}
		b, _ := json.Marshal(map[string]any{"pubkey": address, "account": a})
		must(os.WriteFile(filepath.Join(dir, address+".json"), b, 0644))
	}
	for backend, program := range programs {
		for i, v := range vectors {
			f := newTokenFixture(backend, i, program, user.Public().(ed25519.PublicKey), v)
			if anchor {
				f.data[0] = append(anchorTag("account:Pool"), f.data[0]...)
				f.ix = append(anchorTag("global:swap"), f.ix...)
				if v.mutation == "pool-discriminator" {
					f.data[0][0] ^= 1
				}
				if v.mutation == "instruction-discriminator" {
					f.ix[0] ^= 1
				}
			}
			fixtures[backend] = append(fixtures[backend], f)
			for j, k := range f.keys {
				if j == 7 && v.mutation != "fake-program" {
					continue
				}
				write(k, f.accounts[j], f.data[j])
			}
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
		return fmt.Errorf("no RPC port")
	}
	log, err := os.Create(filepath.Join(build, "token-validator.log"))
	if err != nil {
		return err
	}
	defer log.Close()
	validatorArgs := []string{"--ledger", filepath.Join(tmp, "ledger"), "--rpc-port", fmt.Sprint(port), "--faucet-port", "0", "--bind-address", "127.0.0.1", "--mint", b58(payer.Public().(ed25519.PublicKey)), "--account-dir", dir, "--quiet"}
	for i, program := range programs {
		validatorArgs = append(validatorArgs, "--bpf-program", b58(program), filepath.Join(build, files[i]))
	}
	startup := time.Now()
	cmd := testvm.Command(validatorArgs...)
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
			os.WriteFile(filepath.Join(build, "token-validator-detail.log"), b, 0644)
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
		time.Sleep(testvm.ReadyPoll())
	}
	if !ready {
		return fmt.Errorf("validator startup timeout; see build/token-validator-detail.log")
	}
	startupSeconds := time.Since(startup).Seconds()
	execution := time.Now()
	var token struct{ Value *account }
	must(c.call("getAccountInfo", []any{tokenAddress, map[string]any{"encoding": "base64", "commitment": "confirmed"}}, &token))
	if token.Value == nil || !token.Value.Executable {
		return fmt.Errorf("validator has no executable SPL Token program")
	}
	tokenBytes, err := base64.StdEncoding.DecodeString(token.Value.Data[0])
	if err != nil {
		return err
	}
	if token.Value.Owner == "BPFLoaderUpgradeab1e11111111111111111111111" {
		if len(tokenBytes) != 36 || binary.LittleEndian.Uint32(tokenBytes) != 2 {
			return fmt.Errorf("unexpected SPL Token program account")
		}
		var pd struct{ Value account }
		must(c.call("getAccountInfo", []any{b58(tokenBytes[4:]), map[string]any{"encoding": "base64", "commitment": "confirmed"}}, &pd))
		tokenBytes, err = base64.StdEncoding.DecodeString(pd.Value.Data[0])
		if err != nil {
			return err
		}
		if len(tokenBytes) < 45 {
			return fmt.Errorf("short Token programdata")
		}
		tokenBytes = tokenBytes[45:]
	}
	if len(tokenBytes) < 4 || !bytes.Equal(tokenBytes[:4], []byte{127, 'E', 'L', 'F'}) {
		return fmt.Errorf("SPL Token program data is not an ELF")
	}
	tokenHash := fmt.Sprintf("%x", sha256.Sum256(tokenBytes))
	if path := os.Getenv("GOSVM_CAPTURE_TOKEN_ELF"); path != "" && !testvm.IsRunner() {
		if err = os.WriteFile(path, tokenBytes, 0644); err != nil {
			return err
		}
	}
	var latest struct{ Value struct{ Blockhash string } }
	must(c.call("getLatestBlockhash", []any{map[string]any{"commitment": "confirmed"}}, &latest))
	hash := un58(latest.Value.Blockhash)
	rows := []result{}
	observedErrors := map[string]json.RawMessage{}
	for backend, program := range programs {
		for i, v := range vectors {
			f := fixtures[backend][i]
			tx := tokenTransaction(payer, user, program, hash, f, [][]byte{f.ix})
			addresses := []string{}
			for _, j := range []int{0, 2, 3, 4, 5} {
				addresses = append(addresses, b58(f.keys[j]))
			}
			var sim struct {
				Value struct {
					Err           json.RawMessage
					UnitsConsumed uint64
					Accounts      []*account
					Logs          []string
				}
			}
			must(c.call("simulateTransaction", []any{tx, map[string]any{"encoding": "base64", "sigVerify": true, "commitment": "confirmed", "accounts": map[string]any{"encoding": "base64", "addresses": addresses}}}, &sim))
			trace := strings.Join(sim.Value.Logs, "\n")
			observedErrors[names[backend]+"/"+v.name] = sim.Value.Err
			calls, success := strings.Count(trace, "Program "+tokenAddress+" invoke [2]"), strings.Count(trace, "Program "+tokenAddress+" success")
			wantCalls, wantSuccess := 0, 0
			if v.code == 0 {
				wantCalls, wantSuccess = 2, 2
			} else if v.mutation == "input-frozen" {
				wantCalls, wantSuccess = 1, 0
			} else if v.mutation == "output-frozen" {
				wantCalls, wantSuccess = 2, 1
			}
			if calls != wantCalls || success != wantSuccess {
				return fmt.Errorf("%s/%s unexpected CPI trace %d/%d (wanted %d/%d): %s", names[backend], v.name, calls, success, wantCalls, wantSuccess, trace)
			}
			if v.code == 0 {
				if string(sim.Value.Err) != "null" {
					return fmt.Errorf("%s/%s: %s\n%v", names[backend], v.name, sim.Value.Err, sim.Value.Logs)
				}
				out := wideReference(v.x, v.y, v.amount)
				expected := []uint64{v.counter + 1, v.userX - v.amount, v.x + v.amount, v.y - out, v.userY + out}
				for j, want := range expected {
					if j >= len(sim.Value.Accounts) || sim.Value.Accounts[j] == nil {
						return fmt.Errorf("missing simulated account")
					}
					data, e := base64.StdEncoding.DecodeString(sim.Value.Accounts[j].Data[0])
					must(e)
					offset := 64
					if j == 0 {
						offset = 129
						if anchor {
							offset += 8
						}
					}
					expectedBytes := bytes.Clone(f.data[[]int{0, 2, 3, 4, 5}[j]])
					binary.LittleEndian.PutUint64(expectedBytes[offset:], want)
					if !bytes.Equal(data, expectedBytes) {
						return fmt.Errorf("%s/%s account %d mismatch", names[backend], v.name, j)
					}
				}
			} else {
				var e struct{ InstructionError []json.RawMessage }
				must(json.Unmarshal(sim.Value.Err, &e))
				var custom struct{ Custom uint64 }
				if len(e.InstructionError) != 2 {
					return fmt.Errorf("%s/%s unexpected %s %v", names[backend], v.name, sim.Value.Err, sim.Value.Logs)
				}
				decodeErr := json.Unmarshal(e.InstructionError[1], &custom)
				builtinAllowed := anchor && names[backend] == "anchor" && ((v.mutation == "uninitialized" && string(e.InstructionError[1]) == `"UninitializedAccount"`) || (v.mutation == "token-length" && string(e.InstructionError[1]) == `"InvalidAccountData"`))
				if decodeErr != nil && !builtinAllowed {
					return fmt.Errorf("%s/%s unexpected error %s: %v\n%s", names[backend], v.name, sim.Value.Err, decodeErr, trace)
				}
				if custom.Custom == 0 && !builtinAllowed {
					return fmt.Errorf("%s/%s did not return a custom error: %s", names[backend], v.name, sim.Value.Err)
				}
				if anchor && names[backend] == "anchor" {
					wantCustom, wantBuiltin := anchorTokenError(v)
					if (wantBuiltin != "" && string(e.InstructionError[1]) != strconv.Quote(wantBuiltin)) || (wantBuiltin == "" && custom.Custom != wantCustom) {
						return fmt.Errorf("anchor/%s unexpected error %s (expected custom %d / %s)", v.name, sim.Value.Err, wantCustom, wantBuiltin)
					}
				}
				if !(anchor && names[backend] == "anchor") && custom.Custom != v.code {
					return fmt.Errorf("%s/%s expected %d got %s %v", names[backend], v.name, v.code, sim.Value.Err, sim.Value.Logs)
				}
			}
			var tokenCU uint64
			for _, line := range sim.Value.Logs {
				prefix := "Program " + tokenAddress + " consumed "
				if strings.HasPrefix(line, prefix) {
					var used uint64
					if _, err := fmt.Sscanf(strings.TrimPrefix(line, prefix), "%d", &used); err != nil {
						return err
					}
					tokenCU += used
				}
			}
			if v.code == 0 && (tokenCU == 0 || tokenCU >= sim.Value.UnitsConsumed) {
				return fmt.Errorf("missing/inconsistent Token compute logs")
			}
			rows = append(rows, result{Name: v.name, Backend: names[backend], CU: sim.Value.UnitsConsumed, Code: v.code, TokenCU: tokenCU})
		}
	}
	// Real committed swaps and a two-instruction failure, plus a failed second CPI.
	for backend, program := range programs {
		f := fixtures[backend][0]
		expected := []uint64{0, base.userX, base.x, base.y, base.userY}
		for step := 0; step < 3; step++ {
			data := words(base.amount+uint64(step), 0)
			if anchor {
				data = append(anchorTag("global:swap"), data...)
			}
			ixs := [][]byte{data}
			if step == 2 {
				bad := words(0, 0)
				if anchor {
					bad = append(anchorTag("global:swap"), bad...)
				}
				ixs = append(ixs, bad)
			}
			must(c.call("getLatestBlockhash", []any{map[string]any{"commitment": "confirmed"}}, &latest))
			tx := tokenTransaction(payer, user, program, un58(latest.Value.Blockhash), f, ixs)
			wantError := "null"
			if step == 2 {
				wantError = `{"InstructionError":[1,{"Custom":112}]}`
				if anchor && names[backend] == "anchor" {
					wantError = `{"InstructionError":[1,{"Custom":6004}]}`
				}
			}
			if e := submitToken(&c, tx, wantError); e != nil {
				return e
			}
			if step < 2 {
				amount := base.amount + uint64(step)
				out := wideReference(expected[2], expected[3], amount)
				expected[0]++
				expected[1] -= amount
				expected[2] += amount
				expected[3] -= out
				expected[4] += out
			}
			for j, index := range []int{0, 2, 3, 4, 5} {
				var got struct{ Value account }
				must(c.call("getAccountInfo", []any{b58(f.keys[index]), map[string]any{"encoding": "base64", "commitment": "confirmed"}}, &got))
				data, e := base64.StdEncoding.DecodeString(got.Value.Data[0])
				must(e)
				offset := 64
				if index == 0 {
					offset = 129
					if anchor {
						offset += 8
					}
				}
				expectedBytes := bytes.Clone(f.data[index])
				binary.LittleEndian.PutUint64(expectedBytes[offset:], expected[j])
				if !bytes.Equal(data, expectedBytes) {
					return fmt.Errorf("%s committed step %d mismatch", names[backend], step)
				}
			}
		}
		for i, v := range vectors {
			if v.mutation != "output-frozen" {
				continue
			}
			f = fixtures[backend][i]
			must(c.call("getLatestBlockhash", []any{map[string]any{"commitment": "confirmed"}}, &latest))
			tx := tokenTransaction(payer, user, program, un58(latest.Value.Blockhash), f, [][]byte{f.ix})
			if e := submitToken(&c, tx, `{"InstructionError":[0,{"Custom":17}]}`); e != nil {
				return e
			}
			for _, index := range []int{0, 2, 3, 4, 5} {
				var got struct{ Value account }
				must(c.call("getAccountInfo", []any{b58(f.keys[index]), map[string]any{"encoding": "base64", "commitment": "confirmed"}}, &got))
				data, e := base64.StdEncoding.DecodeString(got.Value.Data[0])
				must(e)
				if !bytes.Equal(data, f.data[index]) {
					return fmt.Errorf("%s failed CPI did not roll back account %d", names[backend], index)
				}
			}
		}
	}
	report := map[string]any{"runtime_engine": testvm.Engine(), "runtime_version": string(bytes.TrimSpace(version)), "target": "sBPF v3", "deactivated_features": []string{}, "elf_sha256": hashes, "token_program": tokenAddress, "token_program_elf_sha256": tokenHash, "cpi_logs_verified": true, "vectors_per_backend": len(vectors), "committed_swaps_per_backend": 2, "failed_second_cpi_rollback_per_backend": true, "failed_second_instruction_rollback_per_backend": true, "results": rows, "observed_errors": observedErrors,
		"startup_seconds": startupSeconds, "fixture_seconds": time.Since(execution).Seconds(), "total_seconds": time.Since(started).Seconds()}
	if path := os.Getenv("GOSVM_EXPORT_SVM_FIXTURES"); path != "" {
		if err = exportTokenFixtures(path, dir, anchor, programs[0], fixtures[0], vectors, rows, observedErrors, tokenBytes); err != nil {
			return err
		}
	}
	if testvm.IsRunner() {
		var info any
		if err = c.call("gosvmRuntimeInfo", []any{}, &info); err != nil {
			return err
		}
		report["runner"] = info
	} else {
		report["validator"] = string(bytes.TrimSpace(version))
		var features []struct {
			Pubkey  string  `json:"pubkey"`
			Account account `json:"account"`
		}
		if err = c.call("getProgramAccounts", []any{"Feature111111111111111111111111111111111111", map[string]any{"encoding": "base64", "commitment": "confirmed"}}, &features); err != nil {
			return err
		}
		active := []string{}
		for _, f := range features {
			data, e := base64.StdEncoding.DecodeString(f.Account.Data[0])
			if e != nil {
				return e
			}
			// Feature account data is bincode Option<u64>: a one-byte tag
			// followed by the activation slot, not a four-byte enum tag.
			if len(data) >= 9 && data[0] == 1 {
				active = append(active, f.Pubkey)
			}
		}
		sort.Strings(active)
		report["active_features"] = active
	}
	b, _ := json.MarshalIndent(report, "", "  ")
	must(os.WriteFile(filepath.Join(build, "tokenswap-verification.json"), append(b, '\n'), 0644))
	fmt.Printf("PASS: %d token-swap simulations, %d committed swaps, %d rollback checks\n", len(rows), 2*len(programs), 2*len(programs))
	for _, r := range rows {
		if r.Name == "wide-swap" || r.Name == "maximum-input" || r.Name == "small-swap" {
			fmt.Printf("%s %-16s %d CU\n", r.Backend, r.Name, r.CU)
		}
	}
	return nil
}

func submitToken(c *rpcClient, tx string, wantError string) error {
	var sig string
	if e := c.call("sendTransaction", []any{tx, map[string]any{"encoding": "base64", "skipPreflight": true}}, &sig); e != nil {
		return e
	}
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); {
		var s struct {
			Value []*struct {
				Err                json.RawMessage
				ConfirmationStatus string
			}
		}
		if e := c.call("getSignatureStatuses", []any{[]string{sig}}, &s); e != nil {
			return e
		}
		if len(s.Value) > 0 && s.Value[0] != nil && (s.Value[0].ConfirmationStatus == "confirmed" || s.Value[0].ConfirmationStatus == "finalized") {
			if string(s.Value[0].Err) != wantError {
				return fmt.Errorf("unexpected transaction result: %s", s.Value[0].Err)
			}
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("confirmation timeout")
}

func anchorTag(s string) []byte { h := sha256.Sum256([]byte(s)); return append([]byte(nil), h[:8]...) }

// Expected Anchor errors are pinned to the actual typed constraint that rejects
// each mutation, rather than treating an arbitrary failure as a passing test.
func anchorTokenError(v tokenVector) (uint64, string) {
	if v.mutation == "token-length" {
		return 0, "InvalidAccountData"
	}
	if v.mutation == "uninitialized" {
		return 0, "UninitializedAccount"
	}
	codes := map[string]uint64{
		"no-accounts": 101, "missing-account": 101, "extra-account": 101, "account-limit": 101,
		"long-pool": 6000, "pool-owner": 3007, "pool-readonly": 2000, "pool-length": 3003,
		"no-signer": 3010, "fake-program": 3008, "alias": 2014, "vault-substitute": 2001,
		"token-owner": 3007, "token-readonly": 2000, "native-token": 6000, "mint": 2014,
		"same-mint": 6003, "user-authority": 2015, "vault-authority": 2015, "pda": 2006, "bump": 2006,
		"input-frozen": 17, "output-frozen": 17, "short-ix": 102, "long-ix": 102,
		"pool-discriminator": 3002, "instruction-discriminator": 101,
	}
	if n, ok := codes[v.mutation]; ok {
		return n, ""
	}
	switch v.code {
	case 112:
		return 6004, ""
	case 113:
		return 6005, ""
	case 114:
		return 6006, ""
	}
	return 0, "unexpected test vector"
}
