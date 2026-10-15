package compiler

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"go/build"
	"go/format"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// stdlibCheck checks a single-file program and returns its error ("" when
// it checks).
func stdlibCheck(t *testing.T, source string) string {
	t.Helper()
	_, err := Check(&Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "p.go", Data: []byte(source)}}}}})
	if err == nil {
		return ""
	}
	return err.Error()
}

// TestStdlibRejections pins the diagnostics for standard-library symbols and
// packages that are not available on chain: by name, at the use, with the
// reason.
func TestStdlibRejections(t *testing.T) {
	importable := "bytes, encoding/binary, errors, math, strconv, strings, unicode, unicode/utf8"
	for name, c := range map[string]struct{ source, want string }{
		"fmt": {"package p\nimport \"fmt\"\nfunc Process(s, i []byte) uint64 { fmt.Println(); return 0 }\n",
			`p.go:2:8: could not import fmt (unsupported import "fmt"; standard-library packages on chain: ` + importable + ` and math/bits (docs/GO_STDLIB.md))`},
		"io": {"package p\nimport \"io\"\nvar _ io.Reader\nfunc Process(s, i []byte) uint64 { return 0 }\n",
			`p.go:2:8: could not import io (unsupported import "io"; standard-library packages on chain: ` + importable + ` and math/bits (docs/GO_STDLIB.md))`},
		"internal": {"package p\nimport \"internal/bytealg\"\nfunc Process(s, i []byte) uint64 { return uint64(bytealg.Count(s, 0)) }\n",
			`p.go:2:8: use of internal package "internal/bytealg" not allowed`},
		"math float function": {"package p\nimport \"math\"\nfunc Process(s, i []byte) uint64 { return uint64(math.Sqrt(4)) }\n",
			"p.go:3:55: math.Sqrt is not available on chain: floats are not supported on chain (docs/GO_STDLIB.md)"},
		"math float constant": {"package p\nimport \"math\"\nconst big = math.MaxFloat64\nfunc Process(s, i []byte) uint64 { return 0 }\n",
			"p.go:3:18: math.MaxFloat64 is not available on chain: floats are not supported on chain (docs/GO_STDLIB.md)"},
		"math pi": {"package p\nimport m \"math\"\nfunc Process(s, i []byte) uint64 { return uint64(m.Pi * 1000) }\n",
			"p.go:3:52: math.Pi is not available on chain: floats are not supported on chain (docs/GO_STDLIB.md)"},
		"strconv float": {"package p\nimport \"strconv\"\nfunc Process(s, i []byte) uint64 { _, err := strconv.ParseFloat(\"1\", 64); if err != nil { return 1 }; return 0 }\n",
			"p.go:3:54: strconv.ParseFloat is not available on chain: floats are not supported on chain (docs/GO_STDLIB.md)"},
		"binary reflection": {"package p\nimport \"encoding/binary\"\nfunc Process(s, i []byte) uint64 { return uint64(binary.Size(s)) }\n",
			"p.go:3:57: encoding/binary.Size is not available on chain: it uses reflection (docs/GO_STDLIB.md)"},
		"binary io": {"package p\nimport \"encoding/binary\"\nfunc Process(s, i []byte) uint64 { v, _ := binary.ReadUvarint(nil); return v }\n",
			"p.go:3:51: encoding/binary.ReadUvarint is not available on chain: it reads from an io.ByteReader; package io is not available on chain (docs/GO_STDLIB.md)"},
		"bytes buffer": {"package p\nimport \"bytes\"\nfunc Process(s, i []byte) uint64 { var b bytes.Buffer; b.Write(s); return 0 }\n",
			"p.go:3:48: bytes.Buffer is not available on chain: it implements io interfaces; package io is not available on chain (docs/GO_STDLIB.md)"},
		"strings reader": {"package p\nimport \"strings\"\nfunc Process(s, i []byte) uint64 { r := strings.NewReader(\"x\"); _ = r; return 0 }\n",
			"p.go:3:49: strings.NewReader is not available on chain: it implements io interfaces; package io is not available on chain (docs/GO_STDLIB.md)"},
		"strings title": {"package p\nimport \"strings\"\nfunc Process(s, i []byte) uint64 { return uint64(len(strings.Title(string(s)))) }\n",
			"p.go:3:62: strings.Title is not available on chain: it needs Unicode letter and digit tables (and is deprecated) (docs/GO_STDLIB.md)"},
		"unicode table": {"package p\nimport \"unicode\"\nfunc Process(s, i []byte) uint64 { if unicode.IsUpper(rune(s[0])) { return 1 }; return 0 }\n",
			"p.go:3:47: unicode.IsUpper is not available on chain: goSVM's on-chain unicode does not include it (docs/GO_STDLIB.md)"},
		"errors as any target": {"package p\nimport \"errors\"\nfunc Process(s, i []byte) uint64 { var t any; if errors.As(nil, t) { return 1 }; return 0 }\n",
			"p.go:3:50: errors.As on chain needs a target of static pointer type, as in errors.As(err, &target): goSVM has no reflection to find the type of an any target (docs/GO_STDLIB.md#errors); cannot infer T (gosvm-stdlib/go1.22.0/errors/wrap.go:117:9)"},
		"errors join value": {"package p\nimport \"errors\"\nfunc Process(s, i []byte) uint64 { f := errors.Join; _ = f; return 0 }\n",
			"p.go:3:41: variadic function types are unsupported"},
		"not in go": {"package p\nimport \"strings\"\nfunc Process(s, i []byte) uint64 { return uint64(strings.Nope()) }\n",
			"p.go:3:58: undefined: strings.Nope"},
		"unexported": {"package p\nimport \"strings\"\nfunc Process(s, i []byte) uint64 { return uint64(strings.maxInt) }\n",
			"p.go:3:58: maxInt not exported by package strings"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := stdlibCheck(t, c.source); got != c.want {
				t.Fatalf("got  %q\nwant %q", got, c.want)
			}
		})
	}
}

