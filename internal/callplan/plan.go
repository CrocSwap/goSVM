package callplan

import (
	"bytes"
	"crypto/ed25519"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"gosvm/internal/network"
	"gosvm/internal/project"
)

// WellKnown names fixed program and sysvar addresses in resolved output.
var WellKnown = map[string]string{
	network.SystemProgram:                          "System program",
	"TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA":  "SPL Token program",
	"TokenzQdBNbLqP5VEhdkAS6EPFLC1PHnBqCXEpPxuEb":  "Token-2022 program",
	"ATokenGPvbdGVxr1b2hvZbsiqW5xWH25efTNsLJA8knL": "Associated Token program",
	"ComputeBudget111111111111111111111111111111":  "ComputeBudget program",
	network.SysvarRent:                             "Rent sysvar",
	network.SysvarClock:                            "Clock sysvar",
	"Sysvar1nstructions1111111111111111111111111":  "Instructions sysvar",
}

// Inputs are everything Build needs beyond the schema.
type Inputs struct {
	Program     []byte            // deployed program ID
	Payer       []byte            // fee payer; the default signer
	PayerSource string            // how the payer was chosen, for output
	Args        map[string]string // argument flag (without dash) -> value
	Accounts    map[string][]byte // -account Name=<address>
	Signers     map[string]ed25519.PrivateKey
	Seeds       map[string]string // -seed Field=value for seeds the schema cannot supply
	// Fetch reads an account's data, or is nil (dry runs); it lets state
	// fields supply PDA seeds and key relations.
	Fetch func(address []byte) ([]byte, error)
	// Unsigned permits signer accounts whose keypair is unavailable (dry runs).
	Unsigned bool
}

// Account is one resolved instruction account.
type Account struct {
	Name             string
	Kind             string
	Key              []byte
	Signer, Writable bool
	Source           string // how the key was resolved
}

// Arg is one encoded argument with its value and how it was chosen.
type Arg struct {
	Field
	Value  []byte
	Source string // "" for a supplied value
}

// Plan is a resolved instruction ready to sign.
type Plan struct {
	Instruction   string
	Discriminator []byte
	Program       []byte
	Args          []Arg
	Accounts      []Account
	Data          []byte
	Signers       []ed25519.PrivateKey // -signer keypairs, beyond the payer
	HeapBytes     int                  // > 32768 prepends ComputeBudget RequestHeapFrame
	Warnings      []string
}

// Instructions are the transaction's instructions: a RequestHeapFrame when
// the project's heap budget needs one, then the call itself.
func (p *Plan) Instructions() []network.Instruction {
	var out []network.Instruction
	if p.HeapBytes > 32*1024 {
		data := []byte{1, 0, 0, 0, 0}
		binary.LittleEndian.PutUint32(data[1:], uint32(p.HeapBytes))
		budget, _ := network.DecodeBase58("ComputeBudget111111111111111111111111111111")
		out = append(out, network.Instruction{Program: budget, Data: data})
	}
	metas := make([]network.Meta, len(p.Accounts))
	for i, a := range p.Accounts {
		metas[i] = network.Meta{Key: a.Key, Signer: a.Signer, Writable: a.Writable}
	}
	return append(out, network.Instruction{Program: p.Program, Accounts: metas, Data: p.Data})
}

// Find returns the named instruction and its argument layout.
func Find(s *project.ResolvedSchema, name string) (int, error) {
	for i, ix := range s.Instructions {
		if ix.Name == name {
			return i, nil
		}
	}
	for i, ix := range s.Instructions {
		if strings.EqualFold(ix.Name, name) {
			return i, nil
		}
	}
	var names []string
	for _, ix := range s.Instructions {
		names = append(names, ix.Name)
	}
	return 0, fmt.Errorf("%s has no instruction %q; instructions: %s (gosvm call -list shows their arguments)", s.Config.Name, name, strings.Join(names, ", "))
}

// ArgField returns the instruction's argument field selected by flag name.
func ArgField(s *project.ResolvedSchema, ix int, name string) (Field, bool) {
	for _, f := range Fields(s.Arguments[ix]) {
		if f.Matches(name) {
			return f, true
		}
	}
	return Field{}, false
}

func expected(fields []Field) string {
	if len(fields) == 0 {
		return "no arguments"
	}
	var u []string
	for _, f := range fields {
		u = append(u, f.Usage())
	}
	return strings.Join(u, " ")
}

