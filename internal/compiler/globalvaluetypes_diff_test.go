package compiler

import (
	"encoding/hex"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Package-level variables combined with value types (struct and array ==,
// type aliases, arrays of composite elements and panic) against native Go:
// examples/global-value-types runs natively in a fresh process per vector
// and as host-compiled C; output bytes, results, panics and each panic's log
// line must match. Panics during initialization abort every invocation, so
// globalInitPanicProbes cover them as separate programs, against native
// processes whose initialization panics. See docs/GO_GLOBALS.md and
// docs/GO_TYPES.md.

const globalValueTypesEntry = "gosvm/examples/global-value-types"

// globalValueTypesProgram reads the example's two packages; replace, when
// set, rewrites the entry package's program.go.
func globalValueTypesProgram(t *testing.T, replace func(string) string) *Program {
	t.Helper()
	p := &Program{Entry: globalValueTypesEntry}
	for _, sub := range []string{"", "wide"} {
		dir := filepath.Join("../../examples/global-value-types", sub)
		path := globalValueTypesEntry
		if sub != "" {
			path += "/" + sub
		}
		unit := PackageSources{Path: path}
		for _, name := range []string{"program.go", "wide.go"} {
			data, err := os.ReadFile(filepath.Join(dir, name))
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				t.Fatal(err)
			}
			if replace != nil && name == "program.go" {
				data = []byte(replace(string(data)))
			}
			unit.Sources = append(unit.Sources, Source{Name: filepath.Join(dir, name), Data: data})
		}
		p.Packages = append(p.Packages, unit)
	}
	return p
}

// goBinary builds a native command from dir (relative to the repository
// root, or absolute with its own go.mod when module is set).
func goBinary(t *testing.T, dir, pkg string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "oracle")
	args := []string{"build", "-o", bin}
	if runtime.GOOS == "darwin" {
		args = append(args, "-ldflags=-linkmode=external")
	}
	cmd := exec.Command("go", append(args, pkg)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v\n%s", pkg, err, out)
	}
	if runtime.GOOS == "darwin" {
		if out, err := exec.Command("codesign", "--force", "--sign", "-", bin).CombinedOutput(); err != nil {
			t.Fatalf("codesign: %v\n%s", err, out)
		}
	}
	return bin
}

// globalValueTypesModes is the number of modes Run dispatches on.
const globalValueTypesModes = 6

// globalValueTypesVectors covers every mode with random inputs and inputs
// of small bytes (so comparisons often succeed); mode 2 sometimes indexes
// past a table and mode 5 calls the initializers' checked function with
// values above its limit.
func globalValueTypesVectors(seed int64, perMode int) [][]byte {
	rng := rand.New(rand.NewSource(seed))
	var vectors [][]byte
	for mode := byte(0); mode < globalValueTypesModes; mode++ {
		n := perMode
		if mode == 0 {
			n = 1 // the initial state does not depend on the input
		}
		for j := 0; j < n; j++ {
			in := make([]byte, 128)
			rng.Read(in)
			in[0] = mode
			if j%3 == 1 {
				for k := 1; k < 128; k++ {
					in[k] = byte(rng.Intn(4))
				}
			}
			switch mode {
			case 2:
				if rng.Intn(4) != 0 {
					in[2] = byte(rng.Intn(200))
				}
			case 5:
				if rng.Intn(3) != 0 {
					in[1] = byte(rng.Intn(65))
				}
			}
			vectors = append(vectors, in)
		}
	}
	return vectors
}

// GlobalValueTypesVectorsForTest is the vector set the SBF test runs.
func GlobalValueTypesVectorsForTest() [][]byte { return globalValueTypesVectors(20261031, 30) }

// compareLines runs the vectors on the host binary (panicking ones alone,
// since the driver exits) and compares each line with want.
func compareLines(t *testing.T, bin string, vectors [][]byte, want []string) {
	t.Helper()
	var got, batch strings.Builder
	flush := func() {
		if batch.Len() > 0 {
			got.WriteString(runHost(t, bin, batch.String()))
			batch.Reset()
		}
	}
	for j, in := range vectors {
		line := fmt.Sprintf("%x\n", in)
		if strings.Contains(want[j], "P:") {
			flush()
			got.WriteString(runHost(t, bin, line))
			continue
		}
		batch.WriteString(line)
	}
	flush()
	gotLines := strings.SplitAfter(got.String(), "\n")
	for j := range want {
		if j >= len(gotLines) || gotLines[j] != want[j] {
			g := ""
			if j < len(gotLines) {
				g = gotLines[j]
			}
			t.Fatalf("vector %d (%x):\nC  %.400q\nGo %.400q", j, vectors[j], g, want[j])
		}
	}
}

