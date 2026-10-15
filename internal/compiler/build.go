package compiler

import (
	"crypto/sha256"
	_ "embed"
	"fmt"
	"gosvm/internal/targets"
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

//go:embed sbf-modern-v3.ld
var linkerScriptModernV3 []byte

// Build loads the restricted import graph and invokes Clang/LLD, with no Go runtime or Cargo.
func Build(source, output, llvm, arch string) error {
	return BuildWithOptions(source, output, llvm, arch, BuildOptions{})
}

// BuildOptions are compiler settings that do not come from Go source.
type BuildOptions struct {
	// HeapBytes is the invocation heap budget for allocation; 0 selects the
	// 32 KiB default. See ValidateHeapBytes and docs/GO_ALLOCATION.md.
	HeapBytes int
}

// readProgramWith reads source and applies the options to the program.
func readProgramWith(source string, o BuildOptions) (*Program, error) {
	if err := ValidateHeapBytes(o.HeapBytes); err != nil {
		return nil, err
	}
	program, err := ReadProgram(source)
	if err != nil {
		return nil, err
	}
	program.HeapBytes = o.HeapBytes
	return program, nil
}

// BuildWithOptions is Build with compiler options.
func BuildWithOptions(source, output, llvm, arch string, o BuildOptions) error {
	program, err := readProgramWith(source, o)
	if err != nil {
		return err
	}
	return buildProgram(program, output, llvm, arch)
}
func buildProgram(program *Program, output, llvm, arch string) error {
	libraryProfile := arch
	cpu, script := "generic", linkerScript
	modern := arch == target.Fogo || arch == target.Solana
	if modern {
		p, err := target.Resolve(arch)
		if err != nil {
			return err
		}
		if err = p.CheckBackend(llvm); err != nil {
			return err
		}
		arch = p.Arch
	} else if arch == target.Legacy {
		arch = "v3"
	}
	switch arch {
	case "v0":
	case "v3":
		cpu, script = "v3", linkerScriptV3
		if modern {
			script = linkerScriptModernV3
		}
	default:
		return fmt.Errorf("unsupported SBF target %q", arch)
	}
	c, functionNames, err := compileProgramNamed(program)
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
	// compileOutput is what the Clang steps printed although they
	// succeeded (frameDiagnostics).
	var compileOutput []byte
	run := func(args []string) error {
		out, err := exec.Command(args[0], args[1:]...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("%s: %w\n%s", args[0], err, out)
		}
		if filepath.Base(args[0]) == "clang" {
			compileOutput = append(compileOutput, out...)
		}
		return nil
	}
	var linkedObjects []string
	if len(program.Libraries) > 0 {
		// Bind pure-library calls within the final image, rather than leaving
		// preemptible function relocations for the SBF loader.
		steps[1] = append(steps[1], "-Bsymbolic")
	}
	staged := map[string]bool{}
	for _, binding := range program.Libraries {
		objects, err := binding.Objects(libraryProfile)
		if err != nil {
			return err
		}
		for _, object := range objects {
			data, ok := binding.Metadata[object]
			if !ok {
				return fmt.Errorf("missing captured library object %s", object)
			}
			digest := fmt.Sprintf("%x", sha256.Sum256(data))
			if staged[digest] {
				continue
			}
			staged[digest] = true
			path := filepath.Join(dir, "library-"+digest+".o")
			if err = os.WriteFile(path, data, 0644); err != nil {
				return err
			}
			linkedObjects = append(linkedObjects, path)
			steps[1] = append(steps[1], path)
		}
	}

	if err = run(steps[0]); err != nil {
		return err
	}
	if err = frameDiagnostics(compileOutput, functionNames); err != nil {
		return err
	}
	// helperObject compiles a helper source like program.c and links it.
	helperObject := func(name string, source []byte) error {
		helper := filepath.Join(dir, name+".c")
		object := filepath.Join(dir, name+".o")
		if err := os.WriteFile(helper, source, 0644); err != nil {
			return err
		}
		args := append([]string(nil), steps[0]...)
		for i := 0; i < len(args)-1; i++ {
			if args[i] == "-c" {
				args[i+1] = helper
			}
			if args[i] == "-o" {
				args[i+1] = object
			}
		}
		if err := run(args); err != nil {
			return err
		}
		steps[1] = append(steps[1], object)
		return nil
	}
	if arch == "v0" || modern {
		names, inspectErr := undefinedSymbols(filepath.Join(dir, "program.o"), false)
		for _, object := range linkedObjects {
			more, err := undefinedSymbols(object, false)
			if err != nil {
				return err
			}
			names = append(names, more...)
		}

		if inspectErr != nil {
			return fmt.Errorf("inspect compiler helpers: %w", inspectErr)
		}
		for _, name := range names {
			if name != "__multi3" {
				continue
			}
			if err = helperObject("builtins", compilerBuiltins); err != nil {
				return err
			}
			break
		}
	}
	if len(program.Libraries) == 0 {
		// LLVM may lower a copy of a parameter taken through an opaque
		// pointer (byval.go) to memmove; programs with libraries define
		// it in program.c.
		names, err := undefinedSymbols(filepath.Join(dir, "program.o"), false)
		if err != nil {
			return fmt.Errorf("inspect compiler helpers: %w", err)
		}
		for _, name := range names {
			if name == "memmove" {
				if err = helperObject("memmove", []byte(memmoveHelper)); err != nil {
					return err
				}
				break
			}
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
	if modern {
		flags := uint32(0)
		if arch == "v3" {
			flags = 3
		}
		if err = target.ValidateELF(data, flags, false); err != nil {
			return err
		}
	}
	return os.WriteFile(output, data, 0644)
}
