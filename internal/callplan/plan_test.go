package callplan

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"gosvm/internal/network"
	"gosvm/internal/project"
	"gosvm/internal/sbftest"
	"gosvm/internal/targets"
)

var (
	schemaMu sync.Mutex
	schemas  = map[string]*project.ResolvedSchema{}
)

func example(t *testing.T, name string) (*project.ResolvedSchema, sbftest.FixtureSuite) {
	t.Helper()
	dir := filepath.Join("..", "..", "examples", name)
	schemaMu.Lock()
	defer schemaMu.Unlock()
	s := schemas[name]
	if s == nil {
		var err error
		if s, err = project.LoadSchema(dir); err != nil {
			t.Fatal(err)
		}
		schemas[name] = s
	}
	var suite sbftest.FixtureSuite
	b, err := os.ReadFile(filepath.Join(dir, "testdata", "svm.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, &suite); err != nil {
		t.Fatal(err)
	}
	return s, suite
}

func key(t *testing.T, s string) []byte {
	t.Helper()
	k, err := network.DecodeBase58(s)
	if err != nil || len(k) != 32 {
		t.Fatalf("bad key %q", s)
	}
	return k
}

func signer(t *testing.T, suite sbftest.FixtureSuite, name string) ed25519.PrivateKey {
	t.Helper()
	for _, s := range suite.Signers {
		if s.Name == name {
			seed, _ := hex.DecodeString(s.Seed)
			return ed25519.NewKeyFromSeed(seed)
		}
	}
	t.Fatalf("no fixture signer %s", name)
	return nil
}

// fixtureInstruction renders a fixture instruction with addresses resolved,
// in the same shape as a plan, for exact comparison.
func fixtureInstruction(t *testing.T, suite sbftest.FixtureSuite, ix sbftest.FixtureInstruction) string {
	t.Helper()
	addresses := map[string]string{"program": suite.ProgramID}
	for _, a := range suite.Accounts {
		addresses[a.Name] = a.Address
	}
	for _, n := range suite.NativePrograms {
		addresses[n.Name] = n.Address
	}
	var b strings.Builder
	fmt.Fprintf(&b, "program %s\ndata %s\n", addresses[ix.Program], ix.Data)
	for _, m := range ix.Accounts {
		if addresses[m.Account] == "" {
			t.Fatalf("fixture account %s has no address", m.Account)
		}
		fmt.Fprintf(&b, "%s signer=%v writable=%v\n", addresses[m.Account], m.Signer, m.Writable)
	}
	return b.String()
}

func planInstruction(p *Plan) string {
	var b strings.Builder
	ixs := p.Instructions()
	ix := ixs[len(ixs)-1]
	fmt.Fprintf(&b, "program %s\ndata %s\n", network.Base58(ix.Program), hex.EncodeToString(ix.Data))
	for _, m := range ix.Accounts {
		fmt.Fprintf(&b, "%s signer=%v writable=%v\n", network.Base58(m.Key), m.Signer, m.Writable)
	}
	return b.String()
}

func step(t *testing.T, suite sbftest.FixtureSuite, c string, i int) sbftest.FixtureInstruction {
	t.Helper()
	for _, fc := range suite.Cases {
		if fc.Name == c {
			return fc.Steps[i].Instructions[len(fc.Steps[i].Instructions)-1]
		}
	}
	t.Fatalf("no fixture case %s", c)
	return sbftest.FixtureInstruction{}
}

// Instructions built by name for the fixture's owner are byte-identical to
// the tutorial's runner fixtures (which pass LiteSVM: Create 14,790 CU,
// Increment 1,995 CU on solana-v3), so `gosvm call` sends what they test.
func TestTutorialMatchesRunnerFixtures(t *testing.T) {
	s, suite := example(t, "tutorial")
	owner := signer(t, suite, "owner")
	in := func(args map[string]string) Inputs {
		return Inputs{Program: key(t, suite.ProgramID), Payer: owner.Public().(ed25519.PublicKey), PayerSource: "fee payer", Args: args}
	}
	for _, tc := range []struct {
		ix   string
		args map[string]string
		c    string
		step int
	}{
		{"Create", map[string]string{"limit": "10"}, "create-and-increment", 0},
		{"Increment", map[string]string{"amount": "3"}, "create-and-increment", 1},
		{"Increment", map[string]string{"amount": "7"}, "create-and-increment", 2},
		{"Preview", map[string]string{"count": "5", "limit": "10", "amount": "3"}, "preview", 0},
	} {
		p, err := Build(s, tc.ix, in(tc.args))
		if err != nil {
			t.Fatalf("%s: %v", tc.ix, err)
		}
		if got, want := planInstruction(p), fixtureInstruction(t, suite, step(t, suite, tc.c, tc.step)); got != want {
			t.Errorf("%s %v:\n%s\nwant\n%s", tc.ix, tc.args, got, want)
		}
	}
	p, err := Build(s, "Increment", in(map[string]string{"amount": "3"}))
	if err != nil {
		t.Fatal(err)
	}
	if src := p.Accounts[0].Source; src != `PDA ["counter", Owner, bump 255] as declared in Create` {
		t.Errorf("counter source %q", src)
	}
	create, _ := Build(s, "Create", in(map[string]string{"limit": "10"}))
	if create.Args[1].Flag != "bump" || create.Args[1].Source != "canonical bump of Counter" || create.Accounts[2].Source != "System program" {
		t.Errorf("create %+v %+v", create.Args, create.Accounts)
	}
	// An explicit bump is encoded as given (and the PDA follows it).
	bump := 254
	for ; bump > 0; bump-- {
		if _, ok := network.CreateProgramAddress([][]byte{[]byte("counter"), owner.Public().(ed25519.PublicKey), {byte(bump)}}, key(t, suite.ProgramID)); ok {
			break
		}
	}
	explicit, err := Build(s, "Create", in(map[string]string{"limit": "10", "bump": fmt.Sprint(bump)}))
	if err != nil || explicit.Data[16] != byte(bump) || bytes.Equal(explicit.Accounts[0].Key, create.Accounts[0].Key) {
		t.Fatalf("explicit bump %d: %v", bump, err)
	}
	// A bump that gives no PDA is reported.
	for v := 254; v > 0; v-- {
		if _, ok := network.CreateProgramAddress([][]byte{[]byte("counter"), owner.Public().(ed25519.PublicKey), {byte(v)}}, key(t, suite.ProgramID)); !ok {
			if _, err := Build(s, "Create", in(map[string]string{"limit": "10", "bump": fmt.Sprint(v)})); err == nil || !strings.Contains(err.Error(), "are not a valid PDA") {
				t.Errorf("on-curve bump %d: %v", v, err)
			}
			break
		}
	}
}

func TestTutorialErrors(t *testing.T) {
	s, suite := example(t, "tutorial")
	owner := signer(t, suite, "owner")
	other := signer(t, suite, "intruder")
	base := Inputs{Program: key(t, suite.ProgramID), Payer: owner.Public().(ed25519.PublicKey)}
	for _, tc := range []struct {
		ix   string
		args map[string]string
		mod  func(*Inputs)
		want string
	}{
		{"Increment", nil, nil, "Increment needs -amount uint64 (expected: -amount uint64)"},
		{"Increment", map[string]string{"amount": "1", "limit": "2"}, nil, "Increment has no argument -limit; expected: -amount uint64"},
		{"Increment", map[string]string{"amount": "-1"}, nil, `-amount: want a uint64, got "-1"`},
		{"Increment", map[string]string{"amount": "18446744073709551616"}, nil, "want a uint64"},
		{"Preview", map[string]string{"count": "1"}, nil, "Preview needs -limit uint64 -amount uint64"},
		{"Nope", nil, nil, "counter has no instruction \"Nope\"; instructions: Create, Increment, Preview"},
		{"Increment", map[string]string{"amount": "1"}, func(in *Inputs) { in.Accounts = map[string][]byte{"Vault": make([]byte, 32)} }, `Increment has no account "Vault"; accounts: Counter, Owner`},
		{"Increment", map[string]string{"amount": "1"}, func(in *Inputs) { in.Accounts = map[string][]byte{"Owner": other.Public().(ed25519.PublicKey)} }, "-signer Owner=<keypair file>"},
	} {
		in := base
		in.Args = tc.args
		if tc.mod != nil {
			tc.mod(&in)
		}
		_, err := Build(s, tc.ix, in)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s %v: got %v, want %q", tc.ix, tc.args, err, tc.want)
		}
	}
	// Another owner signs with -signer: the counter PDA follows that owner,
	// and the fee payer is no longer an instruction account.
	in := base
	in.Args = map[string]string{"amount": "2"}
	in.Signers = map[string]ed25519.PrivateKey{"Owner": other}
	p, err := Build(s, "Increment", in)
	if err != nil || !bytes.Equal(p.Accounts[1].Key, other.Public().(ed25519.PublicKey)) || len(p.Signers) != 1 {
		t.Fatalf("%v %+v", err, p)
	}
	want, _, _ := network.FindProgramAddress([][]byte{[]byte("counter"), other.Public().(ed25519.PublicKey)}, base.Program)
	if !bytes.Equal(p.Accounts[0].Key, want) {
		t.Errorf("counter for -signer owner")
	}
	// -dry-run accepts an address for a signer.
	in = base
	in.Args, in.Unsigned = map[string]string{"amount": "2"}, true
	in.Accounts = map[string][]byte{"Owner": other.Public().(ed25519.PublicKey)}
	if p, err = Build(s, "Increment", in); err != nil || !bytes.Equal(p.Accounts[0].Key, want) {
		t.Fatalf("dry-run owner: %v", err)
	}
	// Custom errors decode by name.
	i, code, ok := CustomError(json.RawMessage(`{"InstructionError":[0,{"Custom":4101}]}`))
	if !ok || i != 0 || code != 4101 || ErrorName(s, code) != "ErrLimit" || ErrorName(s, 6007) != "AccountRelation" || ErrorName(s, 1) != "" {
		t.Errorf("errors %v %v %v", i, code, ok)
	}
	if _, _, ok := CustomError(json.RawMessage(`{"InstructionError":[0,"InvalidAccountData"]}`)); ok {
		t.Error("non-custom error decoded")
	}
}

