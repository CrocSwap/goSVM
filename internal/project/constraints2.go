package project

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"go/types"
	"strings"
)

func hasToken(c Config) bool {
	for _, ix := range c.Instructions {
		for _, a := range ix.Accounts {
			if a.Kind == "token" {
				return true
			}
		}
	}
	return false
}

func hasPDA(c Config) bool {
	for _, ix := range c.Instructions {
		for _, a := range ix.Accounts {
			if a.PDA != nil {
				return true
			}
		}
	}
	return false
}

func relationArgs(ix InstructionDeclaration) bool {
	for _, rel := range ix.Relations {
		if strings.HasPrefix(rel.Field, "args.") || strings.HasPrefix(rel.Equals, "args.") {
			return true
		}
	}
	return false
}

func multiValidationOrder(c Config) []string {
	order := []string{"instruction_layout", "account_count", "privileges", "identity_owner", "alias_policy", "state_layout"}
	if hasInit(c) {
		order = append(order[:5], "init_target", "state_layout")
	}
	if hasToken(c) {
		order = append(order, "token_layout")
	}
	for _, ix := range c.Instructions {
		if relationArgs(ix) {
			order = append(order, "instruction_arguments")
			break
		}
	}
	order = append(order, "relations")
	if hasClose(c) {
		order = append(order, "close_authority")
	}
	if hasPDA(c) {
		order = append(order, "pda")
	}
	if hasInit(c) {
		order = append(order, "initialize")
	}
	order = append(order, "handler")
	if hasClose(c) {
		order = append(order, "owned_state_close")
	}
	return append(order, "writable_state_commit")
}

func validateAccountConstraints(c Config, ix InstructionDeclaration, names map[string]AccountDeclaration) error {
	fields := map[string]bool{}
	for name := range names {
		fields[name] = true
	}
	for _, a := range ix.Accounts {
		if a.Ref != "" {
			if c.SDKVersion != 2 || (a.Kind != "state" && a.Kind != "token") || !identRE.MatchString(a.Ref) || fields[a.Ref] {
				return fmt.Errorf("%s.%s: ref requires SDK 2, state/token and a distinct exported bundle field", ix.Name, a.Name)
			}
			fields[a.Ref] = true
		}
		if a.PDA == nil {
			continue
		}
		p := a.PDA
		if c.SDKVersion != 2 || a.Kind == "program" || a.Signer || a.Kind == "signer" {
			return fmt.Errorf("%s.%s: PDA requires SDK 2 and a non-program, non-transaction-signer account", ix.Name, a.Name)
		}
		if p.Program != "program_id" && names[p.Program].Kind != "program" {
			return fmt.Errorf("%s.%s: PDA program must be program_id or a declared program account", ix.Name, a.Name)
		}
		if len(p.Seeds) > 16 {
			return fmt.Errorf("%s.%s: PDA permits at most 16 seeds including bump", ix.Name, a.Name)
		}
		for _, seed := range p.Seeds {
			switch seed.Kind {
			case "bytes":
				b, err := hex.DecodeString(seed.Hex)
				if err != nil || len(b) > 32 || seed.Account != "" || seed.Field != "" {
					return fmt.Errorf("%s.%s: bytes seed requires at most 32 hex-encoded bytes only", ix.Name, a.Name)
				}
			case "key":
				if names[seed.Account].Name == "" || seed.Hex != "" || seed.Field != "" {
					return fmt.Errorf("%s.%s: key seed requires a declared account only", ix.Name, a.Name)
				}
			case "byte", "uint32", "uint64":
				parts := strings.Split(seed.Field, ".")
				if len(parts) < 2 || (parts[0] != "args" && names[parts[0]].Kind != "state") || seed.Hex != "" || seed.Account != "" {
					return fmt.Errorf("%s.%s: scalar seed requires an args or state field only", ix.Name, a.Name)
				}
				for _, field := range parts[1:] {
					if !identRE.MatchString(field) {
						return fmt.Errorf("%s.%s: invalid seed field %q", ix.Name, a.Name, seed.Field)
					}
				}
			default:
				return fmt.Errorf("%s.%s: unsupported PDA seed kind %q", ix.Name, a.Name, seed.Kind)
			}
		}
	}
	return nil
}

