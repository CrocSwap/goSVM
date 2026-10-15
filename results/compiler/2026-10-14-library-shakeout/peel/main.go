// peel repeatedly runs `gosvm features <entry>` in a workspace, removes the
// top-level declaration (or import) containing each reported error, and logs
// the error until the program checks or no position is reported.
package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var posRe = regexp.MustCompile(`([^\s:(]+\.go):(\d+):(\d+): `)

type entry struct {
	Iter int    `json:"iter"`
	File string `json:"file"`
	Line int    `json:"line"`
	Decl string `json:"decl"`
	Msg  string `json:"msg"`
}

func main() {
	gosvm, ws, entryPkg := os.Args[1], os.Args[2], os.Args[3]
	enc := json.NewEncoder(os.Stdout)
	for i := 0; i < 2000; i++ {
		cmd := exec.Command(gosvm, "features", entryPkg)
		cmd.Dir = ws
		out, _ := cmd.CombinedOutput()
		s := string(out)
		if strings.HasPrefix(strings.TrimSpace(s), "{") {
			var u struct {
				Uses map[string]any `json:"uses"`
			}
			json.Unmarshal(out, &u)
			ce, _ := u.Uses["check_error"].(string)
			if ce == "" {
				fmt.Fprintln(os.Stderr, "CHECKED OK after", i, "removals")
				return
			}
			s = ce
		}
		line := strings.SplitN(strings.TrimSpace(s), "\n", 2)[0]
		all := posRe.FindAllStringSubmatchIndex(line, -1)
		var m []string
		if len(all) > 0 {
			ix := all[len(all)-1]
			m = []string{"", line[ix[2]:ix[3]], line[ix[4]:ix[5]], "", strings.TrimRight(line[ix[1]:], ")")}
		}
		if m == nil {
			fmt.Fprintln(os.Stderr, "STOP (no position):", s)
			return
		}
		file := m[1]
		if !filepath.IsAbs(file) {
			file = filepath.Join(ws, file)
		}
		ln, _ := strconv.Atoi(m[2])
		decl, err := remove(file, ln)
		rel, _ := filepath.Rel(ws, file)
		enc.Encode(entry{i, rel, ln, decl, m[4]})
		if err != nil {
			fmt.Fprintln(os.Stderr, "STOP:", err)
			return
		}
	}
}

// remove deletes the import spec, value/type spec or function containing line.
func remove(file string, line int) (string, error) {
	src, err := os.ReadFile(file)
	if strings.Contains(file, "/main/") {
		lines := strings.Split(string(src), "\n")
		if line < 1 || line > len(lines) {
			return "", fmt.Errorf("bad entry line")
		}
		removed := strings.TrimSpace(lines[line-1])
		lines = append(lines[:line-1], lines[line:]...)
		return "entry: " + removed, os.WriteFile(file, []byte(strings.Join(lines, "\n")), 0644)
	}
	if err != nil {
		return "", err
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, src, parser.ParseComments)
	if err != nil {
		return "", err
	}
	in := func(n ast.Node) bool {
		return fset.Position(n.Pos()).Line <= line && line <= fset.Position(n.End()).Line
	}
	cut := func(n ast.Node, name string) (string, error) {
		a, b := fset.Position(n.Pos()).Offset, fset.Position(n.End()).Offset
		out := append(append([]byte{}, src[:a]...), src[b:]...)
		return name, os.WriteFile(file, out, 0644)
	}
	for _, d := range f.Decls {
		if !in(d) {
			continue
		}
		switch d := d.(type) {
		case *ast.FuncDecl:
			name := d.Name.Name
			if d.Recv != nil && len(d.Recv.List) == 1 {
				name = types(d.Recv.List[0].Type) + "." + name
			}
			// Include the doc comment so line numbers stay meaningful.
			var n ast.Node = d
			if d.Doc != nil {
				return cut(span{d.Doc.Pos(), d.End()}, "func "+name)
			}
			return cut(n, "func "+name)
		case *ast.GenDecl:
			if len(d.Specs) == 1 || d.Lparen == token.NoPos {
				return cut(d, d.Tok.String()+" "+specName(d.Specs[0]))
			}
			for _, sp := range d.Specs {
				if in(sp) {
					return cut(sp, d.Tok.String()+" "+specName(sp))
				}
			}
			return cut(d, d.Tok.String()+" (group)")
		}
	}
	return "", fmt.Errorf("no declaration at %s:%d", file, line)
}

type span struct{ p, e token.Pos }

func (s span) Pos() token.Pos { return s.p }
func (s span) End() token.Pos { return s.e }

func types(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.StarExpr:
		return "*" + types(e.X)
	case *ast.Ident:
		return e.Name
	case *ast.IndexExpr:
		return types(e.X)
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