func TestTutorialList(t *testing.T) {
	s, _ := example(t, "tutorial")
	var b bytes.Buffer
	List(&b, s)
	for _, want := range []string{
		"Create -limit uint64 [-bump byte]",
		`Counter  writable         state Counter: PDA ["counter", Owner, args.Bump]`,
		"System   readonly         System program",
		"Increment -amount uint64",
		`state Counter: PDA ["counter", Owner, args.Bump] as declared in Create`,
		"Preview -count uint64 -limit uint64 -amount uint64",
		"Caller  signer  the fee payer, unless -signer Caller=<keypair file>",
	} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("listing lacks %q:\n%s", want, b.String())
		}
	}
}

// Escrow: Open derives its PDA from a uint64 argument and the canonical bump,
// and its Beneficiary account from the related argument; Claim's PDA reads
// its own state, so it needs -seed (or -account) for the nonce.
func TestEscrow(t *testing.T) {
	s, suite := example(t, "escrow")
	creator := signer(t, suite, "creator")
	beneficiary := signer(t, suite, "beneficiary")
	fixture := step(t, suite, "open-claim-reuse", 0)
	data, _ := hex.DecodeString(fixture.Data)
	in := Inputs{Program: key(t, suite.ProgramID), Payer: creator.Public().(ed25519.PublicKey), Args: map[string]string{
		"beneficiary": network.Base58(data[8:40]), "commitment": hex.EncodeToString(data[40:72]), "amount": "1000000", "nonce": "123"}}
	p, err := Build(s, "Open", in)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := planInstruction(p), fixtureInstruction(t, suite, fixture); got != want {
		t.Errorf("Open:\n%s\nwant\n%s", got, want)
	}
	if p.Accounts[2].Source != "from -beneficiary" || !strings.HasPrefix(p.Accounts[0].Source, `PDA ["escrow", Creator, args.Nonce=123, bump `) {
		t.Errorf("sources %+v", p.Accounts)
	}
	// Leaving out the argument related to an account takes the account's key.
	in.Args = map[string]string{"commitment": hex.EncodeToString(data[40:72]), "amount": "1000000", "nonce": "123"}
	in.Accounts = map[string][]byte{"Beneficiary": data[8:40]}
	if q, err := Build(s, "Open", in); err != nil || !bytes.Equal(q.Data, p.Data) || q.Args[0].Source != "key of Beneficiary" {
		t.Errorf("beneficiary from account: %v", err)
	}

	claimStep := step(t, suite, "open-claim-reuse", 1)
	claimData, _ := hex.DecodeString(claimStep.Data)
	claim := Inputs{Program: in.Program, Payer: beneficiary.Public().(ed25519.PublicKey), Args: map[string]string{"preimage": hex.EncodeToString(claimData[8:40]), "nonce": "123"}}
	_, err = Build(s, "Claim", claim)
	if err == nil || !strings.Contains(err.Error(), "Creator (readonly): pass -account Creator=<address>") {
		t.Fatalf("claim without creator: %v", err)
	}
	claim.Accounts = map[string][]byte{"Creator": creator.Public().(ed25519.PublicKey)}
	_, err = Build(s, "Claim", claim)
	if err == nil || !strings.Contains(err.Error(), "pass -seed Escrow.Nonce=<value>") {
		t.Fatalf("claim without nonce seed: %v", err)
	}
	claim.Seeds = map[string]string{"Escrow.Nonce": "123"}
	q, err := Build(s, "Claim", claim)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := planInstruction(q), fixtureInstruction(t, suite, claimStep); got != want {
		t.Errorf("Claim:\n%s\nwant\n%s", got, want)
	}
	if q.Accounts[2].Source != "fee payer" && !strings.HasPrefix(q.Accounts[0].Source, `PDA ["escrow", Creator, Escrow.Nonce=123, bump `) {
		t.Errorf("claim sources %+v", q.Accounts)
	}
}

