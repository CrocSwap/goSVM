package compiler

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Opt-in reachability trimming (trim.go, docs/GO_PACKAGES.md#trimming).

// trimDep is a dependency package with declarations the subset rejects,
// none of which Process reaches: an unsupported standard-library import
// (math/big), floats, a reference to a symbol the strconv overlay leaves
// out, an unused initializer and a blank one, next to the reached code (a
// method called through an interface, a sentinel error, a constant, a
// generic type's method and an init function).
const trimDep = `package dep

import (
	"errors"
	"math/big"
	"strconv"
)

var ErrZero = errors.New("zero")

var unusedErr = errors.New("unused initializer")

var _ = strconv.Itoa(7)

var Count uint64

type T struct{ v uint64 }

func (t T) Get() uint64 { return t.v + 1 }

func (t T) Float() float64 { return float64(t.v) }

func (t *T) Text() string { return strconv.FormatFloat(1.5, 'f', 2, 64) }

type Getter interface{ Get() uint64 }

type Box[E any] struct{ v E }

func (b Box[E]) Get() uint64 { return 2 }

func (b Box[E]) Ratio() float32 { return 0.5 }

func Big() *big.Int { return big.NewInt(1) }

func F(x uint64) (uint64, error) {
	var g Getter = T{x}
	var h Getter = Box[uint8]{}
	if x == 0 {
		return 0, ErrZero
	}
	return g.Get() + h.Get(), nil
}

const Pi = 3.14

const K = 7

func init() { Count = K }
`

const trimApp = `package app

import "example.com/dep"

func Process(s, i []byte) uint64 {
	v, err := dep.F(uint64(len(i)))
	if err != nil {
		return 1
	}
	return v + dep.K + dep.Count
}
`

func trimProgram(dep, app string, trim ...string) *Program {
	return &Program{Entry: "example.com/app", Trim: trim, Packages: []PackageSources{
		{Path: "example.com/dep", Sources: []Source{{Name: "dep/dep.go", Data: []byte(dep)}}},
		{Path: "example.com/app", Sources: []Source{{Name: "app/app.go", Data: []byte(app)}}},
	}}
}

func TestTrimDropsUnreachable(t *testing.T) {
	// By default the whole package is checked, and the rejection names the
	// opt-in.
	_, err := Check(trimProgram(trimDep, trimApp))
	if err == nil || !strings.Contains(err.Error(), `math/big`) || !strings.HasSuffix(err.Error(), fmt.Sprintf(trimHintText, "example.com/dep")) {
		t.Fatalf("whole-package rejection with the opt-in hint wanted: %v", err)
	}
	checked, err := Check(trimProgram(trimDep, trimApp, "example.com/dep"))
	if err != nil {
		t.Fatal(err)
	}
	r := checked.Trimmed
	if r == nil || len(r.Packages) != 1 || r.Packages[0].Path != "example.com/dep" {
		t.Fatalf("report %+v", r)
	}
	var dropped []string
	for _, d := range r.Packages[0].Dropped {
		dropped = append(dropped, fmt.Sprintf("%s %s %s %v", d.Kind, d.Name, d.Position, d.Initializer))
	}
	want := []string{
		"var unusedErr dep.go:11 true",
		"var _ dep.go:13 true",
		"method T.Float dep.go:21 false",
		"method T.Text dep.go:23 false",
		"method Box.Ratio dep.go:31 false",
		"func Big dep.go:33 false",
		"const Pi dep.go:44 false",
	}
	if !reflect.DeepEqual(dropped, want) {
		t.Errorf("dropped:\n%s\nwant:\n%s", strings.Join(dropped, "\n"), strings.Join(want, "\n"))
	}
	if !reflect.DeepEqual(r.Packages[0].TypeOnlyImports, []string{"math/big"}) {
		t.Errorf("type-only imports %v", r.Packages[0].TypeOnlyImports)
	}
	if got := r.Summary(); len(got) != 1 || got[0] != "trimmed example.com/dep: dropped 7 declarations (2 initializers); type-only imports, not compiled or initialized: math/big" {
		t.Errorf("summary %q", got)
	}
	// The checked program has none of them: no function, no initializer.
	for _, fn := range checked.Functions {
		if name := fn.Object.Name(); name == "Float" || name == "Text" || name == "Big" || name == "Ratio" {
			t.Errorf("dropped %s still checked", name)
		}
	}
	for _, g := range checked.Globals {
		if g.Var.Name() == "unusedErr" {
			t.Error("dropped variable still a global")
		}
	}
	c, err := EmitC(checked)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(c), "unused initializer") || !strings.Contains(string(c), `"zero"`) {
		t.Error("initializers: the dropped one must not run, the reached one must")
	}
	// Whole-package programs are unchanged by an unrelated pattern.
	plain := trimProgram("package dep\nconst K = 7\nvar Count uint64\nfunc F(x uint64) (uint64, error) { return x, nil }\n", trimApp)
	a, err := CompileProgram(plain)
	if err != nil {
		t.Fatal(err)
	}
	plain.Trim = []string{"example.com/other/..."}
	b, err := CompileProgram(plain)
	if err != nil || string(a) != string(b) {
		t.Fatalf("an unmatched pattern changed the C: %v", err)
	}
	plain.Trim = []string{"example.com/dep"}
	b, err = CompileProgram(plain)
	if err != nil || string(a) != string(b) {
		t.Fatalf("trimming a package with nothing to drop changed the C: %v", err)
	}
}

