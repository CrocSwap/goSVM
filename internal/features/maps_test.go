package features

import (
	"os"
	"path/filepath"
	"testing"
)

// Maps have their own construct counters and set uses.maps; a map type that
// is only declared (never a value) does not.
func TestMapCounters(t *testing.T) {
	dir := t.TempDir()
	source := `package program

type Set map[uint8]struct{}

func Process(s, i []byte) uint64 {
	m := make(map[uint8]uint64, 4)
	n := map[uint8]uint64{1: 2}
	set := Set{}
	m[i[0]] = 1
	m[i[1]]++
	v := m[i[2]] + n[1]
	w, ok := n[i[3]]
	var x, ok2 = m[0]
	_, ok3 := set[1]
	delete(m, 1)
	delete(set, 2)
	clear(n)
	if ok || ok2 || ok3 {
		v = v + w + x
	}
	return v + uint64(len(m))
}
`
	if err := os.WriteFile(filepath.Join(dir, "program.go"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	u := checkedReport(t, dir)
	h := u.Language.Handwritten
	for name, want := range map[string]int{"map_literals": 2, "map_makes": 1, "map_lookups": 2, "map_comma_ok_lookups": 3,
		"map_assignments": 2, "map_deletes": 2, "map_clears": 1, "map_types": 3} {
		if h.Constructs[name] != want {
			t.Errorf("%s = %d, want %d", name, h.Constructs[name], want)
		}
	}
	if h.Constructs["map_values"] == 0 {
		t.Error("map_values = 0")
	}
	if !u.Uses.Checked || !u.Uses.Maps || !u.Uses.Allocation {
		t.Errorf("uses: %+v", u.Uses)
	}
	declared := t.TempDir()
	if err := os.WriteFile(filepath.Join(declared, "program.go"), []byte("package program\ntype M map[uint8]uint8\nfunc Process(s, i []byte) uint64 { return 0 }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if u := checkedReport(t, declared); u.Uses.Maps || u.Language.Handwritten.Constructs["map_values"] != 0 {
		t.Errorf("a declared-only map type uses maps: %+v", u.Uses)
	}
}

// Maps combined with closures, function values and defer set every
// matching flag and counter.
func TestMapFunctionCounters(t *testing.T) {
	dir := t.TempDir()
	source := `package program

type Counter map[string]uint64

func (c Counter) Inc(k string) { c[k]++ }

func drop(m map[uint8]uint64, k uint8) { delete(m, k) }

func Process(s, i []byte) uint64 {
	m := map[uint8]uint64{}
	f := func(k uint8) { m[k]++ }
	f(i[0])
	ops := map[string]func(uint64) uint64{"id": func(x uint64) uint64 { return x }}
	defer drop(m, 1)
	c := Counter{}
	inc := c.Inc
	inc("a")
	return ops["id"](m[i[0]]) + c["a"]
}
`
	if err := os.WriteFile(filepath.Join(dir, "program.go"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	u := checkedReport(t, dir)
	h := u.Language.Handwritten
	for name, want := range map[string]int{"map_literals": 3, "function_literals": 2, "method_values": 1, "defer": 1, "indirect_calls": 3} {
		if h.Constructs[name] != want {
			t.Errorf("%s = %d, want %d", name, h.Constructs[name], want)
		}
	}
	if !u.Uses.Checked || !u.Uses.Maps || !u.Uses.Closures || !u.Uses.Defer || !u.Uses.Allocation || !u.Uses.Strings || u.Uses.RecursionCheckInconclusive {
		t.Errorf("uses: %+v", u.Uses)
	}
}
