package main

import (
	"flag"
	"fmt"
	"gosvm/internal/compiler"
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
		case "toolchain", "new", "generate", "check", "build", "test", "doctor", "help", "version":
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
	noCache := flag.Bool("no-cache", false, "rebuild even if source, compiler, backend and output match the last build")
	flag.Parse()
	if *arch != "v0" && *arch != "v3" {
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
		var src []compiler.Source
		src, err = compiler.ReadSources(flag.Arg(0))
		if err == nil {
			out, err = compiler.CompileSources(src)
		}
		if err == nil {
			err = os.MkdirAll(filepath.Dir(*output), 0755)
		}
		if err == nil {
			if *arch == "v3" {
				out = append([]byte("#define GOSVM_SBF_V3 1\n"), out...)
			}
			err = os.WriteFile(*output, append(out, []byte(compiler.ABI)...), 0644)
		}
	} else {
		if *noCache {
			err = compiler.Build(flag.Arg(0), *output, *llvm, *arch)
		} else {
			err = compiler.BuildCached(flag.Arg(0), *output, *llvm, *arch)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
