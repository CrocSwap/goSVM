package compiler

import (
	"bufio"
	"crypto/sha256"
	"embed"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"
)

// Standard-library packages on chain (docs/GO_STDLIB.md). Each supported
// package is a curated overlay of the Go 1.22.0 sources in _stdlib/, compiled
// like a user package: every function in it satisfies the on-chain subset,
// and the program checks, lowers and emits it with its own code. Functions
// whose Go implementation needs reflection, floats, unsafe, assembly or
// packages that are not available on chain are left out of the overlay;
// referring to one is reported by name (stdlibUnavailable).
//
// math/bits stays a compiler intrinsic package (bits.go) and gosvm/solana the
// SDK; neither is in the overlay.

// StdlibVersion is the Go release whose standard library the overlay
// reproduces.
const StdlibVersion = "go1.22.0"

//go:embed _stdlib
var stdlibFS embed.FS

// stdlibRoot is the overlay's directory in stdlibFS.
const stdlibRoot = "_stdlib"

// stdlibSourcePrefix starts the file name of every overlay source, so
// positions in the overlay name the Go version and package.
const stdlibSourcePrefix = "gosvm-stdlib/" + StdlibVersion + "/"

// StdlibPackages are the standard-library packages an on-chain program can
// import, besides math/bits: those with an overlay directory.
var StdlibPackages = func() []string {
	var paths []string
	fs.WalkDir(stdlibFS, stdlibRoot, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".go") {
			dir := strings.TrimPrefix(path.Dir(p), stdlibRoot+"/")
			if len(paths) == 0 || paths[len(paths)-1] != dir {
				paths = append(paths, dir)
			}
		}
		return nil
	})
	sort.Strings(paths)
	return paths
}()

// stdlibPackage reports whether path is an overlay package.
func stdlibPackage(path string) bool {
	i := sort.SearchStrings(StdlibPackages, path)
	return i < len(StdlibPackages) && StdlibPackages[i] == path
}

// stdlibSources returns an overlay package's sources, sorted by name.
func stdlibSources(importPath string) []Source {
	entries, err := stdlibFS.ReadDir(stdlibRoot + "/" + importPath)
	if err != nil {
		return nil
	}
	var sources []Source
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		data, err := stdlibFS.ReadFile(stdlibRoot + "/" + importPath + "/" + e.Name())
		if err != nil {
			panic(err)
		}
		sources = append(sources, Source{Name: stdlibSourcePrefix + importPath + "/" + e.Name(), Data: data})
	}
	return sources
}

// isStdlibSource reports whether a source file name is an overlay file.
func isStdlibSource(name string) bool { return strings.HasPrefix(name, stdlibSourcePrefix) }

// IsStdlibSource reports whether a file name (a token.Position Filename)
// is a standard-library overlay source rather than program code.
func IsStdlibSource(name string) bool { return isStdlibSource(name) }

// IsStdlibPackage reports whether an import path is a standard-library
// overlay package (not math/bits, which is intrinsic).
func IsStdlibPackage(path string) bool { return stdlibPackage(path) }

