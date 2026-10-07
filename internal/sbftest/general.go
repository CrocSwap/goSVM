package sbftest

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"

	"gosvm/internal/testvm"
)

// FixtureFormat is unrelated to the generated account-binding schema.
const FixtureFormat = "gosvm-svm-fixtures-v1"

type FixtureSuite struct {
	Format         string                 `json:"format"`
	ProgramID      string                 `json:"program_id"`
	PayerSeed      string                 `json:"payer_seed"`
	Signers        []FixtureSigner        `json:"signers,omitempty"`
	Programs       []FixtureProgram       `json:"programs,omitempty"`
	NativePrograms []FixtureNativeProgram `json:"native_programs,omitempty"`
	Accounts       []FixtureAccount       `json:"accounts"`
	Sysvars        json.RawMessage        `json:"sysvars,omitempty"`
	Cases          []FixtureCase          `json:"cases"`
}
type FixtureSigner struct {
	Name string `json:"name"`
	Seed string `json:"seed"`
}
type FixtureProgram struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	ELF     string `json:"elf"`
	SHA256  string `json:"sha256"`
}

// Native aliases reference an allowlisted runtime builtin, without an ELF.
// Keep this explicit: bundled SBF programs still require pinned images.
type FixtureNativeProgram struct {
	Name    string `json:"name"`
	Address string `json:"address"`
}
type FixtureState struct {
	Data       string `json:"data"`
	Owner      string `json:"owner"`
	Lamports   uint64 `json:"lamports"`
	Executable bool   `json:"executable"`
	RentEpoch  uint64 `json:"rent_epoch"`
}
type FixtureAccount struct {
	Name    string        `json:"name"`
	Address string        `json:"address"`
	Initial *FixtureState `json:"initial,omitempty"`
}
type FixtureOverride struct {
	Name  string       `json:"name"`
	State FixtureState `json:"state"`
}
type FixtureCase struct {
	Name      string            `json:"name"`
	Overrides []FixtureOverride `json:"overrides,omitempty"`
	Steps     []FixtureStep     `json:"steps"`
}
type FixtureStep struct {
	Sysvars        json.RawMessage      `json:"sysvars,omitempty"`
	FreshBlockhash bool                 `json:"fresh_blockhash,omitempty"`
	Instructions   []FixtureInstruction `json:"instructions"`
	Expect         FixtureExpectation   `json:"expect"`
}
type FixtureInstruction struct {
	Program  string        `json:"program"`
	Accounts []FixtureMeta `json:"accounts"`
	Data     string        `json:"data"`
}
type FixtureMeta struct {
	Account  string `json:"account"`
	Signer   bool   `json:"signer"`
	Writable bool   `json:"writable"`
}
type FixtureExpectation struct {
	Error              json.RawMessage `json:"error"`
	CU                 *uint64         `json:"cu,omitempty"`
	Accounts           []FixtureCheck  `json:"accounts"`
	SimulationAccounts []FixtureCheck  `json:"simulation_accounts,omitempty"`
	LogCounts          map[string]int  `json:"log_counts,omitempty"`
}
type FixtureCheck struct {
	Account string        `json:"account"`
	Absent  bool          `json:"absent,omitempty"`
	State   *FixtureState `json:"state,omitempty"`
}

type fixtureKeys struct {
	addresses map[string][]byte
	signers   map[string]ed25519.PrivateKey
	payer     ed25519.PrivateKey
}

