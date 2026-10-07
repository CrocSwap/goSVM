package project

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gosvm/internal/compiler"
)

// Schema 2 keeps wire declarations in ordinary shared Go packages. The schema
// names those types and separately defines runtime constraints and layout versions.
type WireDeclaration struct {
	Name          string `json:"name"`
	Type          string `json:"type"`
	Version       uint32 `json:"version"`
	Migration     string `json:"migration,omitempty"`
	Discriminator string `json:"discriminator,omitempty"` // Exactly eight hex bytes.
}

type InstructionDeclaration struct {
	Name          string               `json:"name"`
	Handler       string               `json:"handler"`
	Args          string               `json:"args"`
	Bundle        string               `json:"bundle"`
	Version       uint32               `json:"version"`
	Migration     string               `json:"migration,omitempty"`
	Discriminator string               `json:"discriminator,omitempty"`
	Accounts      []AccountDeclaration `json:"accounts"`
	Relations     []KeyRelation        `json:"relations,omitempty"`
	Aliases       []AccountAlias       `json:"aliases,omitempty"`
}

type AccountDeclaration struct {
	Name    string            `json:"name"`
	Kind    string            `json:"kind"` // state, signer, program, or account
	Layout  string            `json:"layout,omitempty"`
	Access  string            `json:"access"` // read or write; required even for raw references
	Signer  bool              `json:"signer,omitempty"`
	Owner   string            `json:"owner,omitempty"`   // program_id or 32 hex bytes
	Address string            `json:"address,omitempty"` // 32 hex bytes
	Ref     string            `json:"ref,omitempty"`     // Optional named reference beside decoded state/token value.
	PDA     *PDADeclaration   `json:"pda,omitempty"`
	Init    *InitDeclaration  `json:"init,omitempty"`
	Close   *CloseDeclaration `json:"close,omitempty"`
}

type InitDeclaration struct {
	Payer  string `json:"payer"`
	System string `json:"system"`
}

// AuthorityField is a canonical key field in the account being closed. Its
// initial value must match the declared transaction signer before the handler.
type CloseDeclaration struct {
	To             string `json:"to"`
	Authority      string `json:"authority"`
	AuthorityField string `json:"authority_field"`
}

type PDADeclaration struct {
	Program string            `json:"program"` // program_id or a declared executable program account
	Seeds   []SeedDeclaration `json:"seeds"`
}

// Seed declarations are data, not Go expressions. Scalars name canonical state/
// argument fields, keys name accounts, and fixed bytes use explicit hex encoding.
type SeedDeclaration struct {
	Kind    string `json:"kind"` // bytes, key, byte, uint32, uint64
	Hex     string `json:"hex,omitempty"`
	Account string `json:"account,omitempty"`
	Field   string `json:"field,omitempty"`
}

type KeyRelation struct {
	Field   string `json:"field"`            // state account followed by an exported field path
	Account string `json:"account"`          // The other account's key
	Equals  string `json:"equals,omitempty"` // Alternative canonical state/token key field
}

type AccountAlias struct {
	Left   string `json:"left"`
	Right  string `json:"right"`
	Reason string `json:"reason"`
}

// WireField is a packed, recursive layout independent of native alignment.
// Array elements in this subset are unsigned scalars; Fields describe structs.
type WireField struct {
	Name   string      `json:"name"`
	Type   string      `json:"type"`
	Offset int         `json:"offset"`
	Size   int         `json:"size"`
	Length int         `json:"length,omitempty"`
	Fields []WireField `json:"fields,omitempty"`
	GoType types.Type  `json:"-"`
}

type WireLayout struct {
	Kind          string      `json:"kind"`
	Name          string      `json:"name"`
	Type          string      `json:"type"` // Canonical Go import path + type name
	Version       uint32      `json:"version"`
	Migration     string      `json:"migration,omitempty"`
	Discriminator []int       `json:"discriminator"`
	Size          int         `json:"size"`
	Fields        []WireField `json:"fields"`
	GoType        types.Type  `json:"-"`
}

