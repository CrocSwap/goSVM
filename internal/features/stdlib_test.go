package features

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Standard-library overlay packages (docs/GO_STDLIB.md) are listed in
// uses.stdlib with the overlay packages they import; their calls are
// stdlib_calls, and their sources are not program files.
func TestStdlibUses(t *testing.T) {
	dir := t.TempDir()
	source := `package program

import (
	"errors"
	"math"
	"math/bits"
	"strings"
)

var ErrShort = errors.New("short")

func Process(s, i []byte) uint64 {
	if len(i) < 2 {
		return uint64(len(ErrShort.Error()))
	}
	u := strings.ToUpper(string(i))
	return uint64(bits.Len64(uint64(len(u)))) + math.MaxUint8
}
`
	if err := os.WriteFile(filepath.Join(dir, "program.go"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	u := checkedReport(t, dir)
	if got := strings.Join(u.Uses.Stdlib, " "); got != "errors internal/bytealg internal/errtext math strings unicode unicode/utf8" {
		t.Errorf("uses.stdlib %q", got)
	}
	if !u.Uses.Checked || !u.Uses.MathBits || !u.Uses.Strings || !u.Uses.Globals {
		t.Errorf("uses %+v", u.Uses)
	}
	var calls []string
	for _, c := range u.StdlibCalls {
		calls = append(calls, c.Name)
	}
	if got := strings.Join(calls, " "); got != "errors.New math/bits.Len64 strings.ToUpper" {
		t.Errorf("stdlib_calls %q", got)
	}
	if len(u.Files) != 1 || strings.Join(u.Program.Packages, " ") != u.Program.EntryPackage {
		t.Errorf("files %+v packages %v", u.Files, u.Program.Packages)
	}
	// A program without overlay imports reports none.
	plain := t.TempDir()
	if err := os.WriteFile(filepath.Join(plain, "program.go"), []byte("package program\nimport \"math/bits\"\nfunc Process(s, i []byte) uint64 { return uint64(bits.Len64(1)) }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if u := checkedReport(t, plain); u.Uses.Stdlib != nil || !u.Uses.MathBits {
		t.Errorf("plain uses %+v", u.Uses)
	}
}