func addressBytes(value string) ([]byte, error) {
	if value == "" {
		return nil, fmt.Errorf("empty address")
	}
	for _, c := range []byte(value) {
		if bytes.IndexByte([]byte(alphabet), c) < 0 {
			return nil, fmt.Errorf("invalid base58 address %q", value)
		}
	}
	b := un58(value)
	if len(b) != 32 || b58(b) != value {
		return nil, fmt.Errorf("require canonical 32-byte address: %q", value)
	}
	return b, nil
}
func fixtureSeed(seed string) (ed25519.PrivateKey, error) {
	b, e := hex.DecodeString(seed)
	if e != nil || len(b) != ed25519.SeedSize {
		return nil, fmt.Errorf("require 32-byte hex test seed")
	}
	return ed25519.NewKeyFromSeed(b), nil
}
func fixtureState(value FixtureState) error {
	data, e := hex.DecodeString(value.Data)
	if e != nil || len(data) > 1024*1024 {
		return fmt.Errorf("require hex account data of at most 1 MiB")
	}
	_, e = addressBytes(value.Owner)
	return e
}
func (v FixtureState) account() account {
	data, _ := hex.DecodeString(v.Data)
	return account{Data: []string{base64.StdEncoding.EncodeToString(data), "base64"}, Owner: v.Owner, Lamports: v.Lamports, Executable: v.Executable, RentEpoch: v.RentEpoch}
}
func canonicalJSON(data []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if e := decoder.Decode(&value); e != nil {
		return nil, e
	}
	if e := decoder.Decode(new(any)); e != io.EOF {
		return nil, fmt.Errorf("require one JSON value")
	}
	return json.Marshal(value)
}
func LoadGeneral(path string) (FixtureSuite, error) {
	var suite FixtureSuite
	f, e := os.Open(path)
	if e != nil {
		return suite, e
	}
	defer f.Close()
	stat, e := f.Stat()
	if e != nil {
		return suite, e
	}
	if stat.Size() > 16*1024*1024 {
		return suite, fmt.Errorf("fixture file exceeds 16 MiB")
	}
	d := json.NewDecoder(io.LimitReader(f, 16*1024*1024+1))
	d.DisallowUnknownFields()
	if e = d.Decode(&suite); e != nil {
		return suite, e
	}
	if e = d.Decode(new(any)); e != io.EOF {
		return suite, fmt.Errorf("require one fixture JSON object")
	}
	_, e = suite.validate()
	return suite, e
}
func (s FixtureSuite) validate() (fixtureKeys, error) {
	k := fixtureKeys{addresses: map[string][]byte{}, signers: map[string]ed25519.PrivateKey{}}
	if s.Format != FixtureFormat || len(s.Cases) == 0 || len(s.Cases) > 1024 || len(s.Accounts) > 4096 {
		return k, fmt.Errorf("require %s, 1..1024 cases, and at most 4096 accounts", FixtureFormat)
	}
	if len(s.Sysvars) > 0 {
		if _, err := testvm.ParseSysvars(s.Sysvars); err != nil {
			return k, fmt.Errorf("initial sysvars: %w", err)
		}
	}
	program, e := addressBytes(s.ProgramID)
	if e != nil {
		return k, e
	}
	k.payer, e = fixtureSeed(s.PayerSeed)
	if e != nil {
		return k, e
	}
	k.addresses["program"] = program
	k.addresses["payer"] = k.payer.Public().(ed25519.PublicKey)
	k.signers[b58(k.addresses["payer"])] = k.payer
	names := map[string]bool{"program": true, "payer": true}
	addresses := map[string]string{b58(program): "program", b58(k.addresses["payer"]): "payer"}
	if bytes.Equal(program, k.addresses["payer"]) {
		return k, fmt.Errorf("payer and program addresses must differ")
	}
	validName := regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)
	add := func(name, address string, signerAccount bool) error {
		if !validName.MatchString(name) {
			return fmt.Errorf("invalid fixture alias %q", name)
		}
		key, e := addressBytes(address)
		if e != nil {
			return e
		}
		if names[name] {
			if signerAccount && k.signers[address] != nil && bytes.Equal(k.addresses[name], key) {
				return nil
			}
			return fmt.Errorf("duplicate/reserved alias %q", name)
		}
		if previous, ok := addresses[address]; ok {
			return fmt.Errorf("address already named %q; reuse that alias", previous)
		}
		names[name] = true
		addresses[address] = name
		k.addresses[name] = key
		return nil
	}
	for _, signer := range s.Signers {
		private, e := fixtureSeed(signer.Seed)
		if e != nil {
			return k, fmt.Errorf("%s: %w", signer.Name, e)
		}
		address := b58(private.Public().(ed25519.PublicKey))
		if e = add(signer.Name, address, false); e != nil {
			return k, e
		}
		k.signers[address] = private
	}
	programs := map[string]bool{"program": true}
	for _, p := range s.NativePrograms {
		if p.Address != "11111111111111111111111111111111" {
			return k, fmt.Errorf("%s: only the native System program is supported", p.Name)
		}
		if e = add(p.Name, p.Address, false); e != nil {
			return k, e
		}
		programs[p.Name] = true
	}
	for _, p := range s.Programs {
		if e = add(p.Name, p.Address, false); e != nil {
			return k, e
		}
		programs[p.Name] = true
		h, e := hex.DecodeString(p.SHA256)
		if e != nil || len(h) != 32 || p.ELF == "" {
			return k, fmt.Errorf("%s: require ELF path and SHA-256", p.Name)
		}
	}
	accounts := map[string]bool{}
	for _, a := range s.Accounts {
		if accounts[a.Name] {
			return k, fmt.Errorf("duplicate account %q", a.Name)
		}
		if e = add(a.Name, a.Address, true); e != nil {
			return k, e
		}
		accounts[a.Name] = true
		if a.Initial != nil {
			if a.Name == "payer" {
				return k, fmt.Errorf("payer uses the runner's initial test balance; use a scenario override to change it")
			}
			if e = fixtureState(*a.Initial); e != nil {
				return k, fmt.Errorf("%s: %w", a.Name, e)
			}
			if a.Initial.Executable {
				return k, fmt.Errorf("%s: executable images belong in programs", a.Name)
			}
		}
	}
	seen := map[string]bool{}
	for _, c := range s.Cases {
		if c.Name == "" || seen[c.Name] || len(c.Steps) == 0 || len(c.Steps) > 64 {
			return k, fmt.Errorf("require unique nonempty case names and 1..64 steps")
		}
		seen[c.Name] = true
		overridden := map[string]bool{}
		for _, o := range c.Overrides {
			if !accounts[o.Name] || overridden[o.Name] || o.State.Executable {
				return k, fmt.Errorf("%s: invalid/duplicate override %q", c.Name, o.Name)
			}
			overridden[o.Name] = true
			if e = fixtureState(o.State); e != nil {
				return k, e
			}
		}
		for i, step := range c.Steps {
			if len(step.Sysvars) > 0 {
				controls, err := testvm.ParseSysvars(step.Sysvars)
				if err != nil {
					return k, fmt.Errorf("%s step %d sysvars: %w", c.Name, i+1, err)
				}
				if controls.Clock == nil && controls.Rent == nil {
					return k, fmt.Errorf("%s step %d sysvars: require clock and/or rent", c.Name, i+1)
				}
			}
			if len(step.Instructions) == 0 || len(step.Instructions) > 8 || len(step.Expect.Accounts) == 0 || len(step.Expect.Accounts) > 64 {
				return k, fmt.Errorf("%s step %d: require 1..8 instructions and 1..64 state assertions", c.Name, i)
			}
			if _, e = canonicalJSON(step.Expect.Error); e != nil {
				return k, fmt.Errorf("%s step %d: explicit expected error required: %w", c.Name, i, e)
			}
			checked := map[string]bool{}
			for _, check := range step.Expect.Accounts {
				if k.addresses[check.Account] == nil || checked[check.Account] || check.Absent == (check.State != nil) {
					return k, fmt.Errorf("%s: invalid/duplicate account assertion %q", c.Name, check.Account)
				}
				checked[check.Account] = true
				if check.State != nil {
					if e = fixtureState(*check.State); e != nil {
						return k, e
					}
				}
			}
			simChecked := map[string]bool{}
			for _, check := range step.Expect.SimulationAccounts {
				if !checked[check.Account] || simChecked[check.Account] || check.Absent == (check.State != nil) || !bytes.Equal(bytes.TrimSpace(step.Expect.Error), []byte("null")) {
					return k, fmt.Errorf("%s: simulation state overrides require a success and unique submitted account assertions", c.Name)
				}
				simChecked[check.Account] = true
				if check.State != nil {
					if e = fixtureState(*check.State); e != nil {
						return k, e
					}
				}
			}
			for text, count := range step.Expect.LogCounts {
				if text == "" || count < 0 {
					return k, fmt.Errorf("%s: invalid log count", c.Name)
				}
			}
			for _, ix := range step.Instructions {
				if !programs[ix.Program] || len(ix.Accounts) > 32 {
					return k, fmt.Errorf("%s: unknown program or over 32 instruction account references", c.Name)
				}
				data, e := hex.DecodeString(ix.Data)
				if e != nil || len(data) > 1024 {
					return k, fmt.Errorf("%s: invalid instruction data", c.Name)
				}
				for _, meta := range ix.Accounts {
					key := k.addresses[meta.Account]
					if key == nil {
						return k, fmt.Errorf("%s: unknown account %q", c.Name, meta.Account)
					}
					if meta.Signer && k.signers[b58(key)] == nil {
						return k, fmt.Errorf("%s: missing test signer %q", c.Name, meta.Account)
					}
					if meta.Writable && meta.Account != "payer" && !checked[meta.Account] {
						return k, fmt.Errorf("%s: writable account %q needs an explicit state assertion", c.Name, meta.Account)
					}
				}
			}
		}
	}
	return k, nil
}