type resolver struct {
	s       *project.ResolvedSchema
	ix      project.InstructionDeclaration
	in      Inputs
	fields  []Field
	values  map[string][]byte // arg path -> encoded value
	sources map[string]string // arg path -> how a derived value was chosen
	done    map[string]*Account
	active  map[string]bool
	payerAt string
	fetched map[string][]byte
	signers []ed25519.PrivateKey
}

// errCycle marks a resolution path that depends on itself; other paths are tried.
var errCycle = errors.New("cycle")

// Build resolves instruction name with in. Errors name the missing argument
// or account and the flag that supplies it.
func Build(s *project.ResolvedSchema, name string, in Inputs) (*Plan, error) {
	i, err := Find(s, name)
	if err != nil {
		return nil, err
	}
	ix := s.Instructions[i]
	layout := s.Arguments[i]
	r := &resolver{s: s, ix: ix, in: in, fields: Fields(layout), values: map[string][]byte{}, sources: map[string]string{}, done: map[string]*Account{}, active: map[string]bool{}, fetched: map[string][]byte{}}
	keys := make([]string, 0, len(in.Args))
	for k := range in.Args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		f, ok := ArgField(s, i, k)
		if !ok {
			return nil, fmt.Errorf("%s has no argument -%s; expected: %s", ix.Name, k, expected(r.fields))
		}
		if r.values[f.Path] != nil {
			return nil, fmt.Errorf("argument -%s given twice", f.Flag)
		}
		b, err := f.Parse(in.Args[k])
		if err != nil {
			return nil, err
		}
		r.values[f.Path] = b
	}
	declared := map[string]bool{}
	for _, a := range ix.Accounts {
		declared[a.Name] = true
	}
	for _, m := range []map[string]bool{keysOf(in.Accounts), keysOf(in.Signers)} {
		for name := range m {
			if !declared[name] {
				return nil, fmt.Errorf("%s has no account %q; accounts: %s", ix.Name, name, accountNames(ix))
			}
		}
	}
	for name := range in.Accounts {
		if in.Signers[name] != nil {
			return nil, fmt.Errorf("account %s given with both -account and -signer", name)
		}
	}
	// Arguments the schema can derive (a trailing PDA bump, or a key related
	// to an account) may be omitted; any other missing argument is an error.
	var missing []string
	for _, f := range r.fields {
		if r.values[f.Path] == nil && !r.derivable(f) {
			missing = append(missing, f.Usage())
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("%s needs %s (expected: %s)", ix.Name, strings.Join(missing, " "), expected(r.fields))
	}
	r.payerAt = r.payerAccount()
	var problems []string
	plan := &Plan{Instruction: ix.Name, Program: in.Program, HeapBytes: s.Config.HeapBytes}
	for _, decl := range ix.Accounts {
		a, err := r.resolve(decl.Name)
		if err != nil {
			problems = append(problems, fmt.Sprintf("  %s (%s): %v", decl.Name, describeFlags(decl), err))
			continue
		}
		plan.Accounts = append(plan.Accounts, *a)
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("cannot resolve %s's accounts:\n%s", ix.Name, strings.Join(problems, "\n"))
	}
	// Arguments related to accounts take the account's key.
	for _, rel := range ix.Relations {
		if path, ok := strings.CutPrefix(rel.Field, "args."); ok && rel.Account != "" && r.values[path] == nil {
			a := r.done[rel.Account]
			r.values[path], r.sources[path] = a.Key, "key of "+rel.Account
		}
	}
	plan.Discriminator = make([]byte, 8)
	for j, d := range layout.Discriminator {
		plan.Discriminator[j] = byte(d)
	}
	plan.Data = make([]byte, layout.Size)
	copy(plan.Data, plan.Discriminator)
	for _, f := range r.fields {
		v := r.values[f.Path]
		if v == nil {
			return nil, fmt.Errorf("%s needs -%s %s (expected: %s)", ix.Name, f.Flag, f.Type, expected(r.fields))
		}
		copy(plan.Data[f.Offset:f.Offset+f.Size], v)
		plan.Args = append(plan.Args, Arg{Field: f, Value: v, Source: r.sources[f.Path]})
	}
	plan.Signers = r.signers
	if in.Fetch != nil {
		for _, a := range plan.Accounts {
			d, _ := r.decl(a.Name)
			if d.Kind != "state" {
				continue
			}
			_, err := r.read(a.Key)
			switch {
			case d.Init == nil && err != nil:
				plan.Warnings = append(plan.Warnings, fmt.Sprintf("%s %s: %v", a.Name, network.Base58(a.Key), err))
			case d.Init != nil && err == nil:
				plan.Warnings = append(plan.Warnings, fmt.Sprintf("%s %s already exists, so creating it fails", a.Name, network.Base58(a.Key)))
			}
		}
	}
	seen := map[string]string{}
	for _, a := range plan.Accounts {
		k := string(a.Key)
		if other, dup := seen[k]; dup && !aliased(ix, other, a.Name) {
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("%s and %s are the same account %s; the program rejects that unless the schema declares an alias (error 6006)", other, a.Name, network.Base58(a.Key)))
		}
		seen[k] = a.Name
	}
	return plan, nil
}

