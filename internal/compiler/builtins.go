package compiler

import (
	"debug/elf"
	_ "embed"
	"fmt"
	"sort"
	"strings"
)

//go:embed builtins.c.txt
var compilerBuiltins []byte

// These are the runtime syscalls actually emitted by this frontend. Compiler
// libcalls are linked locally, never admitted as if they were VM syscalls.
var legacySyscalls = map[string]bool{
	"abort":                      true,
	"sol_create_program_address": true,
	"sol_invoke_signed_c":        true,
	"sol_sha256":                 true,
	"sol_get_clock_sysvar":       true,
	"sol_get_rent_sysvar":        true,
	"sol_set_return_data":        true,
}

func undefinedSymbols(path string, dynamic bool) ([]string, error) {
	f, err := elf.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var symbols []elf.Symbol
	if dynamic {
		symbols, err = f.DynamicSymbols()
	} else {
		symbols, err = f.Symbols()
	}
	if err == elf.ErrNoSymbols {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, s := range symbols {
		if s.Section == elf.SHN_UNDEF && s.Name != "" {
			names = append(names, s.Name)
		}
	}
	sort.Strings(names)
	return names, nil
}

func validateSBFImports(path, arch string) error {
	names, err := undefinedSymbols(path, true)
	if err != nil {
		return fmt.Errorf("inspect SBF imports: %w", err)
	}
	var bad []string
	for _, name := range names {
		if arch != "v0" || !legacySyscalls[name] {
			bad = append(bad, name)
		}
	}
	if len(bad) > 0 {
		return fmt.Errorf("unresolved non-syscall SBF symbols: %s", strings.Join(bad, ", "))
	}
	return nil
}
