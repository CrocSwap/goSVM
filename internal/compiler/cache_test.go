package compiler

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBuildReceiptInvalidation(t *testing.T) {
	dir := t.TempDir()
	llvm := filepath.Join(dir, "llvm")
	mustWrite := func(p, s string) {
		t.Helper()
		if err := os.WriteFile(p, []byte(s), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(llvm, "bin"), 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"clang", "ld.lld"} {
		mustWrite(filepath.Join(llvm, "bin", name), "tool")
	}
	source, output := filepath.Join(dir, "input.go"), filepath.Join(dir, "program.so")
	mustWrite(source, "package p; const Value uint64 = 1")
	mustWrite(output, "ELF")
	key, err := buildKey(source, llvm, "v3")
	if err != nil {
		t.Fatal(err)
	}
	if cacheHit(output, key) {
		t.Fatal("hit without receipt")
	}
	if err := saveReceipt(output, key); err != nil {
		t.Fatal(err)
	}
	if !cacheHit(output, key) {
		t.Fatal("unchanged output missed")
	}
	mustWrite(output, "corrupted")
	if cacheHit(output, key) {
		t.Fatal("accepted corrupted output")
	}
	mustWrite(output, "ELF")
	mustWrite(source, "package p; const Value uint64 = 2")
	changed, err := buildKey(source, llvm, "v3")
	if err != nil {
		t.Fatal(err)
	}
	if changed == key || cacheHit(output, changed) {
		t.Fatal("accepted edited source")
	}
	mustWrite(source, "package p; const Value uint64 = 1")
	v0, err := buildKey(source, llvm, "v0")
	if err != nil || v0 == key {
		t.Fatal("target not in cache key", err)
	}
	p := filepath.Join(llvm, "bin/clang")
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(p, future, future); err != nil {
		t.Fatal(err)
	}
	changed, err = buildKey(source, llvm, "v3")
	if err != nil || changed == key {
		t.Fatal("backend modification not detected", err)
	}
	if err := os.Remove(output); err != nil {
		t.Fatal(err)
	}
	if cacheHit(output, key) {
		t.Fatal("accepted deleted output")
	}
}