// TestTrimRejections: what Process reaches is checked exactly as before,
// with the same messages (only the opt-in hint is gone).
func TestTrimRejections(t *testing.T) {
	for name, c := range map[string]struct{ dep, app, want string }{
		"reached float": {"package dep\nfunc F() uint64 { var f float64 = 1; return uint64(f) }\n",
			"package app\nimport \"example.com/dep\"\nfunc Process(s, i []byte) uint64 { return dep.F() }\n", ""},
		"unsupported init function": {"package dep\nfunc init() { go g() }\nfunc g() {}\nfunc F() uint64 { return 1 }\n",
			"package app\nimport \"example.com/dep\"\nfunc Process(s, i []byte) uint64 { return dep.F() }\n", ""},
		"reached recursion": {"package dep\nfunc F() uint64 { return F() }\n",
			"package app\nimport \"example.com/dep\"\nfunc Process(s, i []byte) uint64 { return dep.F() }\n", ""},
		"method reached through an interface": {"package dep\ntype T struct{}\nfunc (T) Get() uint64 { var f float32 = 1; return uint64(f) }\ntype I interface{ Get() uint64 }\nfunc F() uint64 { var i I = T{}; return i.Get() }\n",
			"package app\nimport \"example.com/dep\"\nfunc Process(s, i []byte) uint64 { return dep.F() }\n", ""},
		"reached use of an unsupported import": {"package dep\nimport \"math/big\"\nfunc F() uint64 { return big.NewInt(1).Uint64() }\n",
			"package app\nimport \"example.com/dep\"\nfunc Process(s, i []byte) uint64 { return dep.F() }\n",
			"dep/dep.go:2:8: " + stdlibImportRule("math/big")},
		"reached left-out overlay symbol": {"package dep\nimport \"strconv\"\nfunc F() uint64 { return uint64(len(strconv.FormatFloat(1, 'f', 1, 64))) }\n",
			"package app\nimport \"example.com/dep\"\nfunc Process(s, i []byte) uint64 { return dep.F() }\n", ""},
	} {
		t.Run(name, func(t *testing.T) {
			_, whole := Check(trimProgram(c.dep, c.app))
			_, trimmed := Check(trimProgram(c.dep, c.app, "example.com/dep"))
			if trimmed == nil {
				t.Fatal("trimming accepted reached unsupported code")
			}
			want := c.want
			if want == "" {
				if whole == nil {
					t.Fatal("whole-package check accepted")
				}
				want = strings.TrimSuffix(whole.Error(), fmt.Sprintf(trimHintText, "example.com/dep"))
			}
			if trimmed.Error() != want {
				t.Fatalf("got  %q\nwant %q", trimmed, want)
			}
		})
	}
}

// TestTrimEntryPackage: the entry package is trimmed only when a pattern
// names it exactly.
func TestTrimEntryPackage(t *testing.T) {
	app := trimApp + "\nfunc unused() float64 { return 1 }\n"
	dep := "package dep\nconst K = 7\nvar Count uint64\nfunc F(x uint64) (uint64, error) { return x, nil }\n"
	if _, err := Check(trimProgram(dep, app, "example.com/...")); err == nil || !strings.Contains(err.Error(), "float") {
		t.Fatalf("a /... pattern trimmed the entry package: %v", err)
	}
	checked, err := Check(trimProgram(dep, app, "example.com/app"))
	if err != nil {
		t.Fatal(err)
	}
	if got := checked.Trimmed.Summary(); len(got) != 1 || !strings.HasPrefix(got[0], "trimmed example.com/app: dropped 1 declarations") {
		t.Fatalf("summary %q", got)
	}
}

