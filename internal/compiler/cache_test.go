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
	original, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(p, "t00l")
	if err = os.Chtimes(p, original.ModTime(), original.ModTime()); err != nil {
		t.Fatal(err)
	}
	contentChanged, err := buildKey(source, llvm, "v3")
	if err != nil || contentChanged == key {
		t.Fatal("backend bytes replaced with identical size/mtime were not detected", err)
	}
	mustWrite(p, "tool")
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

func TestDependencyNamespaceSeparatesImportedContent(t *testing.T) {
	t.Setenv("GOWORK", "off")
	t.Setenv("GOPROXY", "off")
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "model"), 0755)
	write := func(name, source string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/app\n\ngo 1.22\n")
	write("program.go", "package app; import \"example.com/app/model\"; func Value() uint64 {return model.Value}")
	write("model/model.go", "package model; const Value uint64=1")
	key, err := DependencyKey(dir)
	if err != nil {
		t.Fatal(err)
	}
	write("program.go", "package app; import \"example.com/app/model\"; func Value() uint64 {return model.Value+2}")
	edited, err := DependencyKey(dir)
	if err != nil || edited != key {
		t.Fatal("application edit changed dependency namespace", err)
	}
	write("model/model.go", "package model; const Value uint64=2")
	changed, err := DependencyKey(dir)
	if err != nil || changed == key {
		t.Fatal("imported dependency did not separate outputs", err)
	}
}
