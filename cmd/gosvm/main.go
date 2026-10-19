package main

import (
	"flag"
	"fmt"
	"gosvm/internal/compiler"
	"gosvm/internal/targets"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	if len(os.Args) == 1 || os.Args[1] == "--help" || os.Args[1] == "-h" {
		fmt.Print(help)
		return
	}
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "toolchain", "runner", "new", "generate", "check", "build", "test", "svm-test", "doctor", "help", "version", "migrate-target", "library", "deploy", "call", "authority", "features":
			if err := projectCommand(os.Args[1:]); err != nil {
				fmt.Fprintln(os.Stderr, "gosvm:", err)
				os.Exit(1)
			}
			return
		}
	}
	if !strings.HasPrefix(os.Args[1], "-") {
		if _, e := os.Stat(os.Args[1]); e != nil {
			fmt.Fprintf(os.Stderr, "gosvm: unknown command or source %q; run gosvm help\n", os.Args[1])
			os.Exit(2)
		}
	}
	legacy()
}

func legacy() {
	output := flag.String("o", "build/amm-go.so", "output ELF (or C with -emit-c)")
	llvm := flag.String("llvm", llvmDefault(), "Solana platform-tools/llvm directory")
	emit := flag.Bool("emit-c", false, "emit C without compiling")
	arch := flag.String("arch", "v3", "SBF version: v3 (default) or v0")
	profileName := flag.String("target", "", "explicit profile; raw -arch retains historical semantics")
	noCache := flag.Bool("no-cache", false, "rebuild even if source, compiler, backend and output match the last build")
	heapBytes := flag.Int("heap-bytes", 0, "invocation heap budget for allocation: a multiple of 1024 from 32768 to 262144 (0: 32768); above 32768 every transaction must send ComputeBudget RequestHeapFrame")
	maxAccounts := flag.Int("max-accounts", 0, "incoming account limit: 0 (the SDK's: 16 for SDK 1, 32 for SDK 2) or 64, which runs Process in its own stack frame (docs/ACCOUNT_DECODER64.md)")
	var trim trimFlag
	flag.Var(&trim, "trim", "opt in to trimming for packages matching this import path or path/... pattern: only the declarations Process reaches are checked and compiled (repeatable; docs/GO_PACKAGES.md#trimming)")
	flag.Parse()
	if err := compiler.ValidateHeapBytes(*heapBytes); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if err := compiler.ValidateTrim(trim); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if err := compiler.ValidateMaxAccounts(*maxAccounts, 2); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	options := compiler.BuildOptions{HeapBytes: *heapBytes, MaxAccounts: *maxAccounts, Trim: trim}
	emitArch := *arch
	if *profileName != "" {
		explicitArch, explicitLLVM := false, false
		flag.Visit(func(f *flag.Flag) {
			if f.Name == "arch" {
				explicitArch = true
			}
			if f.Name == "llvm" {
				explicitLLVM = true
			}
		})
		if explicitArch {
			fmt.Fprintln(os.Stderr, "choose -target or historical -arch")
			os.Exit(2)
		}
		p, err := target.Resolve(*profileName)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		*arch, emitArch = p.Name, p.Arch
		if !explicitLLVM {
			*llvm = llvmForTools(p.Tools)
		}
	}
	if *profileName == "" && *arch != "v0" && *arch != "v3" {
		fmt.Fprintln(os.Stderr, "unsupported target:", *arch)
		os.Exit(2)
	}
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: gosvm [-o program.so] [-llvm directory] source.go")
		os.Exit(2)
	}
	if *llvm == "" {
		home, _ := os.UserHomeDir()
		*llvm = filepath.Join(home, ".cache/solana/v1.51/platform-tools/llvm")
	}
	var err error
	if *emit {
		var out []byte
		var src *compiler.Program
		src, err = compiler.ReadProgramWithOptions(flag.Arg(0), compiler.ReadOptions{Trim: options.Trim})
		if err == nil {
			src.HeapBytes, src.MaxAccounts = options.HeapBytes, options.MaxAccounts
			var checked *compiler.CheckedProgram
			if checked, err = compiler.Check(src); err == nil {
				out, err = compiler.EmitC(checked)
				printTrimSummary(checked.Trimmed, "")
			}
		}
		if err == nil {
			err = os.MkdirAll(filepath.Dir(*output), 0755)
		}
		if err == nil {
			if emitArch == "v3" {
				out = append([]byte("#define GOSVM_SBF_V3 1\n"), out...)
			}
			err = os.WriteFile(*output, append(out, []byte(compiler.ABI)...), 0644)
		}
	} else {
		if *noCache {
			err = compiler.BuildWithOptions(flag.Arg(0), *output, *llvm, *arch, options)
		} else {
			err = compiler.BuildCachedWithOptions(flag.Arg(0), *output, *llvm, *arch, options)
		}
		if err == nil {
			err = printTrimWarnings(*output)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// trimFlag collects repeated -trim patterns.
type trimFlag []string

func (f *trimFlag) String() string     { return strings.Join(*f, ",") }
func (f *trimFlag) Set(v string) error { *f = append(*f, v); return nil }

// printTrimWarnings prints the warnings for the trim report beside a built
// program (docs/GO_PACKAGES.md#trimming): nothing without trimming.
func printTrimWarnings(output string) error {
	lines, err := compiler.TrimWarnings(output)
	for _, line := range lines {
		fmt.Fprintln(os.Stderr, "gosvm: warning:", line)
	}
	return err
}

// printTrimSummary prints a trim report's summary as warnings, for commands
// that write no report file.
func printTrimSummary(report *compiler.TrimReport, prefix string) {
	for _, line := range report.Summary() {
		fmt.Fprintln(os.Stderr, "gosvm: warning:", prefix+line)
	}
}
