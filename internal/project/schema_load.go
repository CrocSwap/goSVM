package project

import (
	"fmt"

	"gosvm/internal/compiler"
)

// ResolvedSchema is a validated schema-2 project with every wire layout
// resolved against the shared Go types. It is exactly what `gosvm generate`
// uses to produce the account checks, codecs, client and IDL, exposed so an
// alternative backend can consume the same declarations.
type ResolvedSchema struct {
	// Config is the validated gosvm.json (schema 2).
	Config Config
	// Package is the handler package name.
	Package string

	// States are the account state layouts in Config.Layouts order. Each has
	// its eight-byte discriminator, total size (discriminator included) and
	// packed little-endian fields with absolute byte offsets.
	States []WireLayout
	// Arguments are the instruction argument layouts; Arguments[i] belongs to
	// Instructions[i]. Discriminators select the instruction.
	Arguments []WireLayout
	// Instructions are the declarations in dispatch order: accounts in their
	// required transaction order with kind, access, signer, owner, address,
	// state layout, PDA seeds and init/close lifecycle, plus key relations and
	// permitted aliases. All of them passed schema validation.
	Instructions []InstructionDeclaration

	// Errors maps generated validation errors (6000+) and the handler package's
	// Err* constants (1..5999) to their program return codes.
	Errors map[string]uint64
	// ValidationOrder is the order in which the generated adapter applies
	// account and argument checks before calling the handler.
	ValidationOrder []string

	// Types is the Go type universe the layouts were resolved in; GoType
	// fields of States, Arguments and WireField refer to it. It includes the
	// provisional bundle declarations, not the generated adapter.
	Types *compiler.TypedProgram

	stateByName map[string]int
}

// LoadSchema loads and validates a schema-2 project and resolves its layouts
// without writing any file. It applies the same checks as generation,
// including the layouts.json compatibility lock, but does not regenerate
// library bindings or certify the handler's on-chain subset: run
// compiler.Check on the generated program for that.
func LoadSchema(dir string) (*ResolvedSchema, error) {
	c, err := Load(dir)
	if err != nil {
		return nil, err
	}
	if c.Schema != MultiSchema {
		return nil, fmt.Errorf("LoadSchema requires a schema-2 project; %s uses schema %d", dir, c.Schema)
	}
	m, err := resolveMulti(dir, c)
	if err != nil {
		return nil, err
	}
	if _, err = m.checkedLock(dir); err != nil {
		return nil, err
	}
	return &ResolvedSchema{
		Config:          c,
		Package:         m.Package,
		States:          m.States,
		Arguments:       m.Arguments,
		Instructions:    c.Instructions,
		Errors:          m.Errors,
		ValidationOrder: multiValidationOrder(c),
		Types:           m.Types,
		stateByName:     m.StateByName,
	}, nil
}

// State returns the account state layout declared under name.
func (s *ResolvedSchema) State(name string) (WireLayout, bool) {
	i, ok := s.stateByName[name]
	if !ok {
		return WireLayout{}, false
	}
	return s.States[i], true
}