// Resolve field paths against canonical types; emit selectors only after every
// component has been checked. Token fields come from freshly decoded SDK state.
func (m *multiModel) constraintField(ix InstructionDeclaration, args types.Type, field string, allowArgs bool) (types.Type, string, error) {
	parts := strings.Split(field, ".")
	var t types.Type
	expr := "accounts." + field
	if allowArgs && parts[0] == "args" {
		t = args
		expr = field
	} else {
		for j, a := range ix.Accounts {
			if a.Name == parts[0] {
				if a.Kind == "state" {
					t = m.States[m.StateByName[a.Layout]].GoType
				}
				if a.Kind == "token" {
					t = m.Types.Packages["gosvm/sdk/token"].Scope().Lookup("State").Type()
					expr = fmt.Sprintf("tokenState%d.%s", j, strings.Join(parts[1:], "."))
				}
			}
		}
	}
	if t == nil || len(parts) < 2 {
		return nil, "", fmt.Errorf("%s: unknown relationship field %s", ix.Name, field)
	}
	for _, name := range parts[1:] {
		st, ok := t.Underlying().(*types.Struct)
		if !ok {
			return nil, "", fmt.Errorf("%s: relation %s does not name a key field", ix.Name, field)
		}
		t = nil
		for j := 0; j < st.NumFields(); j++ {
			if st.Field(j).Name() == name && st.Field(j).Exported() {
				t = st.Field(j).Type()
				break
			}
		}
		if t == nil {
			return nil, "", fmt.Errorf("%s: unknown relationship field %s", ix.Name, field)
		}
	}
	return t, expr, nil
}

func (m *multiModel) checkPDAFields(ix InstructionDeclaration, args types.Type) error {
	for _, a := range ix.Accounts {
		if a.PDA != nil {
			for _, s := range a.PDA.Seeds {
				if s.Kind == "bytes" || s.Kind == "key" {
					continue
				}
				t, _, err := m.constraintField(ix, args, s.Field, true)
				if err != nil {
					return err
				}
				u, ok := t.Underlying().(*types.Basic)
				want := map[string]types.BasicKind{"byte": types.Uint8, "uint32": types.Uint32, "uint64": types.Uint64}[s.Kind]
				if !ok || u.Kind() != want {
					return fmt.Errorf("%s.%s: seed %s requires %s, got %s", ix.Name, a.Name, s.Field, s.Kind, t)
				}
			}
		}
	}
	return nil
}

func accountIndex(ix InstructionDeclaration, name string) int {
	for j, a := range ix.Accounts {
		if a.Name == name {
			return j
		}
	}
	panic("validated account is absent")
}

func (m *multiModel) pdaAdapter(out *bytes.Buffer, ix InstructionDeclaration, a AccountDeclaration, args WireLayout) {
	alias := m.Imports["gosvm/sdk/pda"]
	fmt.Fprintf(out, "func gosvmPDA_%s_%s(c solana.Context,accounts %s,args %s) uint64 {\nvar seeds %s.Seeds\n", ix.Name, a.Name, ix.Bundle, m.typeName(args.GoType), alias)
	m.seedAdapter(out, ix, a, args)
	source := "solana.ProgramID(c)"
	if a.PDA.Program != "program_id" {
		source = fmt.Sprintf("solana.Key(c,%d)", accountIndex(ix, a.PDA.Program))
	}
	fmt.Fprintf(out, "id:=%s; if len(id)!=32 { return 6008 };var program [32]byte;for j:=uint64(0);j<32;j++ { program[j]=id[j] }\n", source)
	fmt.Fprintf(out, "matches,code:=seeds.Matches(c,%d,program);if code!=0 || !matches { return 6008 };return 0\n}\n", accountIndex(ix, a.Name))
}

func (m *multiModel) seedAdapter(out *bytes.Buffer, ix InstructionDeclaration, a AccountDeclaration, args WireLayout) {
	for j, s := range a.PDA.Seeds {
		expr := ""
		switch s.Kind {
		case "bytes":
			b, _ := hex.DecodeString(s.Hex)
			fmt.Fprintf(out, "var literal%d [%d]byte\n", j, len(b))
			for k, v := range b {
				fmt.Fprintf(out, "literal%d[%d]=%d\n", j, k, v)
			}
			expr = fmt.Sprintf("seeds.AddBytes(literal%d[:])", j)
		case "key":
			expr = fmt.Sprintf("seeds.AddBytes(solana.Key(c,%d))", accountIndex(ix, s.Account))
		default:
			_, field, _ := m.constraintField(ix, args.GoType, s.Field, true)
			method := map[string]string{"byte": "AddByte", "uint32": "AddUint32", "uint64": "AddUint64"}[s.Kind]
			expr = fmt.Sprintf("seeds.%s(%s(%s))", method, s.Kind, field)
		}
		fmt.Fprintf(out, "if %s!=0 { return 6008 }\n", expr)
	}
}