type multiModel struct {
	Config      Config
	Package     string
	Program     *compiler.Program
	Types       *compiler.TypedProgram
	States      []WireLayout
	Arguments   []WireLayout
	Errors      map[string]uint64
	Imports     map[string]string // Import path -> stable generated qualifier
	StateByName map[string]int
}

func validateMultiConfig(c Config) error {
	if c.State != "" || c.Instruction != "" || c.Handler != "" || c.Result != "" {
		return fmt.Errorf("schema 2 cannot contain schema-1 binding names")
	}
	if len(c.Layouts) == 0 || len(c.Instructions) == 0 || len(c.Instructions) > 32 {
		return fmt.Errorf("schema 2 requires account layouts and 1..32 instructions")
	}
	for alias, path := range c.Imports {
		if !token.IsIdentifier(alias) || token.Lookup(alias).IsKeyword() || alias == "_" || alias == "solana" || strings.HasPrefix(alias, "gosvm") || !validModule(path) {
			return fmt.Errorf("invalid schema import %q = %q", alias, path)
		}
	}
	checkType := func(ref string) error {
		parts := strings.Split(ref, ".")
		if len(parts) != 2 || c.Imports[parts[0]] == "" || !identRE.MatchString(parts[1]) {
			return fmt.Errorf("wire type %q must name an exported type in a declared shared-package import", ref)
		}
		return nil
	}
	seenLayouts := map[string]bool{}
	for _, l := range c.Layouts {
		if !identRE.MatchString(l.Name) || seenLayouts[l.Name] || l.Version == 0 {
			return fmt.Errorf("invalid/duplicate account layout %q or zero version", l.Name)
		}
		seenLayouts[l.Name] = true
		if err := checkType(l.Type); err != nil {
			return err
		}
		if _, err := multiDiscriminator("account", l.Name, l.Version, l.Discriminator); err != nil {
			return err
		}
	}
	seenInstructions, seenBundles := map[string]bool{}, map[string]bool{}
	for _, ix := range c.Instructions {
		if !identRE.MatchString(ix.Name) || !identRE.MatchString(ix.Handler) || !identRE.MatchString(ix.Bundle) || ix.Bundle == "GosvmAccountRef" || seenInstructions[ix.Name] || seenBundles[ix.Bundle] || ix.Version == 0 {
			return fmt.Errorf("invalid/duplicate instruction or bundle %q, or zero version", ix.Name)
		}
		seenInstructions[ix.Name], seenBundles[ix.Bundle] = true, true
		if err := checkType(ix.Args); err != nil {
			return err
		}
		if _, err := multiDiscriminator("instruction", ix.Name, ix.Version, ix.Discriminator); err != nil {
			return err
		}
		if len(ix.Accounts) == 0 || len(ix.Accounts) > 16 {
			return fmt.Errorf("%s requires 1..16 declared accounts (schema 2 limit)", ix.Name)
		}
		names := map[string]AccountDeclaration{}
		for _, a := range ix.Accounts {
			if !identRE.MatchString(a.Name) || names[a.Name].Name != "" || (a.Access != "read" && a.Access != "write") {
				return fmt.Errorf("%s: invalid/duplicate account %q or access; require read/write", ix.Name, a.Name)
			}
			names[a.Name] = a
			switch a.Kind {
			case "state":
				if !seenLayouts[a.Layout] || a.Owner != "program_id" {
					return fmt.Errorf("%s.%s: state requires a known layout and program_id owner", ix.Name, a.Name)
				}
			case "token":
				if c.SDKVersion != 2 || a.Layout != "" || a.Owner != "" || a.Signer {
					return fmt.Errorf("%s.%s: token requires SDK 2, no owned layout/owner override and no signer", ix.Name, a.Name)
				}
			case "signer", "program", "account":
				if a.Layout != "" {
					return fmt.Errorf("%s.%s: only state accounts have layouts", ix.Name, a.Name)
				}
				if a.Kind == "program" && (a.Address == "" || a.Access != "read" || a.Signer) {
					return fmt.Errorf("%s.%s: program requires a fixed address, read access and no signer", ix.Name, a.Name)
				}
			default:
				return fmt.Errorf("%s.%s: unsupported account kind %q", ix.Name, a.Name, a.Kind)
			}
			for _, value := range []string{a.Address, a.Owner} {
				if value != "" && value != "program_id" {
					if _, err := hexKey(value); err != nil {
						return fmt.Errorf("%s.%s: %w", ix.Name, a.Name, err)
					}
				}
			}
			if a.Address == "program_id" {
				return fmt.Errorf("%s.%s: address must be 32 hex bytes", ix.Name, a.Name)
			}
		}
		if err := validateAccountConstraints(c, ix, names); err != nil {
			return err
		}
		if err := validateLifecycle(c, ix, names); err != nil {
			return err
		}
		for _, rel := range ix.Relations {
			parts := strings.Split(rel.Field, ".")
			if len(parts) < 2 || (parts[0] != "args" && names[parts[0]].Kind != "state" && names[parts[0]].Kind != "token") || (rel.Account == "" && rel.Equals == "") || (rel.Account != "" && rel.Equals != "") || (rel.Account != "" && names[rel.Account].Name == "") {
				return fmt.Errorf("%s: invalid key relationship %q -> %q", ix.Name, rel.Field, rel.Account)
			}
			for _, field := range parts[1:] {
				if !identRE.MatchString(field) {
					return fmt.Errorf("%s: invalid relation field %q", ix.Name, rel.Field)
				}
			}
			if rel.Equals != "" {
				parts := strings.Split(rel.Equals, ".")
				if len(parts) < 2 || (parts[0] != "args" && names[parts[0]].Kind != "state" && names[parts[0]].Kind != "token") {
					return fmt.Errorf("%s: invalid key relationship target %q", ix.Name, rel.Equals)
				}
				for _, field := range parts[1:] {
					if !identRE.MatchString(field) {
						return fmt.Errorf("%s: invalid relation field %q", ix.Name, rel.Equals)
					}
				}
			}
		}
		seenAliases := map[string]bool{}
		for _, pair := range ix.Aliases {
			l, r := names[pair.Left], names[pair.Right]
			key := aliasKey(pair.Left, pair.Right)
			if l.Name == "" || r.Name == "" || l.Name == r.Name || strings.TrimSpace(pair.Reason) == "" || seenAliases[key] {
				return fmt.Errorf("%s: invalid/duplicate alias pair %q/%q or missing reason", ix.Name, pair.Left, pair.Right)
			}
			if l.Kind == "state" && r.Kind == "state" && (l.Access == "write" || r.Access == "write") {
				return fmt.Errorf("%s: alias pair %s/%s conflicts with mutable state views", ix.Name, pair.Left, pair.Right)
			}
			seenAliases[key] = true
		}
	}
	return nil
}