func keysOf[V any](m map[string]V) map[string]bool {
	out := map[string]bool{}
	for k := range m {
		out[k] = true
	}
	return out
}

func accountNames(ix project.InstructionDeclaration) string {
	var n []string
	for _, a := range ix.Accounts {
		n = append(n, a.Name)
	}
	return strings.Join(n, ", ")
}

func aliased(ix project.InstructionDeclaration, a, b string) bool {
	for _, p := range ix.Aliases {
		if p.Left == a && p.Right == b || p.Left == b && p.Right == a {
			return true
		}
	}
	return false
}

func isSigner(d project.AccountDeclaration) bool { return d.Kind == "signer" || d.Signer }

// kindLabel is the account kind worth showing beside its privileges:
// "state Counter", "token", "program"; signers and raw accounts show none.
func kindLabel(d project.AccountDeclaration) string {
	switch d.Kind {
	case "state":
		return "state " + d.Layout
	case "token", "program":
		return d.Kind
	}
	return ""
}

func describeFlags(d project.AccountDeclaration) string {
	if k := kindLabel(d); k != "" {
		return Privileges(d.Access == "write", isSigner(d)) + " " + k
	}
	return Privileges(d.Access == "write", isSigner(d))
}

// Privileges renders account flags: writable signer, writable, signer or readonly.
func Privileges(writable, signer bool) string {
	switch {
	case writable && signer:
		return "writable signer"
	case writable:
		return "writable"
	case signer:
		return "signer"
	}
	return "readonly"
}

func (r *resolver) decl(name string) (project.AccountDeclaration, bool) {
	for _, a := range r.ix.Accounts {
		if a.Name == name {
			return a, true
		}
	}
	return project.AccountDeclaration{}, false
}

// derivable reports whether a missing argument can be filled from the schema.
func (r *resolver) derivable(f Field) bool {
	for _, a := range r.ix.Accounts {
		if a.PDA == nil || len(a.PDA.Seeds) == 0 {
			continue
		}
		last := a.PDA.Seeds[len(a.PDA.Seeds)-1]
		if last.Kind == "byte" && last.Field == "args."+f.Path {
			return true
		}
	}
	for _, rel := range r.ix.Relations {
		if rel.Field == "args."+f.Path && rel.Account != "" {
			return true
		}
	}
	return false
}

// payerAccount picks the signer account the fee payer fills by default: the
// only signer the caller has not supplied, else an init payer, else a signer
// named like an owner or payer.
func (r *resolver) payerAccount() string {
	var candidates []string
	for _, a := range r.ix.Accounts {
		if isSigner(a) && a.PDA == nil && a.Address == "" && a.Init == nil && r.in.Accounts[a.Name] == nil && r.in.Signers[a.Name] == nil {
			candidates = append(candidates, a.Name)
		}
	}
	// The payer already signs as another account the caller named.
	for name, key := range r.in.Accounts {
		if d, _ := r.decl(name); isSigner(d) && bytes.Equal(key, r.in.Payer) {
			return ""
		}
	}
	if len(candidates) == 1 {
		return candidates[0]
	}
	has := func(n string) bool {
		for _, c := range candidates {
			if c == n {
				return true
			}
		}
		return false
	}
	for _, a := range r.ix.Accounts {
		if a.Init != nil && has(a.Init.Payer) {
			return a.Init.Payer
		}
	}
	for _, n := range []string{"Payer", "Owner", "Authority", "Creator", "User", "Caller", "Signer", "Admin"} {
		if has(n) {
			return n
		}
	}
	return ""
}