func TestGlobalValueTypesDifferential(t *testing.T) {
	oracle := goBinary(t, "../..", "./examples/global-value-types/oracle")
	c, err := CompileProgram(globalValueTypesProgram(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	bin := hostCompile(t, c, DefaultHeapBytes, valueTypesDriver, false)
	vectors := append(globalValueTypesVectors(20261014, 250), GlobalValueTypesVectorsForTest()...)
	want := nativeGlobals(t, oracle, vectors)
	compareLines(t, bin, vectors, want)
	panics, logged := map[byte]int{}, map[byte]int{}
	for j, w := range want {
		if strings.Contains(w, "P:") {
			panics[vectors[j][0]]++
		}
		if strings.HasPrefix(w, "L:") {
			logged[vectors[j][0]]++
		}
	}
	for mode := byte(0); mode < globalValueTypesModes; mode++ {
		switch mode {
		case 2, 5:
			if panics[mode] == 0 {
				t.Errorf("mode %d: no vector panics", mode)
			}
		default:
			if panics[mode] != 0 {
				t.Errorf("mode %d: %d vectors panic", mode, panics[mode])
			}
		}
	}
	if logged[5] == 0 || logged[2] != 0 {
		t.Errorf("panic log lines per mode: %v", logged)
	}
	t.Logf("%d vectors (panics %v, of which logged %v) match native Go, each from fresh initialization", len(vectors), panics, logged)
}

// The example's tables are placed as documented: never-written tables with
// constant initializers (including the two above the per-value limit) are
// read-only data; written ones are in the globals block.
func TestGlobalValueTypesPlacement(t *testing.T) {
	checked, err := Check(globalValueTypesProgram(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	placed := map[string]bool{}
	for _, g := range checked.Globals {
		placed[g.Var.Pkg().Name()+"."+g.Var.Name()] = g.ReadOnly
	}
	for name, readOnly := range map[string]bool{
		"globalvaluetypes.pows": true, "globalvaluetypes.big": true, "globalvaluetypes.pairs": true,
		"globalvaluetypes.origin": true, "globalvaluetypes.cube": true, "globalvaluetypes.cells": true,
		"globalvaluetypes.zeroRec": true, "globalvaluetypes.identity": true, "wide.Origin": true,
		"globalvaluetypes.ledger": false, "globalvaluetypes.grid": false, "globalvaluetypes.recs": false,
		"globalvaluetypes.rows": false, "globalvaluetypes.table": false, "wide.Table": false, "wide.Grid": false,
	} {
		if got, ok := placed[name]; !ok || got != readOnly {
			t.Errorf("%s: read-only %v (declared %v), want %v", name, got, ok, readOnly)
		}
	}
	if checked.GlobalBytes < 160*32 {
		t.Errorf("globals block of %d bytes cannot hold ledger", checked.GlobalBytes)
	}
}

// globalInitPanicProbes panic (or trap) while package-level variables are
// initialized, so Process never runs. src is the entry package's body after
// its package clause and imports; lib, when set, is package probe/lib; line
// is the log line ("" for a runtime error, which logs nothing).
var globalInitPanicProbes = []struct {
	name, src, lib, line string
}{
	{name: "var initializer calls a panicking function", src: `
var x = boom(3)
func boom(n uint64) uint64 { if n > 2 { panic("init var boom") }; return n }`, line: "panic: init var boom"},
	{name: "init function panics with a named value", src: `
type Code uint8
func init() { panic(Code(7)) }`, line: "panic: main.Code(7)"},
	{name: "second init after composite-array writes", src: `
type P struct{ X, Y int64 }
var a int64 = 5
var t [4]P
func init() { t[1].X = a; t[2].Y-- }
func init() { if t[1].X == 5 && t[2] == (P{Y: -1}) { panic(t[2].Y - 2) } }`, line: "panic: -3"},
	{name: "struct equality on globals in init", src: `
type P struct{ X, Y uint64 }
var o = P{1, 2}
var q P
func init() { q = P{1, 2}; panic(q == o) }`, line: "panic: true"},
	{name: "alias-typed table checked by an initializer", src: `
type Int = [4]uint64
type Label string
var pows = [3]Int{{1}, {2}, {3}}
var first = check(pows[0])
func check(x Int) Int { if x != (Int{1}) { return x }; panic(Label("first")) }`, line: `panic: main.Label("first")`},
	{name: "imported package initializer", src: `
var Y = lib.T + 1`, lib: `package lib
type Limit uint16
var Table = [2][2]uint16{{1, 2}, {3, 99}}
var T = Check(uint64(Table[1][1]))
func Check(x uint64) uint64 { if x > 64 { panic(Limit(x)) }; return x }`, line: "panic: lib.Limit(99)"},
	{name: "panic(nil) in init", src: `
func init() { panic(nil) }`, line: NilPanicLine},
	{name: "runtime error in an initializer", src: `
var tab = [4]uint64{1, 2, 3, 4}
var i = idx()
func idx() int { return 5 }
var v = tab[i]`, line: ""},
}

// initPanicSource is a probe's entry package: Process writes the output's
// first byte, which must never happen. sbf selects a Context entrypoint;
// native adds main.
func initPanicSource(src, lib string, sbf, native bool) string {
	var b strings.Builder
	b.WriteString("package main\n\n")
	if lib != "" {
		b.WriteString("import \"probe/lib\"\n")
	}
	if sbf {
		b.WriteString("import \"gosvm/solana\"\n")
	}
	b.WriteString(src + "\n")
	if sbf {
		b.WriteString("func Process(c solana.Context) uint64 { solana.Data(c, 0)[0] = 1; return 0 }\n")
	} else {
		b.WriteString("func Process(s, i []byte) uint64 { s[0] = 1; return 0 }\n")
	}
	if native {
		b.WriteString("func main() { Process(make([]byte, 1), nil) }\n")
	}
	return b.String()
}

func initPanicProgram(src, lib string, sbf bool) *Program {
	p := &Program{Entry: "probe", Packages: []PackageSources{{Path: "probe", Sources: []Source{{Name: "probe.go", Data: []byte(initPanicSource(src, lib, sbf, false))}}}}}
	if lib != "" {
		p.Packages = append(p.Packages, PackageSources{Path: "probe/lib", Sources: []Source{{Name: "lib.go", Data: []byte(lib)}}})
	}
	return p
}

// TestGlobalInitPanics runs each probe natively (its initialization panics
// before main: exit status 2 and the panic's first stderr line) and as
// host-compiled C: the C must log the same line (none for a runtime error)
// and abort with Process's output untouched.
func TestGlobalInitPanics(t *testing.T) {
	for _, probe := range globalInitPanicProbes {
		t.Run(probe.name, func(t *testing.T) {
			dir := t.TempDir()
			files := map[string]string{"go.mod": "module probe\n\ngo 1.22\n", "main.go": initPanicSource(probe.src, probe.lib, false, true)}
			if probe.lib != "" {
				files["lib/lib.go"] = probe.lib
			}
			for name, data := range files {
				if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0644); err != nil {
					t.Fatal(err)
				}
			}
			native := exec.Command(goBinary(t, dir, "."))
			var stderr strings.Builder
			native.Stderr = &stderr
			err := native.Run()
			if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 2 {
				t.Fatalf("native run: %v\n%s", err, stderr.String())
			}
			first, _, _ := strings.Cut(stderr.String(), "\n")
			if probe.line == "" {
				if !strings.HasPrefix(first, "panic: runtime error: ") {
					t.Fatalf("native: %q", first)
				}
			} else if !strings.HasPrefix(first, probe.line) {
				// Go appends " [recovered]" or goexperiment notes after some
				// values; the contracted line is the prefix.
				t.Fatalf("native line %q, want %q", first, probe.line)
			}
			c, err := CompileProgram(initPanicProgram(probe.src, probe.lib, false))
			if err != nil {
				t.Fatal(err)
			}
			bin := hostCompile(t, c, DefaultHeapBytes, valueTypesDriver, false)
			got := runHost(t, bin, strings.Repeat("00", 128)+"\n")
			want := "P:" + strings.Repeat("00", 1024) + "\n"
			if probe.line != "" {
				want = "L:" + hex.EncodeToString([]byte(probe.line)) + " " + want
			}
			if got != want {
				t.Fatalf("C %.200q, want %.200q", got, want)
			}
		})
	}
}
