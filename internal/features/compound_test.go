package features

import (
	"os"
	"path/filepath"
	"testing"
)

// Compound assignment on integers passes Check; each statement counts as
// compound_assignments and as its operator, a map element target counts as
// map_assignments, and a signed target is a signed operation.
func TestCompoundCounters(t *testing.T) {
	dir := t.TempDir()
	source := `package program

func Process(s, i []byte) uint64 {
	x := uint64(i[0])
	x += 3
	x <<= 2
	x &^= 1
	var d int8 = -4
	d /= 3
	m := map[uint8]uint32{}
	m[i[1]] -= 1
	s[0] ^= i[2]
	return x + uint64(d) + uint64(m[0])
}
`
	if err := os.WriteFile(filepath.Join(dir, "program.go"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	u := checkedReport(t, dir)
	h := u.Language.Handwritten
	if h.Constructs["compound_assignments"] != 6 || h.Constructs["map_assignments"] != 1 {
		t.Errorf("constructs: %v", h.Constructs)
	}
	for op, n := range map[string]int{"+=": 1, "<<=": 1, "&^=": 1, "/=": 1, "-=": 1, "^=": 1} {
		if h.Operators[op] != n {
			t.Errorf("operator %s = %d, want %d", op, h.Operators[op], n)
		}
	}
	if !u.Uses.Checked || !u.Uses.SignedIntegers || !u.Uses.Maps || u.Uses.CheckError != "" {
		t.Errorf("uses: %+v", u.Uses)
	}
}