func (r *resolver) resolve(name string) (*Account, error) {
	if a := r.done[name]; a != nil {
		return a, nil
	}
	if r.active[name] {
		return nil, errCycle
	}
	d, ok := r.decl(name)
	if !ok {
		return nil, fmt.Errorf("no account %s in %s", name, r.ix.Name)
	}
	r.active[name] = true
	defer delete(r.active, name)
	a := &Account{Name: name, Kind: d.Kind, Signer: isSigner(d), Writable: d.Access == "write"}
	key, source, err := r.key(d)
	if err != nil {
		return nil, err
	}
	a.Key, a.Source = key, source
	r.done[name] = a
	return a, nil
}

func (r *resolver) key(d project.AccountDeclaration) ([]byte, string, error) {
	if k := r.in.Signers[d.Name]; k != nil {
		r.signers = append(r.signers, k)
		pub := []byte(k.Public().(ed25519.PublicKey))
		if d.PDA != nil {
			r.matchBump(d, pub)
		}
		return pub, "-signer", nil
	}
	if k := r.in.Accounts[d.Name]; k != nil {
		if isSigner(d) && !bytes.Equal(k, r.in.Payer) && !r.in.Unsigned {
			return nil, "", fmt.Errorf("signs the transaction, so pass its keypair: -signer %s=<keypair file> (or -dry-run)", d.Name)
		}
		if d.PDA != nil {
			r.matchBump(d, k)
		}
		return k, "-account", nil
	}
	if d.Address != "" {
		k, err := hex.DecodeString(d.Address)
		if err != nil || len(k) != 32 {
			return nil, "", fmt.Errorf("invalid declared address %q", d.Address)
		}
		if n := WellKnown[network.Base58(k)]; n != "" {
			return k, n, nil
		}
		return k, "fixed address in the schema", nil
	}
	if d.PDA != nil {
		return r.derive(d, *d.PDA, "", nil)
	}
	if isSigner(d) {
		if d.Name == r.payerAt {
			return r.in.Payer, r.in.PayerSource, nil
		}
		if d.Init != nil {
			return nil, "", fmt.Errorf("is a new account that signs its own creation: pass -signer %s=<new keypair file>", d.Name)
		}
		return nil, "", fmt.Errorf("pass -signer %s=<keypair file> (the fee payer signs as %s)", d.Name, orNone(r.payerAt))
	}
	var notes []string
	if k, source, err := r.related(d); err == nil {
		return k, source, nil
	} else if err != errCycle && err != errNoRelation {
		notes = append(notes, err.Error())
	}
	if k, source, err := r.inferred(d); err == nil {
		return k, source, nil
	} else if err != errCycle && err != errNoRelation {
		notes = append(notes, err.Error())
	}
	msg := fmt.Sprintf("pass -account %s=<address>", d.Name)
	if len(notes) > 0 {
		msg += " (" + strings.Join(notes, "; ") + ")"
	}
	return nil, "", errors.New(msg)
}

func orNone(s string) string {
	if s == "" {
		return "no account"
	}
	return s
}

var errNoRelation = errors.New("no relation")

// related resolves an account from a key relation: an argument supplied on
// the command line, or a state field read from a resolved account.
func (r *resolver) related(d project.AccountDeclaration) ([]byte, string, error) {
	err := errNoRelation
	for _, rel := range r.ix.Relations {
		if rel.Account != d.Name {
			continue
		}
		if path, ok := strings.CutPrefix(rel.Field, "args."); ok {
			if v := r.values[path]; v != nil {
				f, _ := r.field(path)
				return v, "from -" + f.Flag, nil
			}
			continue
		}
		v, e := r.stateField(rel.Field, d.Name)
		if e == nil {
			return v, "read from " + rel.Field, nil
		}
		if e != errCycle {
			err = e
		}
	}
	return nil, "", err
}

func (r *resolver) field(path string) (Field, bool) {
	for _, f := range r.fields {
		if f.Path == path {
			return f, true
		}
	}
	return Field{}, false
}