func aliasKey(l, r string) string {
	if l > r {
		l, r = r, l
	}
	return l + "/" + r
}

func hexKey(value string) ([]byte, error) {
	b, err := hex.DecodeString(value)
	if err != nil || len(b) != 32 {
		return nil, fmt.Errorf("key must contain exactly 32 hex bytes: %q", value)
	}
	return b, nil
}

func multiDiscriminator(kind, name string, version uint32, explicit string) ([]int, error) {
	if explicit == "" {
		return discriminator(kind, fmt.Sprintf("%s:v%d", name, version)), nil
	}
	b, err := hex.DecodeString(explicit)
	if err != nil || len(b) != 8 {
		return nil, fmt.Errorf("%s %s: discriminator requires exactly eight hex bytes", kind, name)
	}
	out := make([]int, 8)
	for i := range b {
		out[i] = int(b[i])
	}
	return out, nil
}

func schema2Sources(dir string) ([]compiler.Source, string, error) {
	sources, err := compiler.ReadSources(dir)
	if err != nil {
		return nil, "", err
	}
	var out []compiler.Source
	name := ""
	for _, src := range sources {
		if filepath.Base(src.Name) == "zz_gosvm.go" {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), src.Name, src.Data, parser.PackageClauseOnly)
		if err != nil {
			return nil, "", err
		}
		if name != "" && name != f.Name.Name {
			return nil, "", fmt.Errorf("%s: package mismatch", src.Name)
		}
		name = f.Name.Name
		out = append(out, src)
	}
	if name == "" {
		return nil, "", fmt.Errorf("schema 2 requires handwritten handler sources")
	}
	return out, name, nil
}

