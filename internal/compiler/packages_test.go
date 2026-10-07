package compiler

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func unit(path, source string) PackageSources {
	return PackageSources{path, []Source{{path + "/program.go", []byte(source)}}}
}
func TestImportedValuesAndCalls(t *testing.T) {
	p := &Program{Entry: "app", Packages: []PackageSources{
		unit("app", `package app
import first "shared/a"
import second "shared/b"
func Process(s,i []byte)uint64{
 k:=first.Key{0:9,31:4}; b:=first.Box{Key:k,N:first.Amount(3)}
 b.Key[1]=7;copy:=first.Echo(b);b.Key[1]=8
 second.Write(s,byte(copy.N))
 return first.Process(copy)+second.Process()+uint64(first.Bias)
}`),
		unit("shared/b", `package same;func Process()uint64{return 20};func Write(s []byte,n byte){s[0]=n}`),
		unit("shared/a", `package same
import "shared/types"
type Amount uint64;type Key [32]byte;type Box struct{Key Key;N Amount}
const Bias Amount=5
func Echo(b Box)Box{return b}
func Process(b Box)uint64{return uint64(b.Key[0])+uint64(b.Key[1])+uint64(b.Key[31])+uint64(b.N)+types.Read(types.Value{N:1})}`),
		unit("shared/types", `package types;type Value struct{N uint64};func Read(v Value)uint64{return v.N}`),
	}}
	c, err := CompileProgram(p)
	if err != nil {
		t.Fatal(err)
	}
	reversed := *p
	reversed.Packages = append([]PackageSources(nil), p.Packages...)
	for a, b := 0, len(reversed.Packages)-1; a < b; a, b = a+1, b-1 {
		reversed.Packages[a], reversed.Packages[b] = reversed.Packages[b], reversed.Packages[a]
	}
	other, err := CompileProgram(&reversed)
	if err != nil || !bytes.Equal(c, other) {
		t.Fatalf("graph order changes output: %v", err)
	}
	bin := hostGenerated(t, c, `int main(void){u8 s[1]={0};u64 n=go_Process((slice){s,1},(slice){0,0});return n!=49||s[0]!=3;}`)
	if out, err := exec.Command(bin).CombinedOutput(); err != nil {
		t.Fatalf("qualified values/calls: %v %s", err, out)
	}
}
func TestImportedSDKAndEntryIdentity(t *testing.T) {
	p := &Program{Entry: "app", Packages: []PackageSources{
		unit("app", `package app;import "gosvm/solana";import "app/helper";func Process(c solana.Context)uint64{return helper.Process(c)}`),
		unit("app/helper", `package helper;import chain "gosvm/solana";func Process(c chain.Context)uint64{return chain.Count(c)}`),
	}}
	c, err := CompileProgram(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(c), "static u64 go_Process(") != 2 || !bytes.Contains(c, []byte("GOSVM_CONTEXT_ENTRY")) {
		t.Fatal("entry function confused with imported Process")
	}
	bin := hostGenerated(t, c, `int main(void){Context c={0};return go_Process(&c)!=0;}`)
	if out, err := exec.Command(bin).CombinedOutput(); err != nil {
		t.Fatalf("shared SDK identity: %v %s", err, out)
	}
}
func TestImportedPackageRejections(t *testing.T) {
	for name, source := range map[string]string{
		"globals":                        `var N uint64=1;func F()uint64{return N}`,
		"init":                           `func init(){};func F()uint64{return 1}`,
		"method value":                   `type S struct{};func(s S)F()uint64{return 1};func F()uint64{s:=S{};f:=s.F;return f()}`,
		"unsupported unreachable helper": `func unused(){defer unused()};func F()uint64{return 1}`,
		"cross-package recursion":        `import "app";func F()uint64{return app.Process(nil,nil)}`,
		"direct recursion":               `func F()uint64{return F()}`,
		"function values":                `func F()uint64{f:=G;return f()};func G()uint64{return 1}`,
	} {
		t.Run(name, func(t *testing.T) {
			p := &Program{Entry: "app", Packages: []PackageSources{unit("app", `package app;import "dep";func Process(s,i []byte)uint64{return dep.F()}`), unit("dep", "package dep;"+source)}}
			_, err := CompileProgram(p)
			if err == nil || !strings.Contains(err.Error(), ".go:") {
				t.Fatalf("source-located rejection wanted: %v", err)
			}
		})
	}
	p := &Program{Entry: "app", Packages: []PackageSources{
		unit("app", `package app;import "dep";type K [32]byte;func Process(s,i []byte)uint64{v:=K{};return dep.F(v)}`),
		unit("dep", `package dep;type K [32]byte;func F(k K)uint64{return 1}`),
	}}
	if _, err := CompileProgram(p); err == nil {
		t.Fatal("distinct named types silently merged")
	}
}

func writePackageFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}
func TestModuleImportsAndCache(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) { writePackageFile(t, dir, name, content) }
	write("go.mod", "module example.com/app\n\ngo 1.22\nrequire example.com/lib v0.0.0\nreplace example.com/lib => ./lib\n")
	write("program/entry.go", `package p;import "example.com/lib/model";func Process(s,i []byte)uint64{return model.Read(model.V{N:7})}`)
	write("lib/go.mod", "module example.com/lib\n\ngo 1.22\n")
	write("lib/model/model.go", `package model;import "example.com/lib/internal/math";type V struct{N uint64};func Read(v V)uint64{return math.Identity(v.N)}`)
	write("lib/internal/math/math.go", `package math;func Identity(n uint64)uint64{return n}`)
	write("lib/model/model_test.go", `package model;func unsupportedTestOnly(){defer unsupportedTestOnly()}`)
	write("host/main.go", `package main;import "fmt"
 "math/rand";func main(){fmt.Println("not in graph")}`)
	write("llvm/bin/clang", "fake tool")
	write("llvm/bin/ld.lld", "fake tool")
	src, llvm := filepath.Join(dir, "program"), filepath.Join(dir, "llvm")
	key := func() string {
		t.Helper()
		k, err := buildKey(src, llvm, "v3")
		if err != nil {
			t.Fatal(err)
		}
		return k
	}
	load := func() *Program {
		t.Helper()
		p, err := ReadProgram(src)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = CompileProgram(p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	p := load()
	if p.Entry != "example.com/app/program" || len(p.Packages) != 3 || len(p.Metadata) != 2 {
		t.Fatalf("loaded wrong graph: %+v", p)
	}
	first := key()
	snapshot, err := CompileProgram(p)
	if err != nil {
		t.Fatal(err)
	}
	write("lib/model/model_test.go", `package model;func unsupportedTestOnly(){go unsupportedTestOnly()}`)
	write("host/main.go", "host-only edit")
	if key() != first {
		t.Fatal("host-only changes invalidate SBF cache")
	}
	write("lib/internal/math/math.go", `package math;func Identity(n uint64)uint64{return n+1}`)
	changed := key()
	stillSnapshot, err := CompileProgram(p)
	if err != nil || !bytes.Equal(snapshot, stillSnapshot) {
		t.Fatalf("loaded build inputs changed underneath cache key: %v", err)
	}
	fresh, err := CompileProgram(load())
	if err != nil || bytes.Equal(snapshot, fresh) {
		t.Fatalf("fresh import graph missed helper edit: %v", err)
	}
	if first == changed {
		t.Fatal("transitive helper edit missed")
	}
	write("lib/model/extra.go", `package model;const Extra uint64=3`)
	added := key()
	if added == changed {
		t.Fatal("added dependency file missed")
	}
	if err := os.Remove(filepath.Join(dir, "lib/model/extra.go")); err != nil {
		t.Fatal(err)
	}
	if key() != changed {
		t.Fatal("removed dependency file missed")
	}
	write("go.mod", "module example.com/app\n\ngo 1.22\nrequire example.com/lib v0.0.0\nreplace example.com/lib => ./lib\n// metadata changed\n")
	if key() == changed {
		t.Fatal("go.mod edit missed")
	}
	beforeSum := key()
	write("go.sum", "example.com/unused v0.0.0 h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\n")
	if key() == beforeSum {
		t.Fatal("go.sum edit missed")
	}
	checksum, err := os.ReadFile(filepath.Join(dir, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	// Do not let ambient workspace/tags/toolchain/network settings change loading.
	t.Setenv("GOWORK", filepath.Join(dir, "missing.work"))
	t.Setenv("GOFLAGS", "-tags=nonsense -mod=vendor")
	t.Setenv("GOPROXY", "https://invalid.example")
	load()
	unchanged, err := os.ReadFile(filepath.Join(dir, "go.sum"))
	if err != nil || !bytes.Equal(checksum, unchanged) {
		t.Fatal("resolution mutated project checksums", err)
	}
	write("lib/internal/math/math_linux.go", "package math")
	if _, err := ReadProgram(src); err == nil || !strings.Contains(err.Error(), "platform-specific") {
		t.Fatalf("dependency host selection accepted: %v", err)
	}
}
func TestModuleImportFailures(t *testing.T) {
	for name, dep := range map[string][3]string{
		"cycle":            {`import "example.com/app/program";func F()uint64{return program.Process(nil,nil)}`, "example.com/app/lib", "import cycle"},
		"standard library": {`func F()uint64{return 0}`, "fmt", "standard library"},
		"missing":          {`func F()uint64{return 0}`, "example.com/missing/model", "disabled"},
		"blank":            {`func F()uint64{return 0}`, "_ example.com/app/lib", "blank imports"},
		"internal":         {`func F()uint64{return 0}`, "example.com/other/internal/lib", "internal package"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writePackageFile(t, dir, "go.mod", "module example.com/app\n\ngo 1.22\n")
			importText := fmt.Sprintf("%q", dep[1])
			if strings.HasPrefix(dep[1], "_ ") {
				importText = "_ " + fmt.Sprintf("%q", strings.TrimPrefix(dep[1], "_ "))
			}
			writePackageFile(t, dir, "program/entry.go", "package program;import "+importText+`;func Process(s,i []byte)uint64{return lib.F()}`)
			writePackageFile(t, dir, "lib/lib.go", "package lib;"+dep[0])
			_, err := ReadProgram(filepath.Join(dir, "program"))
			if err == nil || !strings.Contains(err.Error(), dep[2]) || !strings.Contains(err.Error(), ".go:") {
				t.Fatalf("wanted %q at source: %v", dep[2], err)
			}
		})
	}
}

func TestSharedExampleDifferential(t *testing.T) {
	root, err := filepath.Abs("../../examples/shared-packages")
	if err != nil {
		t.Fatal(err)
	}
	p, err := ReadProgram(filepath.Join(root, "program"))
	if err != nil {
		t.Fatal(err)
	}
	c, err := CompileProgram(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Packages) != 5 {
		t.Fatalf("wrong graph: %d", len(p.Packages))
	}
	host := hostGenerated(t, c, arrayHostDriver)
	template, err := os.ReadFile("../../scripts/array_native_driver.go.txt")
	if err != nil {
		t.Fatal(err)
	}
	nativeDir := t.TempDir()
	writePackageFile(t, nativeDir, "go.mod", fmt.Sprintf("module nativecheck\n\ngo 1.22\nrequire example.com/shared-app v0.0.0\nreplace example.com/shared-app => %q\n", root))
	writePackageFile(t, nativeDir, "main.go", strings.Replace(string(template), "gosvm/examples/array-values", "example.com/shared-app/program", 1))
	type row struct{ Name, State, Instruction string }
	var rows []row
	var input strings.Builder
	add := func(x, y, sequence, amount, minimum uint64) {
		s, i := make([]byte, 24), make([]byte, 16)
		binary.LittleEndian.PutUint64(s, x)
		binary.LittleEndian.PutUint64(s[8:], y)
		binary.LittleEndian.PutUint64(s[16:], sequence)
		binary.LittleEndian.PutUint64(i, amount)
		binary.LittleEndian.PutUint64(i[8:], minimum)
		rows = append(rows, row{fmt.Sprint(len(rows)), fmt.Sprintf("%x", s), fmt.Sprintf("%x", i)})
		fmt.Fprintf(&input, "%x %x\n", i, s)
	}
	rng := rand.New(rand.NewSource(20261005))
	for j := 0; j < 1000; j++ {
		x, y := uint64(1+rng.Int63n(500000000)), uint64(1+rng.Int63n(1000000000))
		amount := uint64(1 + rng.Int63n(int64(1000000000-x)))
		minimum := uint64(0)
		if j%2 != 0 {
			minimum = uint64(rng.Int63n(int64(y)))
		}
		add(x, y, rng.Uint64()>>1, amount, minimum)
	}
	for _, v := range [][5]uint64{{0, 1, 0, 1, 0}, {1, 0, 0, 1, 0}, {1, 1, 0, 0, 0}, {1000000000, 1, 0, 1, 0}, {1, 1000000001, 0, 1, 0}, {999999999, 1000000000, 0, 1, 0}, {1, 2, ^uint64(0), 1, 0}, {1, 2, ^uint64(0) - 1, 1, 0}, {1, 2, 0, 1, ^uint64(0)}, {1, 2, 0, ^uint64(0), 0}, {^uint64(0), 1, 0, 1, 0}} {
		add(v[0], v[1], v[2], v[3], v[4])
	}
	encoded, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	native := exec.CommandContext(ctx, "go", "run", "-ldflags=-linkmode=external", ".")
	native.Dir = nativeDir
	native.Env = append(os.Environ(), "GOWORK=off", "GOPROXY=off", "GOSUMDB=off", "GOFLAGS=", "GOTOOLCHAIN=local")
	native.Stdin = bytes.NewReader(encoded)
	out, err := native.Output()
	if err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			t.Fatalf("native oracle: %v %s", err, e.Stderr)
		}
		t.Fatal(err)
	}
	var results []struct {
		Code     uint64
		Data     string
		Panicked bool
	}
	if err = json.Unmarshal(out, &results); err != nil {
		t.Fatal(err)
	}
	if len(results) != len(rows) {
		t.Fatal("native vector count")
	}
	var want strings.Builder
	success := 0
	for _, r := range results {
		if r.Panicked {
			t.Fatal("native fixture panicked")
		}
		if r.Code == 0 {
			success++
		}
		fmt.Fprintf(&want, "%d:%s\n", r.Code, r.Data)
	}
	if success < 500 || success == len(results) {
		t.Fatalf("insufficient success/error coverage: %d", success)
	}
	cmd := exec.Command(host)
	cmd.Stdin = strings.NewReader(input.String())
	got, err := cmd.CombinedOutput()
	if err != nil || string(got) != want.String() {
		t.Fatalf("shared native-Go/C mismatch: %v %s", err, got)
	}
}

func TestImportedMainPackage(t *testing.T) {
	p := &Program{Entry: "app", Packages: []PackageSources{
		unit("app", `package app;import "dep";func Process(s,i []byte)uint64{return dep.F()}`),
		unit("dep", `package main;func F()uint64{return 1}`),
	}}
	if _, err := CompileProgram(p); err == nil || !strings.Contains(err.Error(), "not importable") {
		t.Fatalf("main imported: %v", err)
	}
}
func TestResolverFlagLikeImport(t *testing.T) {
	dir := t.TempDir()
	writePackageFile(t, dir, "go.mod", "module example.com/app\n\ngo 1.22\n")
	writePackageFile(t, dir, "program.go", `package p;import "-export";func Process(s,i []byte)uint64{return 0}`)
	if _, err := ReadProgram(dir); err == nil || !strings.Contains(err.Error(), "-export") {
		t.Fatalf("invalid path interpreted as a flag: %v", err)
	}
}