// stateField reads Account.Field.Path from the resolved account's current
// data. It needs Fetch (unavailable in dry runs).
func (r *resolver) stateField(ref, requester string) ([]byte, error) {
	parts := strings.Split(ref, ".")
	owner, ok := r.decl(parts[0])
	if !ok || owner.Kind != "state" || owner.Name == requester {
		return nil, errCycle
	}
	if r.in.Fetch == nil {
		return nil, fmt.Errorf("%s is read from the cluster, which -dry-run skips", ref)
	}
	a, err := r.resolve(owner.Name)
	if err != nil {
		return nil, errCycle
	}
	layout, ok := r.s.State(owner.Layout)
	if !ok {
		return nil, fmt.Errorf("unknown layout %s", owner.Layout)
	}
	data, err := r.read(a.Key)
	if err != nil {
		return nil, fmt.Errorf("reading %s %s: %w", owner.Name, network.Base58(a.Key), err)
	}
	if len(data) != layout.Size || !discriminatorMatches(data, layout.Discriminator) {
		return nil, fmt.Errorf("%s %s is not an initialized %s account", owner.Name, network.Base58(a.Key), owner.Layout)
	}
	fields := layout.Fields
	var f project.WireField
	for i, p := range parts[1:] {
		found := false
		for _, c := range fields {
			if c.Name == p {
				f, fields, found = c, c.Fields, true
				break
			}
		}
		if !found || (i < len(parts)-2 && len(fields) == 0) {
			return nil, fmt.Errorf("%s has no field %s", owner.Layout, ref)
		}
	}
	return data[f.Offset : f.Offset+f.Size], nil
}

// read fetches an account's data once per Build.
func (r *resolver) read(key []byte) ([]byte, error) {
	if data, ok := r.fetched[string(key)]; ok {
		return data, nil
	}
	data, err := r.in.Fetch(key)
	if err == nil {
		r.fetched[string(key)] = data
	}
	return data, err
}

func discriminatorMatches(data []byte, d []int) bool {
	for i, b := range d {
		if data[i] != byte(b) {
			return false
		}
	}
	return true
}

// inferred derives a state account without its own PDA declaration from the
// same-named account of the same layout in another instruction, such as a
// counter created under a PDA and later passed to Increment.
func (r *resolver) inferred(d project.AccountDeclaration) ([]byte, string, error) {
	if d.Kind != "state" {
		return nil, "", errNoRelation
	}
	for _, other := range r.s.Instructions {
		if other.Name == r.ix.Name {
			continue
		}
		for _, o := range other.Accounts {
			if o.Name == d.Name && o.Kind == d.Kind && o.Layout == d.Layout && o.PDA != nil {
				k, source, err := r.derive(d, *o.PDA, other.Name, nil)
				if err != nil {
					return nil, "", fmt.Errorf("its PDA in %s: %v", other.Name, err)
				}
				return k, source, nil
			}
		}
	}
	return nil, "", errNoRelation
}

// matchBump fills an omitted trailing bump argument from a supplied address.
func (r *resolver) matchBump(d project.AccountDeclaration, key []byte) {
	seeds := d.PDA.Seeds
	last := seeds[len(seeds)-1]
	path, ok := strings.CutPrefix(last.Field, "args.")
	if last.Kind != "byte" || !ok || r.values[path] != nil {
		return
	}
	// On failure the argument stays missing and Build reports it.
	_, _, _ = r.derive(d, *d.PDA, "", key)
}

