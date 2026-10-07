package project

import (
	"bytes"
	"fmt"
	"go/types"
	"strings"
)

func hasInit(c Config) bool {
	for _, ix := range c.Instructions {
		for _, a := range ix.Accounts {
			if a.Init != nil {
				return true
			}
		}
	}
	return false
}

func hasClose(c Config) bool {
	for _, ix := range c.Instructions {
		for _, a := range ix.Accounts {
			if a.Close != nil {
				return true
			}
		}
	}
	return false
}

func instructionLifecycle(ix InstructionDeclaration) bool {
	for _, a := range ix.Accounts {
		if a.Init != nil || a.Close != nil {
			return true
		}
	}
	return false
}

func validateLifecycle(c Config, ix InstructionDeclaration, names map[string]AccountDeclaration) error {
	for _, a := range ix.Accounts {
		if a.Init == nil && a.Close == nil {
			continue
		}
		if c.SDKVersion != 2 || a.Kind != "state" || a.Access != "write" || a.Init != nil && a.Close != nil {
			return fmt.Errorf("%s.%s: init/close requires SDK 2, writable state and one lifecycle action", ix.Name, a.Name)
		}
		for _, pair := range ix.Aliases {
			if pair.Left == a.Name || pair.Right == a.Name {
				return fmt.Errorf("%s.%s: lifecycle targets cannot have account aliases", ix.Name, a.Name)
			}
		}
		if a.Init != nil {
			payer, system := names[a.Init.Payer], names[a.Init.System]
			if payer.Name == "" || payer.Name == a.Name || payer.Kind != "signer" || payer.Access != "write" || payer.Owner != "" && payer.Owner != strings.Repeat("0", 64) {
				return fmt.Errorf("%s.%s: init payer must require a distinct writable System signer", ix.Name, a.Name)
			}
			if system.Kind != "program" || system.Address != strings.Repeat("0", 64) {
				return fmt.Errorf("%s.%s: init system must be a fixed System program account", ix.Name, a.Name)
			}
			if a.PDA == nil && !a.Signer || a.PDA != nil && a.PDA.Program != "program_id" {
				return fmt.Errorf("%s.%s: init target requires a transaction signer or PDA under program_id", ix.Name, a.Name)
			}
		}
		if a.Close != nil {
			to, authority := names[a.Close.To], names[a.Close.Authority]
			if to.Name == "" || to.Name == a.Name || to.Kind == "program" || to.Access != "write" || to.Close != nil || to.Init != nil {
				return fmt.Errorf("%s.%s: close requires a distinct writable non-lifecycle refund account", ix.Name, a.Name)
			}
			if authority.Name == "" || authority.Name == a.Name || authority.Kind != "signer" {
				return fmt.Errorf("%s.%s: close authority must require a distinct transaction signer", ix.Name, a.Name)
			}
			parts := strings.Split(a.Close.AuthorityField, ".")
			if len(parts) < 2 || parts[0] != a.Name {
				return fmt.Errorf("%s.%s: close authority_field must name this state's canonical key", ix.Name, a.Name)
			}
			for _, part := range parts[1:] {
				if !identRE.MatchString(part) {
					return fmt.Errorf("%s.%s: invalid close authority_field", ix.Name, a.Name)
				}
			}
		}
	}
	// Newly initialized fields are zero before the handler, so they cannot define
	// pre-handler relationships or PDA seeds. Seeds use args or existing state.
	for _, rel := range ix.Relations {
		for _, field := range []string{rel.Field, rel.Equals} {
			if names[strings.Split(field, ".")[0]].Init != nil {
				return fmt.Errorf("%s: relationship cannot read uninitialized state %s", ix.Name, field)
			}
		}
	}
	for _, a := range ix.Accounts {
		if a.PDA == nil {
			continue
		}
		for _, seed := range a.PDA.Seeds {
			if seed.Field != "" && names[strings.Split(seed.Field, ".")[0]].Init != nil {
				return fmt.Errorf("%s.%s: PDA seed cannot read uninitialized state %s", ix.Name, a.Name, seed.Field)
			}
		}
	}
	return nil
}

func (m *multiModel) checkLifecycleFields(ix InstructionDeclaration, args types.Type) error {
	for _, a := range ix.Accounts {
		if a.Close == nil {
			continue
		}
		t, _, err := m.constraintField(ix, args, a.Close.AuthorityField, false)
		if err != nil {
			return err
		}
		key, ok := t.Underlying().(*types.Array)
		if !ok || key.Len() != 32 || !types.Identical(key.Elem().Underlying(), types.Typ[types.Uint8]) {
			return fmt.Errorf("%s.%s: close authority_field requires a [32]byte key", ix.Name, a.Name)
		}
	}
	return nil
}

func (m *multiModel) initTargets(out *bytes.Buffer, ix InstructionDeclaration) {
	checkedPayer := map[string]bool{}
	for j, a := range ix.Accounts {
		if a.Init == nil {
			continue
		}
		fmt.Fprintf(out, "initOwner%d:=solana.Owner(c,%d);if len(initOwner%d)!=32 || len(solana.Data(c,%d))!=0 || solana.Lamports(c,%d)!=0 { return 6009 };for k:=uint64(0);k<32;k++ { if initOwner%d[k]!=0 { return 6009 } }\n", j, j, j, j, j, j)
		if !checkedPayer[a.Init.Payer] {
			payer := accountIndex(ix, a.Init.Payer)
			fmt.Fprintf(out, "payerOwner%d:=solana.Owner(c,%d);if len(payerOwner%d)!=32 || len(solana.Data(c,%d))!=0 { return 6009 };for k:=uint64(0);k<32;k++ { if payerOwner%d[k]!=0 { return 6009 } }\n", payer, payer, payer, payer, payer)
			checkedPayer[a.Init.Payer] = true
		}
	}
}

func (m *multiModel) closeAuthorities(out *bytes.Buffer, ix InstructionDeclaration, args WireLayout) {
	for _, a := range ix.Accounts {
		if a.Close != nil {
			_, field, _ := m.constraintField(ix, args.GoType, a.Close.AuthorityField, false)
			fmt.Fprintf(out, "for j:=uint64(0);j<32;j++ { if %s[j]!=key%d[j] { return 6007 } }\n", field, accountIndex(ix, a.Close.Authority))
		}
	}
}

func (m *multiModel) initAdapter(out *bytes.Buffer, ix InstructionDeclaration, a AccountDeclaration, args WireLayout) {
	alias := m.Imports["gosvm/sdk/system"]
	l := m.States[m.StateByName[a.Layout]]
	fmt.Fprintf(out, "func gosvmInit_%s_%s(c solana.Context,accounts %s,args %s,owner [32]byte) uint64 {\n", ix.Name, a.Name, ix.Bundle, m.typeName(args.GoType))
	if a.PDA != nil {
		fmt.Fprintf(out, "var seeds %s.Seeds\n", m.Imports["gosvm/sdk/pda"])
		m.seedAdapter(out, ix, a, args)
		fmt.Fprintf(out, "return %s.CreateSigned(c,%d,%d,%d,%d,owner,&seeds)\n}\n", alias, accountIndex(ix, a.Init.System), accountIndex(ix, a.Init.Payer), accountIndex(ix, a.Name), l.Size)
	} else {
		fmt.Fprintf(out, "return %s.Create(c,%d,%d,%d,%d,owner)\n}\n", alias, accountIndex(ix, a.Init.System), accountIndex(ix, a.Init.Payer), accountIndex(ix, a.Name), l.Size)
	}
}
