package features

import (
	"os"
	"path/filepath"
	"testing"
)

// Interface values have their own construct counters; the program checks,
// so uses.checked is true.
func TestInterfaceCounters(t *testing.T) {
	dir := t.TempDir()
	source := `package program

type Shape interface{ Area() uint64 }
type R struct{ W uint64 }

func (r R) Area() uint64 { return r.W }

func Wrap[T any](v T) any { return v }

func pair() (R, bool) { return R{1}, true }

func Process(s, i []byte) uint64 {
	var a any = uint8(1)
	var sh Shape = R{2}
	b := any(sh)
	var c any = nil
	xs := []any{R{3}, nil, sh}
	var ok bool
	a, ok = pair()
	_ = Wrap(uint16(4))
	switch v := b.(type) {
	case R:
		_ = v
	}
	if r, ok2 := a.(R); ok2 && ok {
		return r.W + sh.Area() + b.(Shape).Area()
	}
	_, _ = c, xs
	return 0
}
`
	if err := os.WriteFile(filepath.Join(dir, "program.go"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	u := checkedReport(t, dir)
	h := u.Language.Handwritten
	// uint8(1), R{2}, R{3}, the pair() result's R, and v in Wrap (a type
	// parameter converted to any); uint16(4) is passed as T, not converted.
	for name, want := range map[string]int{"interface_conversions": 5, "dynamic_calls": 2, "type_assertions": 2, "type_switch": 1} {
		if h.Constructs[name] != want {
			t.Errorf("%s = %d, want %d", name, h.Constructs[name], want)
		}
	}
	if !u.Uses.Checked || !u.Uses.Interfaces || !u.Uses.Generics {
		t.Errorf("uses: %+v", u.Uses)
	}
}