// TestStdlibImportForms: plain and renamed imports in a single file (no
// go.mod), in a module program and in its shared packages.
func TestStdlibImportForms(t *testing.T) {
	for name, source := range map[string]string{
		"plain":   "package p\nimport \"strings\"\nfunc Process(s, i []byte) uint64 { return uint64(strings.Count(string(s), \"a\")) }\n",
		"renamed": "package p\nimport str \"strings\"\nimport bin \"encoding/binary\"\nfunc Process(s, i []byte) uint64 { return uint64(str.Count(string(s), \"a\")) + bin.LittleEndian.Uint64(i) }\n",
		"math":    "package p\nimport \"math\"\nfunc Process(s, i []byte) uint64 { return math.MaxUint32 }\n",
	} {
		t.Run(name, func(t *testing.T) {
			if got := stdlibCheck(t, source); got != "" {
				t.Fatal(got)
			}
		})
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/app\n\ngo 1.22\n")
	writeFile(t, filepath.Join(dir, "codec", "codec.go"), "package codec\n\nimport (\n\t\"encoding/binary\"\n\t\"strconv\"\n)\n\nfunc Amount(b []byte) (uint64, error) { return strconv.ParseUint(string(b), 10, 64) }\n\nfunc Put(b []byte, v uint64) { binary.BigEndian.PutUint64(b, v) }\n")
	writeFile(t, filepath.Join(dir, "program", "program.go"), "package program\n\nimport (\n\t\"errors\"\n\t\"strconv\"\n\n\t\"example.com/app/codec\"\n)\n\nfunc Process(s, i []byte) uint64 {\n\tv, err := codec.Amount(i)\n\tif errors.Is(err, strconv.ErrSyntax) {\n\t\treturn 1\n\t}\n\tcodec.Put(s, v)\n\treturn 0\n}\n")
	p, err := ReadProgram(filepath.Join(dir, "program"))
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, unit := range p.Packages {
		paths = append(paths, unit.Path)
	}
	// Overlay packages are not part of the program's sources.
	if fmt.Sprint(paths) != "[example.com/app/codec example.com/app/program]" {
		t.Fatalf("packages %v", paths)
	}
	if _, err := Check(p); err != nil {
		t.Fatal(err)
	}
	stdlib, err := ProgramStdlib(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(stdlib, " "); got != "encoding/binary errors internal/bytealg internal/errtext strconv unicode/utf8" {
		t.Fatalf("ProgramStdlib %q", got)
	}
}

func writeFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
}

// TestStdlibConstantsOnly: math has only constants on chain, so a program
// using them emits the same C as one that writes the values out.
func TestStdlibConstantsOnly(t *testing.T) {
	compile := func(source string) []byte {
		out, err := CompileSources([]Source{{Name: "p.go", Data: []byte(source)}})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	a := compile("package p\nimport \"math\"\nfunc Process(s, i []byte) uint64 { if uint64(s[0]) > math.MaxInt8 { return math.MaxUint32 }; return uint64(math.MaxInt16) }\n")
	b := compile("package p\nfunc Process(s, i []byte) uint64 { if uint64(s[0]) > 127 { return 4294967295 }; return uint64(32767) }\n")
	if !bytes.Equal(a, b) {
		t.Fatal("math constants changed the emitted C")
	}
}

// TestStdlibIntrinsics checks the intrinsic and dispatch-except marks in
// the checked program and the C they produce.
func TestStdlibIntrinsics(t *testing.T) {
	p := &Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "p.go", Data: []byte(`package p
import (
	"errors"
	"strconv"
	"strings"
)
type L struct{ b []byte }
func (l L) Error() string { return "l" }
func Process(s, i []byte) uint64 {
	var b strings.Builder
	b.WriteString(string(i))
	_, err := strconv.Atoi(b.String())
	if errors.Is(err, L{}) || errors.Is(errors.Join(err, L{}), strconv.ErrSyntax) {
		return 1
	}
	return uint64(len(err.Error()))
}
`)}}}}}
	checked, err := Check(p)
	if err != nil {
		t.Fatal(err)
	}
	intrinsics := map[string]string{}
	for _, fn := range checked.Functions {
		if fn.Intrinsic != "" {
			intrinsics[fn.Object.Pkg().Path()+"."+fn.Object.Name()] = fn.Intrinsic
		}
	}
	want := map[string]string{
		"errors.comparableType": "comparable", "strings.unsafeString": "unsafeString", "strings.noescape": "noescape",
		"strconv.unsafeString": "unsafeString", "internal/errtext.unsafeString": "unsafeString",
	}
	if fmt.Sprint(intrinsics) != fmt.Sprint(want) {
		t.Fatalf("intrinsics %v, want %v", intrinsics, want)
	}
	var excepts []string
	for _, slot := range checked.MethodSlots {
		if slot.ExceptImplementers != nil {
			excepts = append(excepts, slot.Key+" ["+strings.Join(slot.Except, " ")+"]")
		}
	}
	if got := strings.Join(excepts, "; "); got != "Error() (string) except implementers of internal/errtext.Node [*errors.joinError *strconv.NumError]" {
		t.Fatalf("dispatch-except slots %q", got)
	}
	c, err := emitC(checked)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"goSVM intrinsic: dynamic type is comparable", "goSVM intrinsic: string sharing the slice's bytes", "goSVM intrinsic: noescape", "except implementers of internal/errtext.Node"} {
		if !bytes.Contains(c, []byte(s)) {
			t.Errorf("C lacks %q", s)
		}
	}
	// Directives outside the overlay are plain comments.
	user := "package p\n//gosvm:intrinsic comparable\nfunc f(x any) bool { return true }\nfunc Process(s, i []byte) uint64 { if f(s) { return 1 }; return 0 }\n"
	checked, err = Check(&Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "p.go", Data: []byte(user)}}}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, fn := range checked.Functions {
		if fn.Intrinsic != "" {
			t.Fatalf("user function %s marked %s", fn.Object.Name(), fn.Intrinsic)
		}
	}
}