// SimulationCheck selects an explicit phase override, otherwise the shared
// full-state assertion. It never changes the committed-state expectation.
func (e FixtureExpectation) SimulationCheck(check FixtureCheck) FixtureCheck {
	for _, override := range e.SimulationAccounts {
		if override.Account == check.Account {
			return override
		}
	}
	return check
}

// Transaction constructs and signs a legacy transaction using local fixture
// keys. It also validates the complete suite before accepting instructions.
func (s FixtureSuite) Transaction(blockhash string, instructions []FixtureInstruction) (string, string, error) {
	k, e := s.validate()
	if e != nil {
		return "", "", e
	}
	hash, e := addressBytes(blockhash)
	if e != nil {
		return "", "", e
	}
	// Public callers must supply a declared step, so the same alias, signer,
	// writable assertion, and size validation applies outside RunGeneral.
	encoded, e := json.Marshal(instructions)
	if e != nil {
		return "", "", e
	}
	for _, c := range s.Cases {
		for _, step := range c.Steps {
			declared, _ := json.Marshal(step.Instructions)
			if bytes.Equal(encoded, declared) {
				return fixtureTransaction(k, hash, instructions)
			}
		}
	}
	return "", "", fmt.Errorf("instructions must be a declared fixture step")
}

// Merge duplicate public keys and privileges, while preserving instruction
// reference order. Signers precede unsigned keys, writable keys precede readonly.
func fixtureTransaction(k fixtureKeys, hash []byte, instructions []FixtureInstruction) (string, string, error) {
	type entry struct {
		key              []byte
		signer, writable bool
	}
	entries := []entry{}
	add := func(key []byte, signer, writable bool) {
		for i := range entries {
			if bytes.Equal(entries[i].key, key) {
				entries[i].signer = entries[i].signer || signer
				entries[i].writable = entries[i].writable || writable
				return
			}
		}
		entries = append(entries, entry{key, signer, writable})
	}
	add(k.addresses["payer"], true, true)
	for _, ix := range instructions {
		for _, meta := range ix.Accounts {
			add(k.addresses[meta.Account], meta.Signer, meta.Writable)
		}
		add(k.addresses[ix.Program], false, false)
	}
	if len(hash) != 32 || len(entries) > 256 {
		return "", "", fmt.Errorf("invalid blockhash or over 256 message keys")
	}
	sort.SliceStable(entries[1:], func(i, j int) bool {
		a, b := entries[i+1], entries[j+1]
		if a.signer != b.signer {
			return a.signer
		}
		return a.writable && !b.writable
	})
	index := func(key []byte) byte {
		for i, m := range entries {
			if bytes.Equal(m.key, key) {
				return byte(i)
			}
		}
		panic("validated key missing")
	}
	nsig, roSig, roUnsigned := 0, 0, 0
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
	if nsig > 64 {
		return "", "", fmt.Errorf("over 64 signers")
	}
	msg := []byte{byte(nsig), byte(roSig), byte(roUnsigned)}
	msg = append(msg, compact(len(entries))...)
	for _, m := range entries {
		msg = append(msg, m.key...)
	}
	msg = append(msg, hash...)
	msg = append(msg, compact(len(instructions))...)
	for _, ix := range instructions {
		msg = append(msg, index(k.addresses[ix.Program]))
		msg = append(msg, compact(len(ix.Accounts))...)
		for _, meta := range ix.Accounts {
			msg = append(msg, index(k.addresses[meta.Account]))
		}
		data, _ := hex.DecodeString(ix.Data)
		msg = append(msg, compact(len(data))...)
		msg = append(msg, data...)
	}
	wire := compact(nsig)
	signature := ""
	for _, m := range entries {
		if m.signer {
			private := k.signers[b58(m.key)]
			if private == nil {
				return "", "", fmt.Errorf("missing test signing key")
			}
			sig := ed25519.Sign(private, msg)
			if signature == "" {
				signature = b58(sig)
			}
			wire = append(wire, sig...)
		}
	}
	wire = append(wire, msg...)
	if len(wire) > 1232 {
		return "", "", fmt.Errorf("transaction exceeds 1232-byte packet limit")
	}
	return base64.StdEncoding.EncodeToString(wire), signature, nil
}