// StdlibIdentity is the content hash of the overlay packages in paths
// (every file of each, in path and name order) together with the Go version
// they reproduce. Build receipts and dependency keys include it for the
// packages a program reaches; it is empty for no packages.
func StdlibIdentity(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	sorted := append([]string(nil), paths...)
	sort.Strings(sorted)
	h := sha256.New()
	fmt.Fprintf(h, "gosvm-stdlib-v1\x00%s\n", StdlibVersion)
	for _, p := range sorted {
		fmt.Fprintf(h, "package:%s\n", p)
		for _, src := range stdlibSources(p) {
			fmt.Fprintf(h, "%s\x00%x\n", path.Base(src.Name), sha256.Sum256(src.Data))
		}
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// ProgramStdlib lists the overlay packages a program reaches through its
// own packages' imports and the overlay's imports, sorted.
func ProgramStdlib(program *Program) ([]string, error) {
	seen := map[string]bool{}
	var visit func(string)
	visit = func(p string) {
		if seen[p] {
			return
		}
		seen[p] = true
		imports, err := sourceImports(stdlibSources(p))
		if err != nil {
			panic(err)
		}
		for _, im := range imports {
			if stdlibPackage(im.path) {
				visit(im.path)
			}
		}
	}
	for _, unit := range program.Packages {
		imports, err := sourceImports(unit.Sources)
		if err != nil {
			return nil, err
		}
		for _, im := range imports {
			if stdlibPackage(im.path) {
				visit(im.path)
			}
		}
	}
	var paths []string
	for p := range seen {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths, nil
}

// StdlibImportable lists the overlay packages user code can import: all
// but the overlay's internal packages.
func StdlibImportable() []string {
	var paths []string
	for _, p := range StdlibPackages {
		if !strings.HasPrefix(p, "internal/") {
			paths = append(paths, p)
		}
	}
	return paths
}

// stdlibImportRule is the message for a standard-library import that is not
// available on chain.
func stdlibImportRule(path string) string {
	return fmt.Sprintf("unsupported import %q; standard-library packages on chain: %s and math/bits (docs/GO_STDLIB.md)", path, strings.Join(StdlibImportable(), ", "))
}

// stdlibAPI is Go 1.22.0's exported API of each overlay package
// (_stdlib/api.txt, generated from $GOROOT/api by TestStdlibAPI): package
// path, then a name (Name for package-level declarations, Type.Method for
// methods), then the kind.
var stdlibAPI = func() map[string]map[string]string {
	data, err := stdlibFS.ReadFile(stdlibRoot + "/api.txt")
	if err != nil {
		panic(err)
	}
	api := map[string]map[string]string{}
	s := bufio.NewScanner(strings.NewReader(string(data)))
	for s.Scan() {
		line := s.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) != 3 {
			panic("malformed stdlib API line " + line)
		}
		if api[f[0]] == nil {
			api[f[0]] = map[string]string{}
		}
		api[f[0]][f[1]] = f[2]
	}
	return api
}()

// stdlibReasons explains why a Go 1.22 symbol is not in an overlay; the
// default is that goSVM has not added it.
var stdlibReasons = map[string]string{
	"encoding/binary.Read":   "it uses reflection",
	"encoding/binary.Write":  "it uses reflection",
	"encoding/binary.Size":   "it uses reflection",
	"encoding/binary.Append": "it uses reflection",
	"encoding/binary.Encode": "it uses reflection",
	"encoding/binary.Decode": "it uses reflection",
	// ReadUvarint and ReadVarint read from an io.ByteReader.
	"encoding/binary.ReadUvarint": "it reads from an io.ByteReader; package io is not available on chain",
	"encoding/binary.ReadVarint":  "it reads from an io.ByteReader; package io is not available on chain",
}

// floatSymbol reports whether a Go 1.22 symbol of an overlay package is
// about floating-point or complex numbers.
func floatSymbol(pkg, name string) bool {
	if pkg == "math" {
		return true // every math symbol except the integer limits
	}
	if pkg == "strconv" {
		switch name {
		case "ParseFloat", "FormatFloat", "AppendFloat", "ParseComplex", "FormatComplex":
			return true
		}
	}
	return false
}

// stdlibUnavailable explains a reference to a Go 1.22 symbol of an overlay
// package that the overlay leaves out.
func stdlibUnavailable(pkg, name string) string {
	reason := stdlibReasons[pkg+"."+name]
	switch {
	case floatSymbol(pkg, name):
		reason = "floats are not supported on chain"
	case reason == "":
		reason = "goSVM's on-chain " + pkg + " does not include it"
	}
	return fmt.Sprintf("%s.%s is not available on chain: %s (docs/GO_STDLIB.md)", pkg, name, reason)
}

var (
	undefinedQualified = regexp.MustCompile(`^undefined: ([A-Za-z_][A-Za-z0-9_]*)\.([A-Za-z_][A-Za-z0-9_]*)$`)
	undefinedMethod    = regexp.MustCompile(`^[^ ]+ undefined \(type \*?([A-Za-z_][A-Za-z0-9_./]*)\.([A-Za-z_][A-Za-z0-9_]*) has no field or method ([A-Za-z_][A-Za-z0-9_]*)`)
)

// stdlibTypeError rewrites a type-checking error that refers to a Go 1.22
// symbol an overlay package leaves out into a diagnostic naming it, at the
// use. Other errors are returned unchanged.
func stdlibTypeError(err error, files []*ast.File, fset *token.FileSet) error {
	te, ok := err.(types.Error)
	if !ok {
		return err
	}
	if m := undefinedQualified.FindStringSubmatch(te.Msg); m != nil {
		pkg := importedAs(files, fset, te.Pos, m[1])
		if pkg != "" && stdlibPackage(pkg) && stdlibAPI[pkg][m[2]] != "" {
			return fmt.Errorf("%s: %s", fset.Position(te.Pos), stdlibUnavailable(pkg, m[2]))
		}
	}
	if m := undefinedMethod.FindStringSubmatch(te.Msg); m != nil {
		// m[1] is the package path as go/types qualifies it.
		if stdlibPackage(m[1]) && stdlibAPI[m[1]][m[2]+"."+m[3]] != "" {
			return fmt.Errorf("%s: %s", fset.Position(te.Pos), stdlibUnavailable(m[1], m[2]+"."+m[3]))
		}
	}
	if call := errorsAsCall(files, fset, te.Pos); call != "" {
		return fmt.Errorf("%s: errors.As on chain needs a target of static pointer type, as in errors.As(err, &target): goSVM has no reflection to find the type of an any target (docs/GO_STDLIB.md#errors); %s", fset.Position(te.Pos), te.Msg)
	}
	return err
}

// importedAs is the import path the file containing pos imports under name.
func importedAs(files []*ast.File, fset *token.FileSet, pos token.Pos, name string) string {
	for _, f := range files {
		if f.FileStart > pos || pos > f.FileEnd {
			continue
		}
		for _, im := range f.Imports {
			p := strings.Trim(im.Path.Value, `"`)
			local := path.Base(p)
			if im.Name != nil {
				local = im.Name.Name
			}
			if local == name {
				return p
			}
		}
	}
	return ""
}

// errorsAsCall reports the selector of an errors.As call whose arguments
// contain pos, if any ("" otherwise).
func errorsAsCall(files []*ast.File, fset *token.FileSet, pos token.Pos) string {
	found := ""
	for _, f := range files {
		if f.FileStart > pos || pos > f.FileEnd {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || found != "" || pos < call.Pos() || pos > call.End() {
				return found == ""
			}
			fun := call.Fun
			if ix, ok := fun.(*ast.IndexExpr); ok {
				fun = ix.X
			}
			if sel, ok := fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "As" {
				if id, ok := sel.X.(*ast.Ident); ok && importedAs(files, fset, pos, id.Name) == "errors" {
					found = id.Name + ".As"
				}
			}
			return true
		})
	}
	return found
}

// Intrinsics: a few overlay functions have a Go body that type-checks and
// passes the subset rules, but the emitter replaces it with code Go cannot
// express without reflection or package unsafe. They are marked by a
// "//gosvm:intrinsic name" line in their doc comment; only overlay files can
// declare them.
const (
	// intrinsicComparable is func(x any) bool: whether x's dynamic type is
	// comparable (reflectlite.TypeOf(x).Comparable()).
	intrinsicComparable = "comparable"
	// intrinsicUnsafeString is func(b []byte) string: a string sharing b's
	// bytes (unsafe.String(unsafe.SliceData(b), len(b))).
	intrinsicUnsafeString = "unsafeString"
	// intrinsicNoescape is func(p *T) *T: p itself. Its Go body returns
	// nil, so Check's borrow analysis treats the result as referring to no
	// storage (the runtime's noescape); the overlay only compares it.
	intrinsicNoescape = "noescape"
)

// stdlibExcepts maps every dynamic method call in the body of an overlay
// function marked "//gosvm:dispatch-except K1,K2" to the dynamic type keys
// it excludes (sorted): the function's own code guarantees that the
// receiver never holds those types, and excluding them keeps the call
// graph acyclic where Go recurses through an interface (for example
// NumError.Error calling the Error method of its cause, which may be another
// *NumError: strconv follows those causes in a loop instead).
func stdlibExcepts(fset *token.FileSet, files []*ast.File) map[*ast.CallExpr][]string {
	excepts := map[*ast.CallExpr][]string{}
	for _, file := range files {
		for _, d := range file.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Doc == nil || fn.Body == nil || !isStdlibSource(fset.Position(fn.Pos()).Filename) {
				continue
			}
			for _, c := range fn.Doc.List {
				list, ok := strings.CutPrefix(c.Text, "//gosvm:dispatch-except ")
				if !ok {
					continue
				}
				keys := strings.Split(strings.TrimSpace(list), ",")
				sort.Strings(keys)
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					if call, ok := n.(*ast.CallExpr); ok {
						excepts[call] = keys
					}
					return true
				})
			}
		}
	}
	return excepts
}