// TestStdlibIdentity: the identity covers every overlay file of the listed
// packages and the cache key includes it only for programs that reach them.
func TestStdlibIdentity(t *testing.T) {
	if StdlibIdentity(nil) != "" {
		t.Fatal("identity of no packages")
	}
	a, b := StdlibIdentity([]string{"strings"}), StdlibIdentity([]string{"strings", "unicode"})
	if a == "" || a == b || a != StdlibIdentity([]string{"strings"}) {
		t.Fatal("identity not deterministic and distinct")
	}
	var h1, h2 bytes.Buffer
	plain := &Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "p.go", Data: []byte("package p\nfunc Process(s, i []byte) uint64 { return 0 }\n")}}}}}
	if err := writeStdlibIdentity(&h1, plain); err != nil || h1.Len() != 0 {
		t.Fatalf("plain program key changed: %q %v", h1.String(), err)
	}
	uses := &Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "p.go", Data: []byte("package p\nimport \"unicode/utf8\"\nfunc Process(s, i []byte) uint64 { return uint64(utf8.RuneCount(s)) }\n")}}}}}
	if err := writeStdlibIdentity(&h2, uses); err != nil || !strings.HasPrefix(h2.String(), "stdlib:go1.22.0\x00unicode/utf8\x00"+StdlibIdentity([]string{"unicode/utf8"})) {
		t.Fatalf("stdlib key %q %v", h2.String(), err)
	}
}

