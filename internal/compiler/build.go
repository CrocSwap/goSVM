package compiler

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

//go:embed abi.c.txt
var ABI string

//go:embed sbf.ld
var linkerScript []byte

//go:embed sbf-v3.ld
var linkerScriptV3 []byte

// Build loads the restricted import graph and invokes Clang/LLD, with no Go runtime or Cargo.
func Build(source, output, llvm, arch string) error {
	program, err := ReadProgram(source)
	if err != nil {
		return err
	}
	return buildProgram(program, output, llvm, arch)
}
func buildProgram(program *Program, output, llvm, arch string) error {
	cpu, script := "generic", linkerScript
	switch arch {
	case "v0":
	case "v3":
		cpu, script = "v3", linkerScriptV3
	default:
		return fmt.Errorf("unsupported SBF target %q", arch)
	}
	c, err := CompileProgram(program)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(output), 0755); err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "gosvm-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	for name, data := range map[string][]byte{"program.c": append(c, []byte(ABI)...), "sbf.ld": script} {
		if err = os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
			return err
		}
	}
	steps := [][]string{
		{filepath.Join(llvm, "bin", "clang"), "-target", "sbf", "-mcpu=" + cpu, "-O2", "-fno-builtin", "-fPIC", "-fno-stack-protector", "-std=c11", "-Werror", "-c", filepath.Join(dir, "program.c"), "-o", filepath.Join(dir, "program.o")},
		{filepath.Join(llvm, "bin", "ld.lld"), "-z", "notext", "-shared", "--Bdynamic", "--strip-all", "--entry", "entrypoint", "--script", filepath.Join(dir, "sbf.ld"), "-o", filepath.Join(dir, "program.so"), filepath.Join(dir, "program.o")},
	}
	if arch == "v3" {
		steps[0] = append(steps[0], "-DGOSVM_SBF_V3=1")
		steps[1] = append(steps[1], "--no-undefined")
	}
	run := func(args []string) error {
		out, err := exec.Command(args[0], args[1:]...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("%s: %w\n%s", args[0], err, out)
		}
		return nil
	}
	if err = run(steps[0]); err != nil {
		return err
	}
	if arch == "v0" {
		names, inspectErr := undefinedSymbols(filepath.Join(dir, "program.o"), false)
		if inspectErr != nil {
			return fmt.Errorf("inspect compiler helpers: %w", inspectErr)
		}
		for _, name := range names {
			if name != "__multi3" {
				continue
			}
			helper := filepath.Join(dir, "builtins.c")
			object := filepath.Join(dir, "builtins.o")
			if err = os.WriteFile(helper, compilerBuiltins, 0644); err != nil {
				return err
			}
			args := append([]string(nil), steps[0]...)
			args[len(args)-3], args[len(args)-1] = helper, object
			if err = run(args); err != nil {
				return err
			}
			steps[1] = append(steps[1], object)
			break
		}
	}
	if err = run(steps[1]); err != nil {
		return err
	}
	if err = validateSBFImports(filepath.Join(dir, "program.so"), arch); err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(dir, "program.so"))
	if err != nil {
		return err
	}
	return os.WriteFile(output, data, 0644)
}
