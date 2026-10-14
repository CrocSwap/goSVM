package features

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Function values, closures and defer have their own construct counters and
// uses flags; the program checks, so uses.checked is true.
func TestFunctionValueCounters(t *testing.T) {
	dir := t.TempDir()
	source := `package program

type C struct{ n uint64 }

func (c C) Get() uint64  { return c.n }
func (c *C) Inc()        { c.n++ }
func Id[T any](x T) T    { return x }
func two() (a, b uint64) { return 1, 2 }
func add(a, b uint64) uint64 { return a + b }

func Process(s, i []byte) uint64 {
	c := C{n: 1}
	f := add           // function value
	g := Id[uint64]    // function value (an instance)
	m := c.Get         // method value
	e := C.Get         // method expression
	h := func() (r uint64) { r = 2; return }
	defer c.Inc()
	defer func() {}()
	x, y := two()
	return f(x, y) + g(3) + m() + e(c) + h() + C.Get(c) + c.Get()
}
`
	if err := os.WriteFile(filepath.Join(dir, "program.go"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	u := checkedReport(t, dir)
	h := u.Language.Handwritten
	// Indirect calls: f, g, m, e, h and the deferred literal.
	for name, want := range map[string]int{"function_values": 2, "method_values": 1, "method_expressions": 1, "function_literals": 2,
		"indirect_calls": 6, "defer": 2, "named_results": 2, "method_calls": 2} {
		if h.Constructs[name] != want {
			t.Errorf("%s = %d, want %d", name, h.Constructs[name], want)
		}
	}
	if !u.Uses.Checked || !u.Uses.Closures || !u.Uses.Defer || !u.Uses.Generics {
		t.Errorf("uses: %+v", u.Uses)
	}
}

func TestFunctionValueUses(t *testing.T) {
	for name, tc := range map[string]struct {
		src  string
		want Uses
	}{
		"closure": {`func Process(s, i []byte) uint64 { x := uint64(1); f := func() uint64 { return x }; return f() }`,
			Uses{Checked: true, Closures: true, Allocation: true, HeapMovedLocals: 1}},
		"defer": {`func g() {}
func Process(s, i []byte) uint64 { defer g(); return 0 }`,
			Uses{Checked: true, Defer: true}},
		"named results only": {`func two() (a, b uint64) { return 1, 2 }
func Process(s, i []byte) uint64 { a, b := two(); return a + b }`,
			Uses{Checked: true}},
		"recursion through a value": {`func Process(s, i []byte) uint64 { var f func() uint64; f = func() uint64 { return f() }; return f() }`,
			Uses{Closures: true, Allocation: false}},
	} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "program.go"), []byte("package main\n"+tc.src+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
		got := checkedReport(t, dir).Uses
		if !tc.want.Checked && got.CheckError == "" {
			t.Errorf("%s: expected a Check error", name)
		}
		got.CheckError = ""
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: %+v, want %+v", name, got, tc.want)
		}
	}
}