// stdlibOrigins maps every overlay file to the Go 1.22.0 sources it is
// derived from (none for goSVM's own files) and how.
var stdlibOrigins = map[string]struct {
	from []string
	kind string
}{
	"bytes/bytes.go":                            {[]string{"bytes/bytes.go"}, "modified"},
	"bytes/gosvm.go":                            {[]string{"bytes/buffer.go"}, "goSVM helpers; maxInt from the origin"},
	"encoding/binary/binary.go":                 {[]string{"encoding/binary/binary.go"}, "extract"},
	"encoding/binary/native_endian_little.go":   {[]string{"encoding/binary/native_endian_little.go"}, "modified"},
	"encoding/binary/varint.go":                 {[]string{"encoding/binary/varint.go"}, "extract"},
	"errors/errors.go":                          {[]string{"errors/errors.go"}, "verbatim"},
	"errors/gosvm.go":                           {nil, "goSVM helpers"},
	"errors/join.go":                            {[]string{"errors/join.go"}, "modified"},
	"errors/wrap.go":                            {[]string{"errors/wrap.go"}, "modified"},
	"internal/bytealg/bytealg.go":               {[]string{"internal/bytealg/bytealg.go"}, "modified"},
	"internal/bytealg/compare_generic.go":       {[]string{"internal/bytealg/compare_generic.go"}, "modified"},
	"internal/bytealg/count_generic.go":         {[]string{"internal/bytealg/count_generic.go"}, "verbatim apart from the build constraint"},
	"internal/bytealg/equal_generic.go":         {[]string{"internal/bytealg/equal_generic.go"}, "modified"},
	"internal/bytealg/index_generic.go":         {[]string{"internal/bytealg/index_generic.go"}, "verbatim apart from the build constraint"},
	"internal/bytealg/indexbyte_generic.go":     {[]string{"internal/bytealg/indexbyte_generic.go"}, "verbatim apart from the build constraint"},
	"internal/bytealg/lastindexbyte_generic.go": {[]string{"internal/bytealg/lastindexbyte_generic.go"}, "verbatim"},
	"internal/errtext/errtext.go":               {[]string{"errors/join.go", "strconv/atoi.go"}, "goSVM; replaces the recursive Error methods of the origins"},
	"math/const.go":                             {[]string{"math/const.go"}, "extract"},
	"strconv/atob.go":                           {[]string{"strconv/atob.go"}, "verbatim"},
	"strconv/atoi.go":                           {[]string{"strconv/atoi.go"}, "modified"},
	"strconv/bytealg.go":                        {[]string{"strconv/bytealg.go"}, "verbatim apart from the build constraint"},
	"strconv/gosvm.go":                          {nil, "goSVM helpers"},
	"strconv/isprint.go":                        {[]string{"strconv/isprint.go"}, "verbatim"},
	"strconv/itoa.go":                           {[]string{"strconv/itoa.go"}, "verbatim"},
	"strconv/quote.go":                          {[]string{"strconv/quote.go"}, "modified"},
	"strings/builder.go":                        {[]string{"strings/builder.go"}, "modified"},
	"strings/clone.go":                          {[]string{"strings/clone.go"}, "modified"},
	"strings/compare.go":                        {[]string{"strings/compare.go"}, "verbatim"},
	"strings/gosvm.go":                          {[]string{"strings/builder.go"}, "goSVM helpers; noescape after the origin's"},
	"strings/strings.go":                        {[]string{"strings/strings.go"}, "modified"},
	"unicode/graphic.go":                        {[]string{"unicode/graphic.go", "unicode/letter.go"}, "extract, specialized to White_Space"},
	"unicode/letter.go":                         {[]string{"unicode/letter.go"}, "extract"},
	"unicode/tables.go":                         {[]string{"unicode/tables.go"}, "generated extract (TestStdlibUnicodeTables)"},
	"unicode/utf8/utf8.go":                      {[]string{"unicode/utf8/utf8.go"}, "modified"},
}