// stdlibIntrinsic is the intrinsic an overlay function declaration is
// marked as, or "".
func stdlibIntrinsic(fset *token.FileSet, fn *ast.FuncDecl) string {
	if fn.Doc == nil || !isStdlibSource(fset.Position(fn.Pos()).Filename) {
		return ""
	}
	for _, c := range fn.Doc.List {
		if name, ok := strings.CutPrefix(c.Text, "//gosvm:intrinsic "); ok {
			switch name = strings.TrimSpace(name); name {
			case intrinsicComparable, intrinsicUnsafeString, intrinsicNoescape:
				return name
			}
			panic(fmt.Sprintf("unknown intrinsic %q", name))
		}
	}
	return ""
}

// emitIntrinsicBody emits the C body of an intrinsic overlay function.
func (g *generator) emitIntrinsicBody(fn *ast.FuncDecl, name string) {
	sig := g.info.Defs[fn.Name].Type().(*types.Signature)
	param := g.name(sig.Params().At(0))
	switch name {
	case intrinsicComparable:
		var tests []string
		for _, d := range g.ifaces.dynamic {
			if !d.Comparable {
				tests = append(tests, fmt.Sprintf("%s.type == %dULL", param, d.ID))
			}
		}
		if len(tests) == 0 {
			tests = []string{"0"}
		}
		g.line("{ /* goSVM intrinsic: dynamic type is comparable */")
		g.line("return !(%s);\n}", strings.Join(tests, " || "))
	case intrinsicUnsafeString:
		g.line("{ /* goSVM intrinsic: string sharing the slice's bytes */")
		g.line("return (gosvm_string){%s.len ? %s.ptr : 0, %s.len};\n}", param, param, param)
	case intrinsicNoescape:
		g.line("{ /* goSVM intrinsic: noescape */")
		g.line("return %s;\n}", param)
	default:
		g.unchecked(fn)
	}
}
