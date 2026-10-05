package compiler

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMultiFileValueSemantics(t *testing.T) {
	sources := []Source{
		{"types.go", []byte(`package p
 type Small uint8
 type Inner struct { N uint64; B Small }
 type Outer struct { Child Inner; Tail uint64 }
 func mutate(x Outer) Outer {x.Child.N=99; x.Child.B=x.Child.B+1;return x}
 func next(s []byte) uint64 {s[0]=s[0]+1;return uint64(s[0])}
 func literal(s []byte) Outer {return Outer{Tail:next(s),Child:Inner{N:next(s)}}}
 `)},
		{"entry.go", []byte(`package p
 func Process(s,i []byte) uint64 {
  original:=Outer{Child:Inner{N:7,B:255},Tail:3}
  copy:=mutate(original)
  if original.Child.N!=7||original.Child.B!=255||copy.Child.N!=99||copy.Child.B!=0{return 1}
  z:=literal(s)
  if z.Tail!=1||z.Child.N!=2||s[0]!=2{return 2}
  var zero Outer
  if zero.Child.N!=0||zero.Child.B!=0||zero.Tail!=0{return 3}
  saved:=copy
  copy.Child.N=42
  if saved.Child.N!=99{return 4}
  return 0
 }`)},
	}
	bin := hostSources(t, sources, `int main(void){u8 s[1]={0};return go_Process((slice){s,1},(slice){s,1});}`)
	if out, e := exec.Command(bin).CombinedOutput(); e != nil {
		t.Fatalf("struct semantics: %v %s", e, out)
	}
	a, e := CompileSources(sources)
	if e != nil {
		t.Fatal(e)
	}
	b, e := CompileSources([]Source{sources[1], sources[0]})
	if e != nil || !bytes.Equal(a, b) {
		t.Fatal("output depends on source enumeration order")
	}
}

func TestValueTypeRejections(t *testing.T) {
	for name, code := range map[string]string{
		"hidden context": `import "gosvm/solana";type Box struct { C solana.Context };func Process(c solana.Context)uint64{return 0}`,
		"slice field":    `type Box struct { B []byte };func Process(s,i []byte)uint64{return 0}`,
		"signed named":   `type Bad int;func Process(s,i []byte)uint64{var b Bad;return uint64(b)}`,
		"comparison":     `type V struct { N uint64 };func Process(s,i []byte)uint64{a:=V{};if a==a{return 0};return 1}`,
		"conversion":     `type V struct { N uint64 };type W V;func Process(s,i []byte)uint64{a:=V{};b:=W(a);return b.N}`,
		"alias":          `type V = uint64;func Process(s,i []byte)uint64{return 0}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, e := Compile("bad.go", []byte("package p;"+code)); e == nil {
				t.Fatal("accepted unsupported type")
			}
		})
	}
	_, e := CompileSources([]Source{{"a.go", []byte("package p;func Process(s,i []byte)uint64{return f()}")}, {"b.go", []byte("package p;func f()uint64{return Process(nil,nil)}")}})
	if e == nil || !strings.Contains(e.Error(), "recursion") {
		t.Fatalf("cross-file recursion: %v", e)
	}
}

func TestPackageSourcesAndCache(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	os.Mkdir(src, 0700)
	write := func(name, body string) {
		t.Helper()
		if e := os.WriteFile(filepath.Join(src, name), []byte(body), 0600); e != nil {
			t.Fatal(e)
		}
	}
	write("a.go", "package p;func Process(s,i []byte)uint64{return helper()}")
	write("b.go", "package p;func helper()uint64{return 1}")
	write("a_test.go", "ignored tests")
	llvm := filepath.Join(dir, "llvm")
	os.MkdirAll(filepath.Join(llvm, "bin"), 0700)
	for _, tool := range []string{"clang", "ld.lld"} {
		os.WriteFile(filepath.Join(llvm, "bin", tool), []byte("fake tool"), 0600)
	}
	key := func() string {
		t.Helper()
		v, e := buildKey(src, llvm, "v3")
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	first := key()
	write("a_test.go", "changed host tests")
	if key() != first {
		t.Fatal("host tests invalidate SBF cache")
	}
	write("b.go", "package p;func helper()uint64{return 2}")
	second := key()
	if first == second {
		t.Fatal("helper edit missed")
	}
	write("c.go", "package p;const Extra uint64=1")
	third := key()
	if third == second {
		t.Fatal("added file missed")
	}
	os.Remove(filepath.Join(src, "c.go"))
	if key() != second {
		t.Fatal("removed file missed")
	}
	write("host_linux.go", "package p")
	if _, e := ReadSources(src); e == nil {
		t.Fatal("platform-specific source accepted")
	}
	os.Remove(filepath.Join(src, "host_linux.go"))
	write("b.go", "//go:build custom\n\npackage p")
	if _, e := ReadSources(src); e == nil {
		t.Fatal("build-tag source accepted")
	}
}

func TestPlatformWordsInsideFilename(t *testing.T) {
	dir := t.TempDir()
	if e := os.WriteFile(filepath.Join(dir, "my_linux_helpers.go"), []byte("package p"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := ReadSources(dir); e != nil {
		t.Fatalf("platform word is not a Go filename constraint: %v", e)
	}
}
