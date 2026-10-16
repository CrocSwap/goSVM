// Command trim writes the on-chain copies of the example's libraries: the
// pinned upstream module sources from the Go module cache, minus the
// top-level declarations trim.json lists (each with the check error that
// rejected it). Every kept declaration is byte-identical to upstream; whole
// files whose declarations are all removed are left out. Each module keeps
// its upstream go.mod and license file, and only the packages the example
// uses are written.
//
//	go run ./trim -out <dir>            # <dir>/<name> for each module
//	go run ./trim -out <dir> -modcache <GOMODCACHE>
//
// Upstream files are verified against trim.json's SHA-256 before trimming,
// and the trimmed files against trim.json's trimmed_sha256, so the copies are
// reproducible. The manifest is regenerated from a peel of the unmodified
// modules (results/compiler/2026-10-16-real-libraries/README.md).
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

type removal struct {
	Line   int    `json:"line"`
	Decl   string `json:"decl"`
	Reason string `json:"reason"`
}

type file struct {
	File          string    `json:"file"`
	SHA256        string    `json:"sha256"`
	TrimmedSHA256 string    `json:"trimmed_sha256,omitempty"`
	Drop          bool      `json:"drop,omitempty"`
	Reason        string    `json:"reason,omitempty"` // why a dropped file is dropped
	Removed       []removal `json:"remove,omitempty"`
}

type module struct {
	Name    string   `json:"name"`
	Path    string   `json:"path"`
	Version string   `json:"version"`
	Extra   []string `json:"extra"` // license and module files copied unchanged
	Files   []file   `json:"files"`
}

type manifest struct {
	Modules []module `json:"modules"`
}

func main() {
	out := flag.String("out", "", "output directory (created; must not exist)")
	modcache := flag.String("modcache", "", "Go module cache (default: go env GOMODCACHE)")
	manifestPath := flag.String("manifest", "", "trim manifest (default: trim.json next to this command's source)")
	update := flag.Bool("update", false, "record the trimmed files' hashes in the manifest instead of checking them")
	flag.Parse()
	if *out == "" {
		fail("-out is required")
	}
	if *modcache == "" {
		b, err := exec.Command("go", "env", "GOMODCACHE").Output()
		if err != nil {
			fail("go env GOMODCACHE: %v", err)
		}
		*modcache = strings.TrimSpace(string(b))
	}
	if *manifestPath == "" {
		*manifestPath = filepath.Join("trim", "trim.json")
	}
	data, err := os.ReadFile(*manifestPath)
	if err != nil {
		fail("%v", err)
	}
	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		fail("%s: %v", *manifestPath, err)
	}
	if _, err := os.Stat(*out); err == nil {
		fail("%s exists", *out)
	}
	for i := range m.Modules {
		mod := &m.Modules[i]
		src := filepath.Join(*modcache, escape(mod.Path)+"@"+mod.Version)
		dst := filepath.Join(*out, mod.Name)
		if err := os.MkdirAll(dst, 0755); err != nil {
			fail("%v", err)
		}
		for _, extra := range mod.Extra {
			b, err := os.ReadFile(filepath.Join(src, extra))
			if err != nil {
				fail("%s@%s: %v (download it: go mod download %s@%s)", mod.Path, mod.Version, err, mod.Path, mod.Version)
			}
			write(filepath.Join(dst, extra), b)
		}
		listed := map[string]bool{}
		dirs := map[string]bool{}
		for j := range mod.Files {
			f := &mod.Files[j]
			listed[f.File] = true
			dirs[filepath.Dir(f.File)] = true
			b, err := os.ReadFile(filepath.Join(src, f.File))
			if err != nil {
				fail("%s@%s: %v", mod.Path, mod.Version, err)
			}
			if sum := fmt.Sprintf("%x", sha256.Sum256(b)); sum != f.SHA256 {
				fail("%s@%s %s: sha256 %s, manifest %s", mod.Path, mod.Version, f.File, sum, f.SHA256)
			}
			if f.Drop {
				continue
			}
			trimmed, err := trim(f.File, b, f.Removed)
			if err != nil {
				fail("%s@%s %s: %v", mod.Path, mod.Version, f.File, err)
			}
			sum := fmt.Sprintf("%x", sha256.Sum256(trimmed))
			if *update {
				f.TrimmedSHA256 = sum
			} else if sum != f.TrimmedSHA256 {
				fail("%s@%s %s: trimmed sha256 %s, manifest %s", mod.Path, mod.Version, f.File, sum, f.TrimmedSHA256)
			}
			write(filepath.Join(dst, f.File), trimmed)
		}
		// Every non-test Go file of the packages used is accounted for.
		for dir := range dirs {
			entries, err := os.ReadDir(filepath.Join(src, dir))
			if err != nil {
				fail("%v", err)
			}
			for _, e := range entries {
				name := filepath.Join(dir, e.Name())
				if strings.HasSuffix(e.Name(), ".go") && !strings.HasSuffix(e.Name(), "_test.go") && !listed[filepath.Clean(name)] {
					fail("%s@%s: %s is not in the manifest", mod.Path, mod.Version, name)
				}
			}
		}
	}
	if *update {
		b, err := json.MarshalIndent(m, "", "  ")
		if err != nil {
			fail("%v", err)
		}
		if err := os.WriteFile(*manifestPath, append(b, '\n'), 0644); err != nil {
			fail("%v", err)
		}
	}
}

