package features

import (
	"os"
	"path/filepath"
	"testing"
)

// Struct equality, type aliases, arrays of composite elements and panic have
// their own uses flags; the counters behind them only count those forms.
func TestValueTypeCounters(t *testing.T) {
	dir := t.TempDir()
	source := `package program

type P struct{ X, Y uint8 }
type Word = uint64
type Grid [2][3]uint8

func Process(s, i []byte) uint64 {
	a, b := P{1, 2}, P{X: i[0]}
	if a == b || a != b {
		s[0] = 1
	}
	switch a {
	case P{}, P{1, 0}:
		s[1] = 1
	}
	var ps [2]P
	var g Grid
	var w Word = uint64(ps[0].X) + uint64(g[1][2])
	if w > 3 {
		panic("big")
	}
	return w
}
`
	if err := os.WriteFile(filepath.Join(dir, "program.go"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	u := checkedReport(t, dir)
	h := u.Language.Handwritten
	for name, want := range map[string]int{"struct_equality": 2, "struct_switch_cases": 2, "type_aliases": 1, "composite_array_types": 2} {
		if h.Constructs[name] != want {
			t.Errorf("%s = %d, want %d", name, h.Constructs[name], want)
		}
	}
	if h.Builtins["panic"] != 1 {
		t.Errorf("panic calls = %d", h.Builtins["panic"])
	}
	if !u.Uses.Checked || !u.Uses.StructEquality || !u.Uses.TypeAliases || !u.Uses.CompositeArrays || !u.Uses.Panic {
		t.Errorf("uses: %+v", u.Uses)
	}
	if u.Uses.Interfaces || u.Uses.Strings || u.Uses.Allocation {
		t.Errorf("panic(\"big\") set unrelated uses: %+v", u.Uses)
	}
	// Scalar arrays, struct field comparisons and switches on scalars set none.
	plain := t.TempDir()
	if err := os.WriteFile(filepath.Join(plain, "program.go"), []byte(`package program
type P struct{ X uint8 }
func Process(s, i []byte) uint64 {
	var a [4]uint64
	p, q := P{1}, P{2}
	switch p.X { case 1: s[0] = 1 }
	if p.X == q.X && a == [4]uint64{} { return 1 }
	return 0
}
`), 0644); err != nil {
		t.Fatal(err)
	}
	if u := checkedReport(t, plain).Uses; u.StructEquality || u.TypeAliases || u.CompositeArrays || u.Panic {
		t.Errorf("plain program uses: %+v", u)
	}
}

// Pointer conversions (*T)(p) have their own counter and uses flag; other
// conversions do not set it.
func TestPointerConversionCounter(t *testing.T) {
	dir := t.TempDir()
	source := `package program

type A [4]uint64
type B [4]uint64

func Process(s, i []byte) uint64 {
	var a A
	b := (*B)(&a)
	b[0] = uint64(i[0])
	var n *A
	if (*B)(n) == nil {
		s[0] = 1
	}
	return B(a)[0] + uint64(uint8(a[0]))
}
`
	if err := os.WriteFile(filepath.Join(dir, "program.go"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	u := checkedReport(t, dir)
	if got := u.Language.Handwritten.Constructs["pointer_conversions"]; got != 2 {
		t.Errorf("pointer_conversions = %d, want 2", got)
	}
	if !u.Uses.Checked || !u.Uses.PointerConversions {
		t.Errorf("uses: %+v", u.Uses)
	}
	plain := t.TempDir()
	if err := os.WriteFile(filepath.Join(plain, "program.go"), []byte("package program\ntype A [2]uint64\ntype B [2]uint64\nfunc Process(s, i []byte) uint64 { var a A; return B(a)[0] + uint64(i[0]) }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if u := checkedReport(t, plain).Uses; u.PointerConversions {
		t.Errorf("array conversion set pointer_conversions: %+v", u)
	}
}

// Slice-to-array conversions have their own counter and uses flag; they are
// not pointer conversions.
func TestSliceArrayConversionCounter(t *testing.T) {
	dir := t.TempDir()
	source := `package program

type Key [4]byte

func Process(s, i []byte) uint64 {
	p := (*[4]byte)(s)
	k := Key(i)
	q := (*Key)(p)
	return uint64(p[0]) + uint64(k[1]) + uint64(q[2])
}
`
	if err := os.WriteFile(filepath.Join(dir, "program.go"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	u := checkedReport(t, dir)
	if got := u.Language.Handwritten.Constructs["slice_array_conversions"]; got != 2 {
		t.Errorf("slice_array_conversions = %d, want 2", got)
	}
	if got := u.Language.Handwritten.Constructs["pointer_conversions"]; got != 1 {
		t.Errorf("pointer_conversions = %d, want 1", got)
	}
	if !u.Uses.Checked || !u.Uses.SliceArrayConversions || !u.Uses.PointerConversions {
		t.Errorf("uses: %+v", u.Uses)
	}
}
