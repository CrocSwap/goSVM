package features

import (
	"os"
	"path/filepath"
	"testing"
)

// Allocation builtins have their own construct counters, in addition to
// builtin_calls and builtins[name].
func TestAllocationCounters(t *testing.T) {
	dir := t.TempDir()
	source := "package program\ntype N struct{ Next *N }\nfunc Process(s, i []byte) uint64 {\n\tp := new(N)\n\tw := make([]uint64, 2)\n\tw = append(w, 1, 2)\n\tw = append(w, w...)\n\tn := uint64(copy(w, w[1:]))\n\tl := []uint16{1}\n\tp.Next = &N{}\n\treturn n + uint64(len(l))\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "program.go"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	h := report(t, filepath.Join(dir, "program.go")).Language.Handwritten
	for name, want := range map[string]int{"new_calls": 1, "make_calls": 1, "append_calls": 2, "append_spreads": 1, "copy_calls": 1, "slice_literals": 1, "builtin_calls": 6} {
		if h.Constructs[name] != want {
			t.Errorf("%s = %d, want %d", name, h.Constructs[name], want)
		}
	}
	if h.Builtins["append"] != 2 || h.Builtins["new"] != 1 {
		t.Errorf("builtins: %v", h.Builtins)
	}
}