func TestValidateTrim(t *testing.T) {
	for _, ok := range []string{"example.com/lib", "example.com/lib/...", "lib"} {
		if err := ValidateTrim([]string{ok}); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "/...", "example.com/.../x", "example.com/lib/", "/abs", "a b"} {
		if err := ValidateTrim([]string{bad}); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	for path, want := range map[string]bool{"example.com/lib": true, "example.com/lib/x": true, "example.com/library": false} {
		if matchTrim([]string{"example.com/lib/..."}, path) != want {
			t.Errorf("%s: want %v", path, want)
		}
	}
}

// TestTrimModules: module packages only a trimmed package imports are type
// information, compiled (promoted) only when a reached declaration uses
// them; then they follow the ordinary rules.
func TestTrimModules(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		writePackageFile(t, dir, name, content)
	}
	write("app/go.mod", "module example.com/app\n\ngo 1.22\n\nrequire (\n\texample.com/a v0.0.0\n\texample.com/b v0.0.0\n)\n\nreplace example.com/a => ../a\n\nreplace example.com/b => ../b\n")
	write("a/go.mod", "module example.com/a\n\ngo 1.22\n\nrequire example.com/b v0.0.0\n")
	write("b/go.mod", "module example.com/b\n\ngo 1.22\n")
	write("a/a.go", "package a\n\nimport (\n\t\"fmt\"\n\n\t\"example.com/b\"\n)\n\nfunc F() uint64 { return 1 }\n\nfunc G() uint64 { return b.H() }\n\nfunc S() string { return fmt.Sprint(1) }\n")
	write("b/b.go", "package b\n\nimport \"fmt\"\n\nvar Calls uint64 = 3\n\nfunc H() uint64 { return Calls }\n\nfunc P() { fmt.Println() }\n")
	program := func(call string, trim ...string) (*Program, error) {
		write("app/program.go", "package app\n\nimport \"example.com/a\"\n\nfunc Process(s, i []byte) uint64 { return "+call+" }\n")
		return ReadProgramWithOptions(filepath.Join(dir, "app"), ReadOptions{Trim: trim})
	}
	// Default: a's fmt import is rejected at resolution, with the hint.
	if _, err := program("a.F()"); err == nil || !strings.HasSuffix(err.Error(), fmt.Sprintf(trimHintText, "example.com/a")) {
		t.Fatalf("default: %v", err)
	}
	// Trimmed a, b used only by its dropped G: type information only.
	p, err := program("a.F()", "example.com/a")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.TypeOnly) != 1 || p.TypeOnly[0].Path != "example.com/b" {
		t.Fatalf("type-only packages %+v", p.TypeOnly)
	}
	checked, err := Check(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := checked.Packages["example.com/b"]; ok {
		t.Error("b compiled although only a dropped declaration uses it")
	}
	if got := checked.Trimmed.Packages[0].TypeOnlyImports; !reflect.DeepEqual(got, []string{"example.com/b", "fmt"}) {
		t.Errorf("type-only imports %v", got)
	}
	// The cache key includes the trim configuration.
	plainKey, err := DependencyKeyWithOptions(filepath.Join(dir, "app"), BuildOptions{Trim: []string{"example.com/a"}})
	if err != nil {
		t.Fatal(err)
	}
	otherKey, err := DependencyKeyWithOptions(filepath.Join(dir, "app"), BuildOptions{Trim: []string{"example.com/a/..."}})
	if err != nil || otherKey == plainKey {
		t.Fatalf("trim patterns not in the dependency key: %v", err)
	}
	// G reached: b is compiled, and checked as a whole (it is not trimmed).
	p, err = program("a.G()", "example.com/a")
	if err != nil {
		t.Fatal(err)
	}
	_, err = Check(p)
	if err == nil || !strings.Contains(err.Error(), `b.go:3:8: unsupported import "fmt"`) || !strings.HasSuffix(err.Error(), fmt.Sprintf(trimHintText, "example.com/b")) {
		t.Fatalf("promoted whole package: %v", err)
	}
	// Both trimmed: b's reached H compiles, P is dropped.
	p, err = program("a.G()", "example.com/a", "example.com/b")
	if err != nil {
		t.Fatal(err)
	}
	checked, err = Check(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := checked.Packages["example.com/b"]; !ok {
		t.Fatal("b not compiled")
	}
	if got := checked.Trimmed.Summary(); !reflect.DeepEqual(got, []string{
		"trimmed example.com/a: dropped 2 declarations (0 initializers); type-only imports, not compiled or initialized: fmt",
		"trimmed example.com/b: dropped 1 declarations (0 initializers); type-only imports, not compiled or initialized: fmt",
	}) {
		t.Errorf("summary %q", got)
	}
	// A module the trimmed package imports must be in the module cache.
	write("a/a.go", "package a\n\nimport \"example.com/missing/m\"\n\nfunc F() uint64 { return 1 }\n\nfunc G() uint64 { return m.X }\n")
	if _, err = program("a.F()", "example.com/a"); err == nil || !strings.Contains(err.Error(), `example.com/missing/m`) {
		t.Fatalf("missing type-only module: %v", err)
	}
}

// TestTrimWarnings: builds write the report beside the output and warn
// from it.
func TestTrimWarnings(t *testing.T) {
	checked, err := Check(trimProgram(trimDep, trimApp, "example.com/dep"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := MarshalTrimReport(checked.Trimmed)
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "program.so")
	if lines, err := TrimWarnings(output); err != nil || lines != nil {
		t.Fatalf("no report: %v %v", lines, err)
	}
	if err = os.WriteFile(TrimReportPath(output), b, 0644); err != nil {
		t.Fatal(err)
	}
	lines, err := TrimWarnings(output)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"trimmed example.com/dep: dropped 7 declarations (2 initializers); type-only imports, not compiled or initialized: math/big",
		"full list of dropped declarations: " + TrimReportPath(output)}
	if !reflect.DeepEqual(lines, want) {
		t.Fatalf("warnings %q", lines)
	}
}
