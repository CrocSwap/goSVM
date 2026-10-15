package compiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gosvm/internal/targets"
)

func TestFrameDiagnosticsParse(t *testing.T) {
	names := map[string]string{"v12": "program.big"}
	for _, line := range []string{
		"Error: Function v12 overflows the maximum allowed frame space by accessing an offset 960 bytes greater than the maximum of 4096. Please, minimize large stack variables.",
		"Error: Function v12 Stack offset of 5000 exceeded max offset of 4096 by 904 bytes, please minimize large stack variables.",
		"Error: A function call in method v12 overwrites values in the frame. Please, decrease stack usage or remove parameters from the call.",
	} {
		err := frameDiagnostics([]byte("warning: other\n"+line+"\n\n"), names)
		if err == nil || !strings.Contains(err.Error(), "program.big (C v12)") || !strings.Contains(err.Error(), "4096 bytes") {
			t.Fatalf("%q: %v", line, err)
		}
	}
	if err := frameDiagnostics([]byte("warning: unused\n"), names); err != nil {
		t.Fatal(err)
	}
}

// A function whose frame exceeds 4 KiB fails the fogo-v0 build (fixed
// frames) naming the Go function; Clang alone exits successfully.
func TestFrameOverflowFailsBuild(t *testing.T) {
	llvm := modernLLVM()
	if llvm == "" {
		t.Skip("pinned platform-tools v1.57 backend required")
	}
	src := `package main

import "gosvm/solana"

func big(in []byte, out []byte) {
	var a, b, c, d, e [1000]byte
	for j := 0; j < len(in); j++ {
		k := int(in[j]) % 1000
		a[k], b[k], c[k], d[k], e[k] = in[j], in[j]+1, in[j]+2, in[j]+3, in[j]+4
	}
	for j := 0; j < len(out); j++ {
		k := int(out[j]) % 1000
		out[j] = a[k] ^ b[k] ^ c[k] ^ d[k] ^ e[k]
	}
}

func Process(c solana.Context) uint64 {
	big(solana.Instruction(c), solana.Data(c, 0))
	return 0
}
`
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "program.go"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	err := Build(dir, filepath.Join(dir, "program.so"), llvm, target.Fogo)
	if err == nil || !strings.Contains(err.Error(), "SBF stack frame exceeds 4096 bytes") {
		t.Fatalf("fogo-v0 build: %v", err)
	}
	if !strings.Contains(err.Error(), "main.big") && !strings.Contains(err.Error(), "Process") {
		t.Fatalf("diagnostic names no Go function: %v", err)
	}
	t.Log(err)
}
