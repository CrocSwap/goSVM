package features

import (
	"os"
	"path/filepath"
	"testing"
)

// Strings have their own construct counters and set uses.strings; string
// constants folded into constant expressions do not.
func TestStringCounters(t *testing.T) {
	dir := t.TempDir()
	source := `package program

const k = "abc"

type E struct{}

func (E) Error() string { return "e" }

func Process(s, i []byte) uint64 {
	x := string(i)
	y := x + "-" + k
	y += x
	b := []byte(y)
	r := []rune(x)
	z := string(rune(i[0])) + string(r)
	var a any = x
	var err error = E{}
	if err != nil && a != nil {
		for range z {
		}
	}
	return uint64(len(b)) + uint64(len(k))
}
`
	if err := os.WriteFile(filepath.Join(dir, "program.go"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	u := checkedReport(t, dir)
	h := u.Language.Handwritten
	// x + "-" + k is two + operators; y += x one; string(rune(...)) + string(r) one.
	for name, want := range map[string]int{"string_concatenations": 4, "string_conversions": 5, "string_ranges": 1} {
		if h.Constructs[name] != want {
			t.Errorf("%s = %d, want %d", name, h.Constructs[name], want)
		}
	}
	if h.Constructs["string_values"] == 0 {
		t.Error("string_values = 0")
	}
	if !u.Uses.Checked || !u.Uses.Strings || !u.Uses.Interfaces {
		t.Errorf("uses: %+v", u.Uses)
	}
	constant := t.TempDir()
	if err := os.WriteFile(filepath.Join(constant, "program.go"), []byte("package program\nconst k = \"abc\"\nfunc Process(s, i []byte) uint64 { return uint64(len(k)) }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if u := checkedReport(t, constant); u.Uses.Strings || u.Language.Handwritten.Constructs["string_values"] != 0 {
		t.Errorf("a constant-only program uses strings: %+v", u.Uses)
	}
}
