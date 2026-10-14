package compiler

import (
	"bytes"
	"gosvm/internal/targets"
	"gosvm/internal/toolchain"
	"os"
	"path/filepath"
	"testing"
)

// modernLLVM returns the content-pinned platform-tools v1.57 backend, or "".
func modernLLVM() string {
	candidates := []string{filepath.Join(toolchain.ModernRoot(), "llvm")}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".cache/solana", target.ModernTools, "platform-tools/llvm"))
	}
	p, _ := target.Resolve(target.Fogo)
	for _, llvm := range candidates {
		if p.CheckBackend(llvm) == nil {
			return llvm
		}
	}
	return ""
}

// Uncached builds of the same source from different absolute paths must be
// byte-identical (no embedded build paths or temporary names).
func TestModernRebuildIsByteIdentical(t *testing.T) {
	llvm := modernLLVM()
	if llvm == "" {
		t.Skip("pinned platform-tools v1.57 backend required")
	}
	source, err := os.ReadFile(filepath.Join("..", "..", "examples", "clock2", "program.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, profile := range []string{target.Fogo, target.Solana} {
		var outputs [][]byte
		for _, sub := range []string{"a", filepath.Join("longer", "rebuild", "path")} {
			dir := filepath.Join(t.TempDir(), sub)
			if err = os.MkdirAll(dir, 0755); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(dir, "program.go"), source, 0644); err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(dir, "program.so")
			if err = Build(dir, output, llvm, profile); err != nil {
				t.Fatal(profile, err)
			}
			data, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(data, []byte(dir)) {
				t.Fatalf("%s ELF embeds its build path %s", profile, dir)
			}
			outputs = append(outputs, data)
		}
		if !bytes.Equal(outputs[0], outputs[1]) {
			t.Fatalf("%s rebuilds from different paths differ", profile)
		}
	}
}