// Full swap: CreatePool's transaction-signer target needs -signer; its vault
// and authority PDAs fill three bump arguments. Swap reads its vaults and the
// authority's bump from the Pool account's current data.
func TestFullSwap(t *testing.T) {
	s, suite := example(t, "full-swap")
	user := signer(t, suite, "authority")
	program := key(t, suite.ProgramID)
	pool := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{5}, 32))
	in := Inputs{Program: program, Payer: user.Public().(ed25519.PublicKey), Args: map[string]string{}}
	_, err := Build(s, "CreatePool", in)
	if err == nil || !strings.Contains(err.Error(), "CreatePool needs") {
		t.Fatalf("missing args: %v", err)
	}
	var args []string
	for _, f := range Fields(s.Arguments[1]) {
		args = append(args, f.Usage())
	}
	t.Logf("CreatePool args: %s", strings.Join(args, " "))
	for _, f := range Fields(s.Arguments[1]) {
		if !strings.Contains(f.Flag, "bump") {
			in.Args[f.Flag] = "1"
		}
	}
	_, err = Build(s, "CreatePool", in)
	for _, want := range []string{"Pool (writable signer state ManagedPool): is a new account that signs its own creation: pass -signer Pool=<new keypair file>", "CreatorX (writable token): pass -account CreatorX=<address>", "MintX (readonly): pass -account MintX=<address>", "VaultX (writable): its PDA seed Pool is unresolved"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("CreatePool error lacks %q: %v", want, err)
		}
	}
	in.Signers = map[string]ed25519.PrivateKey{"Pool": pool}
	in.Accounts = map[string][]byte{}
	for _, n := range []string{"CreatorX", "CreatorY", "MintX", "MintY"} {
		in.Accounts[n] = bytes.Repeat([]byte{byte(len(n))}, 32)
	}
	p, err := Build(s, "CreatePool", in)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]Account{}
	for _, a := range p.Accounts {
		byName[a.Name] = a
	}
	poolKey := pool.Public().(ed25519.PublicKey)
	vaultX, xb, _ := network.FindProgramAddress([][]byte{[]byte("vaultx"), poolKey}, program)
	authority, ab, _ := network.FindProgramAddress([][]byte{poolKey}, program)
	if !bytes.Equal(byName["VaultX"].Key, vaultX) || !bytes.Equal(byName["Authority"].Key, authority) || byName["System"].Source != "System program" || byName["TokenProgram"].Source != "SPL Token program" || !bytes.Equal(byName["Creator"].Key, in.Payer) {
		t.Errorf("CreatePool accounts %+v", p.Accounts)
	}
	bumps := map[string]byte{}
	for _, a := range p.Args {
		if strings.Contains(a.Flag, "bump") {
			bumps[a.Flag] = a.Value[0]
		}
	}
	if bumps["x-bump"] != xb || bumps["bump"] != ab {
		t.Errorf("bumps %v want x %d authority %d", bumps, xb, ab)
	}

	// Swap: the fixture's initial Pool account supplies the vaults and bump.
	fixture := step(t, suite, "wide-swap", 0)
	addresses := map[string]string{}
	state := map[string][]byte{}
	for _, a := range suite.Accounts {
		addresses[a.Name] = a.Address
		if a.Initial != nil {
			state[a.Address], _ = hex.DecodeString(a.Initial.Data)
		}
	}
	data, _ := hex.DecodeString(fixture.Data)
	swapFields := Fields(s.Arguments[0])
	swap := Inputs{Program: program, Payer: user.Public().(ed25519.PublicKey), Args: map[string]string{}, Accounts: map[string][]byte{
		"Pool":  key(t, addresses[fixture.Accounts[0].Account]),
		"UserX": key(t, addresses[fixture.Accounts[2].Account]),
		"UserY": key(t, addresses[fixture.Accounts[5].Account]),
	}}
	for _, f := range swapFields {
		swap.Args[f.Flag] = f.Format(data[f.Offset : f.Offset+f.Size])
	}
	_, err = Build(s, "Swap", swap)
	if err == nil || !strings.Contains(err.Error(), "Pool.VaultX is read from the cluster, which -dry-run skips") {
		t.Fatalf("swap without fetch: %v", err)
	}
	swap.Fetch = func(address []byte) ([]byte, error) {
		if b, ok := state[network.Base58(address)]; ok {
			return b, nil
		}
		return nil, fmt.Errorf("account does not exist")
	}
	q, err := Build(s, "Swap", swap)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := planInstruction(q), fixtureInstruction(t, suite, fixture); got != want {
		t.Errorf("Swap:\n%s\nwant\n%s", got, want)
	}
	if q.Accounts[3].Source != "read from Pool.VaultX" || !strings.HasPrefix(q.Accounts[6].Source, "PDA [Pool, Pool.Bump=") {
		t.Errorf("swap sources %+v", q.Accounts)
	}
}