// stdlibProvenance derives _stdlib/PROVENANCE.json: the Go version, the
// license and, for every overlay file, its own hash and its origins' hashes
// in the host GOROOT.
func stdlibProvenance(t *testing.T) []byte {
	t.Helper()
	version, err := os.ReadFile(filepath.Join(build.Default.GOROOT, "VERSION"))
	if err != nil {
		t.Fatal(err)
	}
	if v := strings.SplitN(string(version), "\n", 2)[0]; v != StdlibVersion {
		t.Fatalf("host Go is %s; the overlay reproduces %s", v, StdlibVersion)
	}
	type origin struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	}
	type file struct {
		Overlay string   `json:"overlay"`
		SHA256  string   `json:"sha256"`
		Kind    string   `json:"kind"`
		Origins []origin `json:"origins"`
	}
	out := struct {
		Go       string `json:"go"`
		License  string `json:"license"`
		Identity string `json:"identity"`
		Files    []file `json:"files"`
	}{Go: StdlibVersion, License: "LICENSE (Go's BSD-style license, $GOROOT/LICENSE)", Identity: StdlibIdentity(StdlibPackages)}
	var names []string
	for _, p := range StdlibPackages {
		for _, src := range stdlibSources(p) {
			names = append(names, strings.TrimPrefix(src.Name, stdlibSourcePrefix))
		}
	}
	sort.Strings(names)
	for _, name := range names {
		o, ok := stdlibOrigins[name]
		if !ok {
			t.Fatalf("overlay file %s has no recorded origin", name)
		}
		data, err := stdlibFS.ReadFile(stdlibRoot + "/" + name)
		if err != nil {
			t.Fatal(err)
		}
		f := file{Overlay: name, SHA256: fmt.Sprintf("%x", sha256.Sum256(data)), Kind: o.kind, Origins: []origin{}}
		for _, from := range o.from {
			original, err := os.ReadFile(filepath.Join(build.Default.GOROOT, "src", filepath.FromSlash(from)))
			if err != nil {
				t.Fatal(err)
			}
			f.Origins = append(f.Origins, origin{from, fmt.Sprintf("%x", sha256.Sum256(original))})
			if o.kind == "verbatim" {
				// Verbatim files differ from the origin only by an added
				// goSVM comment.
				var kept []string
				for _, line := range strings.Split(string(data), "\n") {
					if !strings.HasPrefix(line, "// goSVM") && !strings.HasPrefix(line, "// unchanged") && !strings.HasPrefix(line, "// from its build") {
						kept = append(kept, line)
					}
				}
				if norm := strings.ReplaceAll(strings.Join(kept, "\n"), "\n\n\n", "\n\n"); norm != string(original) {
					t.Errorf("%s is not verbatim", name)
				}
			}
		}
		out.Files = append(out.Files, f)
	}
	if len(names) != len(stdlibOrigins) {
		t.Fatalf("%d overlay files, %d recorded origins", len(names), len(stdlibOrigins))
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(b, '\n')
}

// TestStdlibProvenance checks _stdlib/PROVENANCE.json and LICENSE against
// the host Go 1.22.0 toolchain (GOSVM_UPDATE_STDLIB=1 rewrites the JSON).
func TestStdlibProvenance(t *testing.T) {
	want := stdlibProvenance(t)
	path := filepath.Join("_stdlib", "PROVENANCE.json")
	if os.Getenv("GOSVM_UPDATE_STDLIB") != "" {
		if err := os.WriteFile(path, want, 0644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("_stdlib/PROVENANCE.json is stale; rerun with GOSVM_UPDATE_STDLIB=1")
	}
	license, err := os.ReadFile(filepath.Join(build.Default.GOROOT, "LICENSE"))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join("_stdlib", "LICENSE")); err != nil || !bytes.Equal(got, license) {
		t.Fatal("_stdlib/LICENSE is not Go's license")
	}
}

// TestStdlibPackagesList pins the overlay packages and keeps their sources
// gofmt-formatted, like Go's.
func TestStdlibPackagesList(t *testing.T) {
	want := "bytes encoding/binary errors internal/bytealg internal/errtext math strconv strings unicode unicode/utf8"
	if got := strings.Join(StdlibPackages, " "); got != want {
		t.Fatalf("StdlibPackages %q", got)
	}
	for _, p := range StdlibPackages {
		for _, src := range stdlibSources(p) {
			formatted, err := format.Source(src.Data)
			if err != nil || !bytes.Equal(formatted, src.Data) {
				t.Errorf("%s is not gofmt-formatted (%v)", src.Name, err)
			}
		}
	}
}
