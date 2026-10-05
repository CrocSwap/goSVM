package compiler

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ReadSources loads one file or every non-test Go file in a package directory.
// Build constraints are rejected rather than selecting host-dependent sources.
func ReadSources(path string) ([]Source, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	paths := []string{path}
	if st.IsDir() {
		paths = nil
		entries, err := os.ReadDir(path)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			n := e.Name()
			if !e.IsDir() && strings.HasSuffix(n, ".go") && !strings.HasSuffix(n, "_test.go") && !strings.HasPrefix(n, ".") && !strings.HasPrefix(n, "_") {
				stem := strings.TrimSuffix(n, ".go")
				if parts := strings.Split(stem, "_"); len(parts) > 1 {
					part := parts[len(parts)-1]
					switch part {
					case "aix", "android", "darwin", "dragonfly", "freebsd", "hurd", "illumos", "ios", "js", "linux", "nacl", "netbsd", "openbsd", "plan9", "solaris", "wasip1", "windows", "zos", "386", "amd64", "amd64p32", "arm", "arm64", "loong64", "mips", "mipsle", "mips64", "mips64le", "ppc64", "ppc64le", "riscv64", "s390x", "sparc64", "wasm":
						return nil, fmt.Errorf("%s: platform-specific filenames unsupported in on-chain packages", n)
					}
				}
				paths = append(paths, filepath.Join(path, n))
			}
		}
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("%s: no Go source files", path)
	}
	var sources []Source
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		if bytes.Contains(b, []byte("//go:build")) || bytes.Contains(b, []byte("// +build")) {
			return nil, fmt.Errorf("%s: build constraints unsupported in on-chain packages", p)
		}
		sources = append(sources, Source{p, b})
	}
	return sources, nil
}
