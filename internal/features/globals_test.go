package features

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Package-level variables and init functions have their own counters and
// the globals flag; Check reports the globals block size and the variables
// it placed in read-only data, and a writable block sets allocation.
func TestGlobalCounters(t *testing.T) {
	dir := t.TempDir()
	source := `package program

var table = [4]uint64{1, 2, 3, 4} // read-only
var name = "constant"              // read-only
var counter uint64                 // written: the block
var _ = mark()
var a, b = two()

func mark() uint64 { counter++; return counter }
func two() (uint64, uint64) { return 1, 2 }

func init() { counter = counter + 10 }
func init() {}

func Process(s, i []byte) uint64 { return table[i[0]&3] + uint64(len(name)) + counter + a + b }
`
	if err := os.WriteFile(filepath.Join(dir, "program.go"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	u := checkedReport(t, dir)
	h := u.Language.Handwritten
	for name, want := range map[string]int{"package_vars": 6, "init_functions": 2, "var_decls": 6} {
		if h.Constructs[name] != want {
			t.Errorf("%s = %d, want %d", name, h.Constructs[name], want)
		}
	}
	if !u.Uses.Checked || !u.Uses.Globals || !u.Uses.Allocation || u.Uses.GlobalHeapBytes != 24 || u.Uses.ReadOnlyGlobals != 2 {
		t.Errorf("uses: %+v", u.Uses)
	}
}

func TestGlobalUses(t *testing.T) {
	for name, tc := range map[string]struct {
		src  string
		want Uses
	}{
		// Read-only tables need no storage and no allocator.
		"read-only table": {`var pows = [...]uint64{1, 10, 100}
func Process(s, i []byte) uint64 { return pows[i[0]%3] }`,
			Uses{Checked: true, Globals: true, ReadOnlyGlobals: 1}},
		"init only": {`func init() {}
func Process(s, i []byte) uint64 { return 0 }`,
			Uses{Checked: true, Globals: true}},
		"writable": {`var n uint64
func Process(s, i []byte) uint64 { n++; return n }`,
			Uses{Checked: true, Globals: true, Allocation: true, GlobalHeapBytes: 8}},
		// Package variables combined with value types set both sides' flags;
		// the [60]Int table above the per-value limit is admitted because it
		// is only indexed.
		"globals with value types": {`type Int = [4]uint64
type P struct{ K, V uint64 }
var pows = [60]Int{{1}, {10}}
var recs [4]P
var origin = P{1, 2}
func init() { if pows[1][0] != 10 { panic("pows") } }
func Process(s, i []byte) uint64 { recs[i[0]&3].V++; if recs[0] == origin { return 1 }; return pows[i[1]%60][0] }`,
			Uses{Checked: true, Globals: true, Allocation: true, GlobalHeapBytes: 64, ReadOnlyGlobals: 2,
				StructEquality: true, TypeAliases: true, CompositeArrays: true, Panic: true}},
		"local var is not global": {`func Process(s, i []byte) uint64 { var n uint64; return n }`,
			Uses{Checked: true}},
	} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "program.go"), []byte("package main\n"+tc.src+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
		got := checkedReport(t, dir).Uses
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: %+v, want %+v", name, got, tc.want)
		}
	}
}