func TestFieldParsing(t *testing.T) {
	signed := Field{Flag: "delta", Size: 2, ElemSize: 2, Signed: true}
	if b, err := signed.Parse("-2"); err != nil || hex.EncodeToString(b) != "feff" || signed.Format(b) != "-2" {
		t.Errorf("int16 -2: %x %v", b, err)
	}
	if _, err := signed.Parse("40000"); err == nil {
		t.Error("int16 overflow accepted")
	}
	wide := Field{Flag: "x", Size: 8, ElemSize: 8}
	if b, err := wide.Parse("0xff"); err != nil || b[0] != 0xff || wide.Format(b) != "255" {
		t.Errorf("hex uint64: %x %v", b, err)
	}
	k := Field{Flag: "key", Size: 32, Length: 32, ElemSize: 1}
	b58 := "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA"
	if b, err := k.Parse(b58); err != nil || k.Format(b) != b58 {
		t.Errorf("base58 key: %v", err)
	}
	if b, err := k.Parse("0x" + strings.Repeat("01", 32)); err != nil || b[31] != 1 {
		t.Errorf("hex key: %v", err)
	}
	if _, err := k.Parse("abc"); err == nil || !strings.Contains(err.Error(), "base58 key or 64 hex digits") {
		t.Errorf("bad key: %v", err)
	}
	arr := Field{Flag: "weights", Size: 6, Length: 3, ElemSize: 2}
	if b, err := arr.Parse("1, 2,65535"); err != nil || hex.EncodeToString(b) != "01000200ffff" || arr.Format(b) != "1,2,65535" {
		t.Errorf("array: %x %v", b, err)
	}
	if _, err := arr.Parse("1,2"); err == nil {
		t.Error("short array accepted")
	}
	for in, want := range map[string]string{"Amount": "amount", "MinOut": "min-out", "XBump": "x-bump", "PoolID": "pool-id", "A": "a"} {
		if got := kebab(in); got != want {
			t.Errorf("kebab(%s) = %s", in, got)
		}
	}
	f := Field{Flag: "min-out", Path: "MinOut"}
	if !f.Matches("min-out") || !f.Matches("MinOut") || !f.Matches("minout") || f.Matches("min") {
		t.Error("matching")
	}
}