func resolveMulti(dir string, c Config) (*multiModel, error) {
	sources, name, err := schema2Sources(dir)
	if err != nil {
		return nil, err
	}
	// Provisional declarations exist only in memory. They let ordinary Go type
	// checking resolve handler signatures before generated bundles exist on disk.
	var stub bytes.Buffer
	fmt.Fprintf(&stub, "package %s\nimport (\n", name)
	aliases := make([]string, 0, len(c.Imports))
	for alias := range c.Imports {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)
	for _, alias := range aliases {
		fmt.Fprintf(&stub, "%s %q\n", alias, c.Imports[alias])
	}
	if hasToken(c) {
		fmt.Fprint(&stub, "gosvmtoken \"gosvm/sdk/token\"\n")
	}
	if hasPDA(c) {
		fmt.Fprint(&stub, "gosvmpda \"gosvm/sdk/pda\"\n")
	}
	if hasInit(c) {
		fmt.Fprint(&stub, "gosvmsystem \"gosvm/sdk/system\"\n")
	}
	fmt.Fprint(&stub, ")\ntype GosvmAccountRef struct { Index uint64; Key [32]byte }\n")
	if hasPDA(c) {
		fmt.Fprint(&stub, "type gosvmSeedBuilder = gosvmpda.Seeds\n")
	}
	if hasInit(c) {
		fmt.Fprint(&stub, "const gosvmInitRentLimit = gosvmsystem.MaxDataLen\n")
	}
	for _, l := range c.Layouts {
		fmt.Fprintf(&stub, "type gosvmLayout%s = %s\n", l.Name, l.Type)
	}
	for _, ix := range c.Instructions {
		fmt.Fprintf(&stub, "type gosvmArgs%s = %s\ntype %s struct {\n", ix.Name, ix.Args, ix.Bundle)
		for _, a := range ix.Accounts {
			typeName := "GosvmAccountRef"
			if a.Kind == "state" {
				for _, l := range c.Layouts {
					if l.Name == a.Layout {
						typeName = l.Type
						break
					}
				}
			} else if a.Kind == "token" {
				typeName = "gosvmtoken.Account"
			}
			fmt.Fprintf(&stub, "%s %s\n", a.Name, typeName)
			if a.Ref != "" {
				fmt.Fprintf(&stub, "%s GosvmAccountRef\n", a.Ref)
			}
		}
		fmt.Fprint(&stub, "}\n")
	}
	program, err := compiler.ReadProgramWithSources(dir, append(sources, compiler.Source{Name: filepath.Join(dir, "zz_gosvm.go"), Data: stub.Bytes()}))
	if err != nil {
		return nil, err
	}
	typed, err := compiler.TypeCheckProgram(program)
	if err != nil {
		return nil, err
	}
	m := &multiModel{Config: c, Package: name, Program: program, Types: typed, Imports: map[string]string{}, StateByName: map[string]int{}, Errors: map[string]uint64{
		"AccountCount": 6000, "AccountFlags": 6001, "AccountOwner": 6002, "StateLayout": 6003, "InstructionLayout": 6004, "AccountIdentity": 6005, "AccountAlias": 6006, "AccountRelation": 6007,
	}}
	if hasPDA(c) {
		m.Errors["AccountPDA"] = 6008
	}
	if hasInit(c) {
		m.Errors["AccountInit"] = 6009
	}
	paths := make([]string, 0, len(typed.Packages))
	for path := range typed.Packages {
		if path != typed.Entry.Path() {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	for i, path := range paths {
		m.Imports[path] = fmt.Sprintf("model%d", i)
	}
	resolveType := func(ref string) (types.Type, error) {
		parts := strings.Split(ref, ".")
		pkg := typed.Packages[c.Imports[parts[0]]]
		if pkg == nil {
			return nil, fmt.Errorf("shared type package %q not loaded", ref)
		}
		if pkg == typed.Entry {
			return nil, fmt.Errorf("wire types must be in a shared package, not the handler package")
		}
		seen := map[*types.Package]bool{}
		var pure func(*types.Package) bool
		pure = func(p *types.Package) bool {
			if seen[p] {
				return true
			}
			seen[p] = true
			if p.Path() == "gosvm/solana" {
				return false
			}
			for _, dep := range p.Imports() {
				if !pure(dep) {
					return false
				}
			}
			return true
		}
		if !pure(pkg) {
			return nil, fmt.Errorf("shared wire package %s depends on the runtime SDK", pkg.Path())
		}
		obj, ok := pkg.Scope().Lookup(parts[1]).(*types.TypeName)
		if !ok || obj.IsAlias() {
			return nil, fmt.Errorf("%s must be a declared named Go type", ref)
		}
		if _, ok := obj.Type().Underlying().(*types.Struct); !ok {
			return nil, fmt.Errorf("%s must be a struct", ref)
		}
		return obj.Type(), nil
	}
	seenDisc := map[string]string{}
	add := func(kind, name, ref string, version uint32, migration, explicit string) (WireLayout, error) {
		t, err := resolveType(ref)
		if err != nil {
			return WireLayout{}, err
		}
		disc, err := multiDiscriminator(kind, name, version, explicit)
		if err != nil {
			return WireLayout{}, err
		}
		key := fmt.Sprintf("%v", disc)
		if previous := seenDisc[key]; previous != "" {
			return WireLayout{}, fmt.Errorf("discriminator collision: %s and %s %s", previous, kind, name)
		}
		seenDisc[key] = kind + " " + name
		root, err := m.wireField(name, t, 8, map[types.Type]bool{})
		if err != nil {
			return WireLayout{}, err
		}
		if root.Size+8 > 1024 {
			return WireLayout{}, fmt.Errorf("%s %s: encoded size exceeds 1024 bytes", kind, name)
		}
		return WireLayout{Kind: kind, Name: name, Type: canonicalType(t), Version: version, Migration: migration, Discriminator: disc, Size: 8 + root.Size, Fields: root.Fields, GoType: t}, nil
	}
	for _, l := range c.Layouts {
		resolved, err := add("account", l.Name, l.Type, l.Version, l.Migration, l.Discriminator)
		if err != nil {
			return nil, err
		}
		m.StateByName[l.Name] = len(m.States)
		m.States = append(m.States, resolved)
	}
	for _, ix := range c.Instructions {
		resolved, err := add("instruction", ix.Name, ix.Args, ix.Version, ix.Migration, ix.Discriminator)
		if err != nil {
			return nil, err
		}
		m.Arguments = append(m.Arguments, resolved)
		if err := m.checkLifecycleFields(ix, resolved.GoType); err != nil {
			return nil, err
		}
		if err := m.checkHandler(ix, resolved.GoType); err != nil {
			return nil, err
		}
		for _, rel := range ix.Relations {
			fields := []string{rel.Field}
			if rel.Equals != "" {
				fields = append(fields, rel.Equals)
			}
			for _, field := range fields {
				t, _, err := m.constraintField(ix, resolved.GoType, field, true)
				if err != nil {
					return nil, err
				}
				array, ok := t.Underlying().(*types.Array)
				if !ok || array.Len() != 32 || !types.Identical(array.Elem().Underlying(), types.Typ[types.Uint8]) {
					return nil, fmt.Errorf("%s: relation %s requires a [32]byte key", ix.Name, field)
				}
			}
		}
		if err := m.checkPDAFields(ix, resolved.GoType); err != nil {
			return nil, err
		}
	}
	for _, name := range typed.Entry.Scope().Names() {
		if !strings.HasPrefix(name, "Err") {
			continue
		}
		obj, ok := typed.Entry.Scope().Lookup(name).(*types.Const)
		if !ok {
			continue
		}
		code, ok := constant.Uint64Val(obj.Val())
		if !ok || code == 0 || code >= 6000 {
			return nil, fmt.Errorf("%s: handler error %s must be 1..5999", typed.Fset.Position(obj.Pos()), name)
		}
		m.Errors[name] = code
	}
	return m, nil
}

func canonicalType(t types.Type) string {
	return types.TypeString(t, func(p *types.Package) string { return p.Path() })
}

func (m *multiModel) typeName(t types.Type) string {
	return types.TypeString(t, func(p *types.Package) string {
		if p == m.Types.Entry {
			return ""
		}
		if p.Path() == "gosvm/solana" {
			return "solana"
		}
		return m.Imports[p.Path()]
	})
}

func (m *multiModel) wireField(name string, t types.Type, offset int, active map[types.Type]bool) (WireField, error) {
	f := WireField{Name: name, Type: canonicalType(t), Offset: offset, GoType: t}
	if active[t] {
		return f, fmt.Errorf("recursive wire type %s", t)
	}
	active[t] = true
	defer delete(active, t)
	switch u := t.Underlying().(type) {
	case *types.Basic:
		switch u.Kind() {
		case types.Uint8:
			f.Size = 1
		case types.Uint32:
			f.Size = 4
		case types.Uint64:
			f.Size = 8
		default:
			return f, fmt.Errorf("wire field %s: require byte, uint32 or uint64, got %s", name, t)
		}
	case *types.Array:
		elem, err := m.wireField(name+"[]", u.Elem(), 0, active)
		if err != nil {
			return f, err
		}
		if _, ok := u.Elem().Underlying().(*types.Basic); !ok || u.Len() > 1024 || int(u.Len())*elem.Size > 1024 {
			return f, fmt.Errorf("wire array %s requires bounded unsigned scalar elements", name)
		}
		f.Size, f.Length = int(u.Len())*elem.Size, int(u.Len())
	case *types.Struct:
		for i := 0; i < u.NumFields(); i++ {
			field := u.Field(i)
			if !field.Exported() || field.Embedded() || u.Tag(i) != "" {
				return f, fmt.Errorf("wire field %s.%s must be exported, unembedded and untagged", name, field.Name())
			}
			child, err := m.wireField(field.Name(), field.Type(), offset+f.Size, active)
			if err != nil {
				return f, err
			}
			f.Fields = append(f.Fields, child)
			f.Size += child.Size
			if f.Size > 1024 {
				return f, fmt.Errorf("wire struct %s exceeds 1024 bytes", name)
			}
		}
	default:
		return f, fmt.Errorf("wire field %s has unsupported type %s", name, t)
	}
	return f, nil
}

func (m *multiModel) checkHandler(ix InstructionDeclaration, args types.Type) error {
	obj, ok := m.Types.Entry.Scope().Lookup(ix.Handler).(*types.Func)
	if !ok {
		return fmt.Errorf("%s: handler %s not found", ix.Name, ix.Handler)
	}
	sig := obj.Type().(*types.Signature)
	bundle := m.Types.Entry.Scope().Lookup(ix.Bundle).Type()
	valid := !sig.Variadic() && sig.Params().Len() == 3 && sig.Results().Len() == 2
	if valid {
		ctx, ok := sig.Params().At(0).Type().(*types.Named)
		valid = ok && ctx.Obj().Pkg() != nil && ctx.Obj().Pkg().Path() == "gosvm/solana" && ctx.Obj().Name() == "Context" && types.Identical(sig.Params().At(1).Type(), bundle) && types.Identical(sig.Params().At(2).Type(), args) && types.Identical(sig.Results().At(0).Type(), bundle) && types.Identical(sig.Results().At(1).Type(), types.Typ[types.Uint64])
	}
	if !valid {
		return fmt.Errorf("%s: handler %s must accept (solana.Context, %s, %s) and return (%s, uint64)", m.Types.Fset.Position(obj.Pos()), ix.Handler, ix.Bundle, m.typeName(args), ix.Bundle)
	}
	return nil
}

type layoutLock struct {
	Format  string       `json:"format"`
	Layouts []WireLayout `json:"layouts"`
}

// A changed existing version is rejected by generate as well as check. An
// explicit higher version appends history instead of overwriting old layouts.
func (m *multiModel) checkedLock(dir string) ([]byte, error) {
	lock := layoutLock{Format: "gosvm-layouts-2"}
	b, err := os.ReadFile(filepath.Join(dir, "layouts.json"))
	if err == nil {
		d := json.NewDecoder(bytes.NewReader(b))
		d.DisallowUnknownFields()
		if err := d.Decode(&lock); err != nil {
			return nil, fmt.Errorf("layout lock: %w", err)
		}
		if err := d.Decode(new(any)); err != io.EOF {
			return nil, fmt.Errorf("layout lock must contain one object")
		}
		if lock.Format != "gosvm-layouts-2" {
			return nil, fmt.Errorf("unsupported layout lock format")
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	} else {
		previous, err := os.ReadFile(filepath.Join(dir, "zz_gosvm.go"))
		if err == nil && bytes.Contains(previous, []byte("schema 2")) {
			return nil, fmt.Errorf("schema-2 layout lock is missing; restore layouts.json before generation")
		}
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}
	seen := map[string]bool{}
	discs := map[uint64]string{}
	for _, l := range lock.Layouts {
		key := fmt.Sprintf("%s/%s/%d", l.Kind, l.Name, l.Version)
		if l.Version == 0 || seen[key] || (l.Kind != "account" && l.Kind != "instruction") || !identRE.MatchString(l.Name) || len(l.Discriminator) != 8 || l.Size < 8 || l.Size > 1024 || l.Type == "" {
			return nil, fmt.Errorf("invalid/duplicate historical layout %s", key)
		}
		for _, value := range l.Discriminator {
			if value < 0 || value > 255 {
				return nil, fmt.Errorf("invalid historical discriminator for %s", key)
			}
		}
		word := discWord(l.Discriminator)
		if previous := discs[word]; previous != "" {
			return nil, fmt.Errorf("historical discriminator collision: %s and %s", previous, key)
		}
		discs[word] = key
		seen[key] = true
	}
	current := append(append([]WireLayout(nil), m.States...), m.Arguments...)
	for _, l := range current {
		key := fmt.Sprintf("%s/%s/%d", l.Kind, l.Name, l.Version)
		if previous := discs[discWord(l.Discriminator)]; previous != "" && previous != key {
			return nil, fmt.Errorf("layout discriminator already used by %s; a new version must use a different discriminator", previous)
		}
		found := false
		latest := uint32(0)
		for _, previous := range lock.Layouts {
			if previous.Version != l.Version && previous.Kind == l.Kind && previous.Name == l.Name && discWord(previous.Discriminator) == discWord(l.Discriminator) {
				return nil, fmt.Errorf("%s %s: a new layout version must use a different discriminator", l.Kind, l.Name)
			}
			if previous.Kind != l.Kind || previous.Name != l.Name {
				continue
			}
			if previous.Version > latest {
				latest = previous.Version
			}
			if previous.Version != l.Version {
				continue
			}
			found = true
			old, _ := json.Marshal(previous)
			now, _ := json.Marshal(l)
			if !bytes.Equal(old, now) {
				return nil, fmt.Errorf("%s %s version %d layout changed; choose a higher explicit version and migration policy", l.Kind, l.Name, l.Version)
			}
		}
		if l.Version < latest {
			return nil, fmt.Errorf("%s %s: version %d is below recorded version %d", l.Kind, l.Name, l.Version, latest)
		}
		if !found {
			if latest != 0 && strings.TrimSpace(l.Migration) == "" {
				return nil, fmt.Errorf("%s %s: a new version requires a recorded migration policy", l.Kind, l.Name)
			}
			lock.Layouts = append(lock.Layouts, l)
		}
	}
	sort.Slice(lock.Layouts, func(i, j int) bool {
		a, b := lock.Layouts[i], lock.Layouts[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Version < b.Version
	})
	out, err := json.MarshalIndent(lock, "", "  ")
	return append(out, '\n'), err
}