// derive computes a PDA from its seed declarations. origin names the
// instruction whose declaration is borrowed ("" for the account's own). A
// trailing byte seed that no argument, -seed or account state supplies is
// the canonical bump (or, with want, the bump that reproduces want).
func (r *resolver) derive(d project.AccountDeclaration, pda project.PDADeclaration, origin string, want []byte) ([]byte, string, error) {
	program, programName := r.in.Program, "this program"
	if pda.Program != "program_id" {
		p, err := r.resolve(pda.Program)
		if err != nil {
			return nil, "", fmt.Errorf("PDA program %s: %v", pda.Program, err)
		}
		program, programName = p.Key, pda.Program
	}
	var seeds [][]byte
	var shown []string
	bumpArg, bump := "", false
	for i, s := range pda.Seeds {
		last := i == len(pda.Seeds)-1
		switch s.Kind {
		case "bytes":
			b, _ := hex.DecodeString(s.Hex)
			seeds = append(seeds, b)
			shown = append(shown, showBytes(b))
			continue
		case "key":
			a, err := r.resolve(s.Account)
			if err != nil {
				if err == errCycle {
					return nil, "", fmt.Errorf("seed %s depends on this account", s.Account)
				}
				if _, declared := r.decl(s.Account); declared {
					return nil, "", fmt.Errorf("its PDA seed %s is unresolved", s.Account)
				}
				return nil, "", fmt.Errorf("seed %s: %v", s.Account, err)
			}
			seeds = append(seeds, a.Key)
			shown = append(shown, s.Account)
			continue
		}
		width, signed := seedWidth(s.Kind)
		if raw, ok := r.in.Seeds[s.Field]; ok {
			b, err := parseScalar(raw, width, signed)
			if err != nil {
				return nil, "", fmt.Errorf("-seed %s: %v", s.Field, err)
			}
			seeds = append(seeds, b)
			shown = append(shown, s.Field+"="+formatScalar(b, signed))
			continue
		}
		if path, isArg := strings.CutPrefix(s.Field, "args."); isArg && origin == "" {
			if v := r.values[path]; v != nil {
				seeds = append(seeds, v)
				shown = append(shown, s.Field+"="+formatScalar(v, signed))
				continue
			}
			if last && s.Kind == "byte" {
				bumpArg, bump = path, true
				continue
			}
			return nil, "", fmt.Errorf("seed %s needs -%s", s.Field, kebab(path))
		}
		if !strings.HasPrefix(s.Field, "args.") {
			v, err := r.stateField(s.Field, d.Name)
			if err == nil {
				seeds = append(seeds, v)
				shown = append(shown, s.Field+"="+formatScalar(v, signed))
				continue
			}
			if !(last && s.Kind == "byte") {
				hint := fmt.Sprintf("seed %s is unknown: pass -seed %s=<value>", s.Field, s.Field)
				if err != errCycle {
					hint += " (" + err.Error() + ")"
				}
				return nil, "", errors.New(hint)
			}
		} else if !(last && s.Kind == "byte") {
			return nil, "", fmt.Errorf("seed %s comes from %s's arguments: pass -seed %s=<value>", s.Field, origin, s.Field)
		}
		bump = true
	}
	var key []byte
	var b byte
	if bump {
		found := false
		if want != nil {
			for v := 255; v >= 0 && !found; v-- {
				if k, ok := network.CreateProgramAddress(append(append([][]byte(nil), seeds...), []byte{byte(v)}), program); ok && bytes.Equal(k, want) {
					key, b, found = k, byte(v), true
				}
			}
			if !found {
				return nil, "", fmt.Errorf("no bump makes %s the PDA", network.Base58(want))
			}
		} else if key, b, found = network.FindProgramAddress(seeds, program); !found {
			return nil, "", fmt.Errorf("no valid bump for the seeds")
		}
		shown = append(shown, fmt.Sprintf("bump %d", b))
		if bumpArg != "" {
			r.values[bumpArg] = []byte{b}
			r.sources[bumpArg] = "canonical bump of " + d.Name
			if want != nil {
				r.sources[bumpArg] = "bump of the supplied " + d.Name
			}
		}
	} else {
		var ok bool
		if key, ok = network.CreateProgramAddress(seeds, program); !ok {
			return nil, "", fmt.Errorf("seeds [%s] are not a valid PDA", strings.Join(shown, ", "))
		}
	}
	source := "PDA [" + strings.Join(shown, ", ") + "]"
	if programName != "this program" {
		source += " of " + programName
	}
	if origin != "" {
		source += " as declared in " + origin
	}
	return key, source, nil
}

func seedWidth(kind string) (int, bool) {
	switch kind {
	case "byte":
		return 1, false
	case "uint16":
		return 2, false
	case "uint32":
		return 4, false
	case "uint64":
		return 8, false
	case "int8":
		return 1, true
	case "int16":
		return 2, true
	case "int32":
		return 4, true
	}
	return 8, true // int64
}

func showBytes(b []byte) string {
	for _, c := range b {
		if c < 0x20 || c > 0x7e || c == '"' {
			return "0x" + hex.EncodeToString(b)
		}
	}
	return `"` + string(b) + `"`
}

// CustomError extracts {"InstructionError":[i,{"Custom":code}]}.
func CustomError(raw json.RawMessage) (index int, code uint64, ok bool) {
	var e struct {
		InstructionError []json.RawMessage
	}
	if json.Unmarshal(raw, &e) != nil || len(e.InstructionError) != 2 {
		return 0, 0, false
	}
	var custom struct{ Custom *uint64 }
	if json.Unmarshal(e.InstructionError[0], &index) != nil || json.Unmarshal(e.InstructionError[1], &custom) != nil || custom.Custom == nil {
		return 0, 0, false
	}
	return index, *custom.Custom, true
}

// ErrorName returns the schema's name for a program return code: a handler's
// Err* constant or a generated validation error (6000+), or "".
func ErrorName(s *project.ResolvedSchema, code uint64) string {
	var names []string
	for name, c := range s.Errors {
		if c == code {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return strings.Join(names, "/")
}