// trim removes the listed declarations from src. A declaration is named as
// in the manifest ("func (*Int).ToBig" is "func *Int.ToBig", a spec of a
// grouped declaration is "var Name" or "import \"path\"") and found by its
// first line; a grouped declaration whose specs are all removed is removed
// whole. A removed declaration takes its doc comment, the rest of its last
// line and its line break with it.
func trim(name string, src []byte, removed []removal) ([]byte, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	want := map[int]string{}
	for _, r := range removed {
		if _, dup := want[r.Line]; dup {
			return nil, fmt.Errorf("two removals at line %d", r.Line)
		}
		want[r.Line] = r.Decl
	}
	type cut struct{ from, to int }
	var cuts []cut
	add := func(doc *ast.CommentGroup, n ast.Node) {
		from := fset.Position(n.Pos()).Offset
		if doc != nil {
			from = fset.Position(doc.Pos()).Offset
		}
		// The whole lines: back to the line start when only blanks precede,
		// forward through the line break.
		for from > 0 && (src[from-1] == ' ' || src[from-1] == '\t') {
			from--
		}
		to := fset.Position(n.End()).Offset
		for to < len(src) && src[to] != '\n' {
			to++
		}
		if to < len(src) {
			to++
		}
		cuts = append(cuts, cut{from, to})
	}
	found := 0
	match := func(n ast.Node, decl string) bool {
		line := fset.Position(n.Pos()).Line
		w, ok := want[line]
		if !ok {
			return false
		}
		if w != decl {
			panic(fmt.Sprintf("%s:%d: manifest names %q, the source has %q", name, line, w, decl))
		}
		found++
		return true
	}
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if match(d, funcName(d)) {
				add(d.Doc, d)
			}
		case *ast.GenDecl:
			if !d.Lparen.IsValid() {
				if match(d, d.Tok.String()+" "+specName(d.Specs[0])) {
					add(d.Doc, d)
				}
				continue
			}
			var specCuts []cut
			all := len(d.Specs) > 0
			for _, sp := range d.Specs {
				if !match(sp, d.Tok.String()+" "+specName(sp)) {
					all = false
					continue
				}
				before := len(cuts)
				add(specDoc(sp), sp)
				specCuts = append(specCuts, cuts[before:]...)
				cuts = cuts[:before]
			}
			if all {
				add(d.Doc, d)
			} else {
				cuts = append(cuts, specCuts...)
			}
		}
	}
	if found != len(removed) {
		return nil, fmt.Errorf("found %d of %d removed declarations", found, len(removed))
	}
	sort.Slice(cuts, func(i, j int) bool { return cuts[i].from < cuts[j].from })
	var b bytes.Buffer
	at := 0
	for _, c := range cuts {
		if c.from < at {
			return nil, fmt.Errorf("overlapping removals at offset %d", c.from)
		}
		b.Write(src[at:c.from])
		at = c.to
	}
	b.Write(src[at:])
	// The result must still parse.
	if _, err := parser.ParseFile(token.NewFileSet(), name, b.Bytes(), parser.ParseComments); err != nil {
		return nil, fmt.Errorf("trimmed source: %v", err)
	}
	return b.Bytes(), nil
}

func funcName(d *ast.FuncDecl) string {
	name := d.Name.Name
	if d.Recv != nil && len(d.Recv.List) == 1 {
		name = recvName(d.Recv.List[0].Type) + "." + name
	}
	return "func " + name
}

func recvName(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.StarExpr:
		return "*" + recvName(e.X)
	case *ast.Ident:
		return e.Name
	case *ast.IndexExpr:
		return recvName(e.X)
	}
	return "?"
}

func specName(s ast.Spec) string {
	switch s := s.(type) {
	case *ast.ImportSpec:
		return s.Path.Value
	case *ast.TypeSpec:
		return s.Name.Name
	case *ast.ValueSpec:
		var n []string
		for _, id := range s.Names {
			n = append(n, id.Name)
		}
		return strings.Join(n, ",")
	}
	return "?"
}

func specDoc(s ast.Spec) *ast.CommentGroup {
	switch s := s.(type) {
	case *ast.ImportSpec:
		return s.Doc
	case *ast.TypeSpec:
		return s.Doc
	case *ast.ValueSpec:
		return s.Doc
	}
	return nil
}

// escape is the module cache's case encoding of a module path.
func escape(path string) string {
	var b strings.Builder
	for _, r := range path {
		if unicode.IsUpper(r) {
			b.WriteByte('!')
			b.WriteRune(unicode.ToLower(r))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func write(path string, data []byte) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		fail("%v", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		fail("%v", err)
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "trim: "+format+"\n", args...)
	os.Exit(1)
}