// A heap budget above 32 KiB prepends RequestHeapFrame.
func TestHeapFrame(t *testing.T) {
	p := &Plan{Program: make([]byte, 32), HeapBytes: 64 * 1024}
	ixs := p.Instructions()
	if len(ixs) != 2 || network.Base58(ixs[0].Program) != "ComputeBudget111111111111111111111111111111" || hex.EncodeToString(ixs[0].Data) != "0100000100" {
		t.Fatalf("%+v", ixs)
	}
	p.HeapBytes = 0
	if len(p.Instructions()) != 1 {
		t.Fatal("unexpected heap request")
	}
}

// TestTutorialRunnerE2E runs the tutorial's create/increment/preview cases
// with instructions built by Build in place of the fixture's, against a
// compiled tutorial ELF and the managed LiteSVM runner, and requires the
// same outcome and CU as the original fixtures. Opt in with
// GOSVM_CALL_E2E_ELF=<examples/tutorial build/solana/program.so> (and
// GOSVM_CALL_E2E_TARGET=fogo for a fogo ELF).
func TestTutorialRunnerE2E(t *testing.T) {
	elf := os.Getenv("GOSVM_CALL_E2E_ELF")
	if elf == "" {
		t.Skip("set GOSVM_CALL_E2E_ELF to a compiled tutorial program to run against the SVM runner")
	}
	name := os.Getenv("GOSVM_CALL_E2E_TARGET")
	if name == "" {
		name = "solana"
	}
	profile, err := target.Resolve(name)
	if err != nil {
		t.Fatal(err)
	}
	s, suite := example(t, "tutorial")
	owner := signer(t, suite, "owner")
	names := map[string]string{suite.ProgramID: "program"}
	for _, a := range suite.Accounts {
		names[a.Address] = a.Name
	}
	for _, n := range suite.NativePrograms {
		names[n.Address] = n.Name
	}
	build := func(ix string, args map[string]string) sbftest.FixtureInstruction {
		p, err := Build(s, ix, Inputs{Program: key(t, suite.ProgramID), Payer: owner.Public().(ed25519.PublicKey), Args: args})
		if err != nil {
			t.Fatal(err)
		}
		out := sbftest.FixtureInstruction{Program: names[network.Base58(p.Program)], Data: hex.EncodeToString(p.Data)}
		for _, a := range p.Accounts {
			out.Accounts = append(out.Accounts, sbftest.FixtureMeta{Account: names[network.Base58(a.Key)], Signer: a.Signer, Writable: a.Writable})
		}
		return out
	}
	plans := map[string][]sbftest.FixtureInstruction{
		"create-and-increment": {build("Create", map[string]string{"limit": "10"}), build("Increment", map[string]string{"amount": "3"}), build("Increment", map[string]string{"amount": "7"})},
		"preview":              {build("Preview", map[string]string{"count": "5", "limit": "10", "amount": "3"})},
	}
	original, called := suite, suite
	original.Cases, called.Cases = nil, nil
	for _, c := range suite.Cases {
		if plans[c.Name] == nil {
			continue
		}
		original.Cases = append(original.Cases, c)
		c.Steps = append([]sbftest.FixtureStep(nil), c.Steps...)
		for i := range c.Steps {
			c.Steps[i].Instructions = []sbftest.FixtureInstruction{plans[c.Name][i]}
		}
		called.Cases = append(called.Cases, c)
	}
	dir := t.TempDir()
	run := func(label string, fixtures sbftest.FixtureSuite) []uint64 {
		path := filepath.Join(dir, label+".json")
		b, _ := json.Marshal(fixtures)
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatal(err)
		}
		report := filepath.Join(dir, label+"-report.json")
		var out bytes.Buffer
		if err := sbftest.RunGeneral(context.Background(), elf, path, report, &out, sbftest.FastOptions{Profile: profile.Name, PlatformTools: profile.Tools}); err != nil {
			t.Fatalf("%s: %v\n%s", label, err, out.String())
		}
		t.Logf("%s:\n%s", label, out.String())
		var r struct {
			Cases []struct {
				Steps []struct {
					CU uint64 `json:"cu"`
				} `json:"steps"`
			} `json:"cases"`
		}
		b, _ = os.ReadFile(report)
		if err := json.Unmarshal(b, &r); err != nil {
			t.Fatal(err)
		}
		var cu []uint64
		for _, c := range r.Cases {
			for _, s := range c.Steps {
				cu = append(cu, s.CU)
			}
		}
		return cu
	}
	want, got := run("fixtures", original), run("call", called)
	if fmt.Sprint(got) != fmt.Sprint(want) || len(got) != 4 {
		t.Fatalf("call-built CU %v, fixture CU %v", got, want)
	}
}
