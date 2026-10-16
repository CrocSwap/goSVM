package features

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gosvm/internal/compiler"
)

// TestTrimmedUses: uses.trimmed lists, per trimmed package, the dropped
// declarations, the dropped initializers and the type-only imports
// (docs/GO_PACKAGES.md#trimming); it is omitted without trimming.
func TestTrimmedUses(t *testing.T) {
	dep := "package dep\n\nimport \"math/big\"\n\nvar unused = big.NewInt(1)\n\nvar Used uint64 = 2\n\nfunc Big() *big.Int { return big.NewInt(2) }\n\nfunc F() uint64 { return Used }\n"
	dir := t.TempDir()
	for name, content := range map[string]string{
		"go.mod":     "module example.com/app\n\ngo 1.22\n",
		"program.go": "package app\n\nimport \"example.com/app/dep\"\n\nfunc Process(s, i []byte) uint64 { return dep.F() }\n",
		"dep/dep.go": dep,
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	report := func(trim ...string) (*Usage, *compiler.Program) {
		t.Helper()
		p, err := compiler.ReadProgramWithOptions(dir, compiler.ReadOptions{Trim: trim})
		if err != nil {
			t.Fatal(err)
		}
		tp, err := compiler.TypeCheckProgram(p)
		if err != nil {
			t.Fatal(err)
		}
		u, err := Report(tp)
		if err != nil {
			t.Fatal(err)
		}
		return u, p
	}
	u, p := report("example.com/app/dep")
	cp, err := compiler.Check(p)
	ApplyCheck(u, cp, err)
	if !u.Uses.Checked {
		t.Fatalf("not checked: %s", u.Uses.CheckError)
	}
	want := []TrimmedUse{{Package: "example.com/app/dep", DroppedDeclarations: []string{"func Big", "var unused"},
		DroppedInitializers: []string{"unused"}, TypeOnlyImports: []string{"math/big"}}}
	if !reflect.DeepEqual(u.Uses.Trimmed, want) {
		t.Fatalf("trimmed %+v", u.Uses.Trimmed)
	}
	b, err := Marshal(u)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"trimmed": [`) {
		t.Fatalf("no uses.trimmed in %s", b)
	}
	// Without trimming the field is omitted.
	if err = os.WriteFile(filepath.Join(dir, "dep/dep.go"), []byte("package dep\n\nfunc F() uint64 { return 2 }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	u, _ = report()
	b, err = Marshal(u)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err = json.Unmarshal(b, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, ok := decoded["uses"].(map[string]any)["trimmed"]; ok {
		t.Fatal("uses.trimmed present without trimming")
	}
}
