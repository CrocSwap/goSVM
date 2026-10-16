package compiler

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Opt-in reachability trimming (docs/GO_PACKAGES.md#trimming).
//
// By default every declaration of every package in the on-chain import graph
// must satisfy the subset. A program can opt in, per import-path pattern, to
// checking and compiling only the declarations of matching packages that
// Process reaches: Program.Trim. A trimmed package is still type-checked as
// a whole (imports only its dropped declarations use are type information:
// standard-library packages from GOROOT, module packages from the module
// cache, never compiled), then reachability from Process, the package
// initializers that are kept and every init function selects the
// declarations Check sees; every other declaration is dropped from the
// checked program and reported (TrimReport).

// TrimReport lists what trimming removed from a program; nil without
// Program.Trim. It is part of TypedProgram (and so of CheckedProgram).
type TrimReport struct {
	// Patterns are Program.Trim.
	Patterns []string `json:"patterns"`
	// Packages are the trimmed packages of the program, sorted by path,
	// including those from which nothing was dropped.
	Packages []TrimmedPackage `json:"packages"`
}

// TrimmedPackage is what trimming removed from one package.
type TrimmedPackage struct {
	Path string `json:"path"`
	// Dropped are the declarations removed, in file and source order. A
	// var specification is one entry (its names joined by ", ").
	Dropped []TrimmedDecl `json:"dropped"`
	// TypeOnlyImports are the imports only dropped declarations use: they
	// are type information, not compiled on chain, and a module package
	// among them is not initialized (Go would initialize it).
	TypeOnlyImports []string `json:"type_only_imports,omitempty"`
}

// TrimmedDecl is one removed declaration.
type TrimmedDecl struct {
	// Name is F, T.M (a method), T, or the declared names of a var or
	// const specification ("_" for a blank variable).
	Name string `json:"name"`
	// Kind is "func", "method", "type", "var" or "const".
	Kind string `json:"kind"`
	// Position is file:line of the declaration (the file name relative to
	// its package directory).
	Position string `json:"position"`
	// Initializer reports a dropped var specification with an
	// initializer: Go would run it at package initialization.
	Initializer bool `json:"initializer,omitempty"`
}

// DroppedInitializers counts the dropped var specifications with
// initializers.
func (p TrimmedPackage) DroppedInitializers() int {
	n := 0
	for _, d := range p.Dropped {
		if d.Initializer {
			n++
		}
	}
	return n
}

// Summary is one line per trimmed package that dropped something: the
// counts and the type-only imports.
func (r *TrimReport) Summary() []string {
	if r == nil {
		return nil
	}
	var lines []string
	for _, p := range r.Packages {
		if len(p.Dropped) == 0 && len(p.TypeOnlyImports) == 0 {
			continue
		}
		line := fmt.Sprintf("trimmed %s: dropped %d declarations (%d initializers)", p.Path, len(p.Dropped), p.DroppedInitializers())
		if len(p.TypeOnlyImports) > 0 {
			line += fmt.Sprintf("; type-only imports, not compiled or initialized: %s", strings.Join(p.TypeOnlyImports, ", "))
		}
		lines = append(lines, line)
	}
	return lines
}

// EmitC lowers a checked program to C, as CompileProgram does after Check;
// callers that need the CheckedProgram too (its TrimReport) use Check and
// EmitC.
func EmitC(program *CheckedProgram) ([]byte, error) { return emitC(program) }

// MarshalTrimReport is the report's canonical JSON (indented, trailing
// newline).
func MarshalTrimReport(r *TrimReport) ([]byte, error) {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// ValidateTrim checks trim patterns: an import path, or an import path
// followed by "/..." (every package under it, including itself).
func ValidateTrim(patterns []string) error {
	for _, p := range patterns {
		base := strings.TrimSuffix(p, "/...")
		if base == "" || strings.Contains(base, "...") || strings.ContainsAny(base, " \t\\") || strings.HasPrefix(base, "/") || strings.HasSuffix(base, "/") {
			return fmt.Errorf("trim pattern %q: want an import path or an import path followed by /...", p)
		}
	}
	return nil
}

// matchTrim reports whether a pattern names path.
func matchTrim(patterns []string, path string) bool {
	for _, p := range patterns {
		if base, ok := strings.CutSuffix(p, "/..."); ok {
			if path == base || strings.HasPrefix(path, base+"/") {
				return true
			}
		} else if p == path {
			return true
		}
	}
	return false
}

// trimHintText is appended to whole-package rejections in dependency
// packages.
const trimHintText = `; to check only the declarations Process reaches in package %q, opt in with "trim" in gosvm.json or -trim (docs/GO_PACKAGES.md#trimming)`

// trimHint is the opt-in hint for a rejection in package path: empty for
// the entry package, the standard-library overlay and trimmed packages.
func trimHint(p *Program, path string) string {
	if path == "" || path == p.Entry || stdlibPackage(path) || path == "gosvm/solana" || matchTrim(p.Trim, path) {
		return ""
	}
	// Only module packages (import paths beginning with a domain name):
	// programs given as in-memory package sets name theirs otherwise.
	if first, _, _ := strings.Cut(path, "/"); !strings.Contains(first, ".") {
		return ""
	}
	return fmt.Sprintf(trimHintText, path)
}

// errorPosition matches the position of an error and of each error nested
// in it ("could not import p (file:line:col: ...)").
var errorPosition = regexp.MustCompile(`(?:^|\()([^()]+?):\d+:\d+: `)

// withTrimHint appends the opt-in hint to a Check error located (innermost)
// in a file of a dependency package.
func withTrimHint(err error, program *Program) error {
	msg := err.Error()
	all := errorPosition.FindAllStringSubmatch(msg, -1)
	if all == nil {
		return err
	}
	file := all[len(all)-1][1]
	owner := ""
	for _, units := range [][]PackageSources{program.Packages, program.TypeOnly} {
		for _, unit := range units {
			for _, src := range unit.Sources {
				if src.Name == file {
					owner = unit.Path
				}
			}
		}
	}
	hint := trimHint(program, owner)
	if hint == "" {
		return err
	}
	if _, ok := err.(compileError); ok {
		return compileError(msg + hint)
	}
	return errors.New(msg + hint)
}

// Resolution.

// listedDeps is the subset of go list -json used for type-only packages.
type listedDeps struct {
	ImportPath, Dir, Name string
	Standard              bool
	GoFiles               []string
	Module                *listedModule
	Error                 *struct{ Err string }
	DepsErrors            []*struct{ Err string }
}

// typeOnlyEnv selects the files go list and go/build pick for type
// information: linux/amd64 without cgo, whatever the host.
var typeOnlyEnv = map[string]string{"CGO_ENABLED": "0", "GOOS": "linux", "GOARCH": "amd64"}

// resolveTypeOnly records, in Program.TypeOnly, the module packages that only
// trimmed packages import (pending) and their module dependencies outside
// the on-chain graph (seen), with their module metadata.
func resolveTypeOnly(p *Program, r *moduleResolver, pending []sourceImport, from map[string]string, seen map[string]bool, metadata map[string]bool) error {
	args := []string{"list", "-e", "-deps", "-json", "-mod=readonly", "-modfile=" + r.modFile, "--"}
	for _, im := range pending {
		args = append(args, im.path)
	}
	cmd := r.command(typeOnlyEnv, args...)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: trimmed package %q: resolving its imports for type information: offline go list: %s", pending[0].position, from[pending[0].path], strings.TrimSpace(stderr.String()))
	}
	positions := map[string]sourceImport{}
	for _, im := range pending {
		positions[im.path] = im
	}
	dec := json.NewDecoder(&out)
	for dec.More() {
		var pkg listedDeps
		if err := dec.Decode(&pkg); err != nil {
			return fmt.Errorf("go list metadata: %w", err)
		}
		if pkg.Standard || seen[pkg.ImportPath] {
			continue
		}
		if pkg.Error != nil || pkg.Dir == "" || pkg.Module == nil {
			reason := "not a module package"
			if pkg.Error != nil {
				reason = pkg.Error.Err
			}
			where := "a trimmed package"
			if im, ok := positions[pkg.ImportPath]; ok {
				where = fmt.Sprintf("%s: trimmed package %q", im.position, from[im.path])
			}
			return fmt.Errorf("%s imports %q, which must be in the module cache for type information even when only dropped declarations use it (docs/GO_PACKAGES.md#trimming): %s", where, pkg.ImportPath, reason)
		}
		seen[pkg.ImportPath] = true
		var sources []Source
		unsupported := ""
		if s, err := ReadSources(pkg.Dir); err != nil {
			unsupported = err.Error()
		} else {
			sources = s
			for _, src := range s {
				if bytes.Contains(src.Data, []byte(`import "C"`)) {
					unsupported = filepath.Base(src.Name) + `: cgo (import "C") unsupported in on-chain packages`
				}
			}
		}
		if unsupported != "" {
			sources = nil
			for _, name := range pkg.GoFiles {
				data, err := os.ReadFile(filepath.Join(pkg.Dir, name))
				if err != nil {
					return err
				}
				sources = append(sources, Source{filepath.Join(pkg.Dir, name), data})
			}
			if p.TypeOnlyUnsupported == nil {
				p.TypeOnlyUnsupported = map[string]string{}
			}
			p.TypeOnlyUnsupported[pkg.ImportPath] = unsupported
		}
		p.TypeOnly = append(p.TypeOnly, PackageSources{Path: pkg.ImportPath, Sources: sources})
		for _, module := range []*listedModule{pkg.Module, replacement(pkg.Module)} {
			if module == nil || module.GoMod == "" || module.GoMod == r.modFile {
				continue
			}
			for _, name := range []string{module.GoMod, filepath.Join(filepath.Dir(module.GoMod), "go.sum")} {
				if metadata[name] {
					continue
				}
				b, e := os.ReadFile(name)
				if os.IsNotExist(e) {
					continue
				}
				if e != nil {
					return e
				}
				metadata[name] = true
				p.Metadata = append(p.Metadata, Source{name, b})
			}
		}
	}
	sort.Slice(p.TypeOnly, func(i, j int) bool { return p.TypeOnly[i].Path < p.TypeOnly[j].Path })
	return nil
}

// Type information for imports compiled nowhere.

// realStd caches the real standard library (GOROOT sources for
// linux/amd64 without cgo), type-checked without function bodies: type
// information for the imports of trimmed and type-only packages. Errors are
// ignored; nothing of it is compiled.
var realStd struct {
	sync.Mutex
	fset *token.FileSet
	pkgs map[string]*types.Package
}

// importRealStd is the real standard-library package path; the caller holds
// no lock (it is taken here, recursively safe through importRealStdLocked).
func importRealStd(path string) (*types.Package, error) {
	realStd.Lock()
	defer realStd.Unlock()
	return importRealStdLocked(path)
}

type realStdLockedImporter struct{}

func (realStdLockedImporter) Import(path string) (*types.Package, error) {
	return importRealStdLocked(path)
}

func importRealStdLocked(path string) (*types.Package, error) {
	if path == "unsafe" {
		return types.Unsafe, nil
	}
	if realStd.pkgs == nil {
		realStd.pkgs, realStd.fset = map[string]*types.Package{}, token.NewFileSet()
	}
	if pkg, ok := realStd.pkgs[path]; ok {
		if pkg == nil {
			return nil, fmt.Errorf("import cycle involving %q", path)
		}
		return pkg, nil
	}
	root := goroot()
	if root == "" {
		return nil, fmt.Errorf("no GOROOT for the type information of %q", path)
	}
	ctxt := build.Default
	ctxt.GOROOT, ctxt.GOOS, ctxt.GOARCH, ctxt.CgoEnabled = root, "linux", "amd64", false
	dir := filepath.Join(root, "src", filepath.FromSlash(path))
	if _, err := os.Stat(dir); err != nil {
		dir = filepath.Join(root, "src", "vendor", filepath.FromSlash(path))
	}
	bp, err := ctxt.ImportDir(dir, 0)
	if err != nil {
		return nil, fmt.Errorf("standard-library package %q: %w", path, err)
	}
	realStd.pkgs[path] = nil
	var files []*ast.File
	for _, name := range bp.GoFiles {
		file, err := parser.ParseFile(realStd.fset, filepath.Join(dir, name), nil, parser.SkipObjectResolution)
		if err != nil {
			delete(realStd.pkgs, path)
			return nil, err
		}
		files = append(files, file)
	}
	conf := types.Config{Importer: realStdLockedImporter{}, IgnoreFuncBodies: true, FakeImportC: true,
		Sizes: &types.StdSizes{WordSize: 8, MaxAlign: 8}, Error: func(error) {}}
	pkg, _ := conf.Check(path, realStd.fset, files, nil)
	if pkg == nil {
		delete(realStd.pkgs, path)
		return nil, fmt.Errorf("type-checking standard-library package %q failed", path)
	}
	realStd.pkgs[path] = pkg
	return pkg, nil
}

// trimLoader is the trimming state of one load of the program.
type trimLoader struct {
	p        *programImporter
	program  *Program
	patterns []string
	// typeOnlySrc are Program.TypeOnly by path; promoted are those Check
	// compiles on chain (loaded as units).
	typeOnlySrc map[string]PackageSources
	promoted    map[string]bool
	// fatal are the trimmed packages whose check fails, with the error.
	fatal map[string]error
	// restart lists promotions requested while loading: the load is
	// repeated with them on chain.
	restart []string
	// typeOnly are the type-only module packages checked (nil while one is
	// being checked).
	typeOnly map[string]*types.Package
	// used maps each trimmed package to the imports it took type-only.
	used map[string]map[string]bool
	// errors are each trimmed package's type errors, decided by
	// reachability; files its parsed files; order the trimmed packages in
	// check order.
	errors map[string][]types.Error
	files  map[string][]*ast.File
	order  []string
}

// errRestart stops a load that needs another package on chain.
var errRestart = errors.New("internal: trimming restarts the load")

// trimState is what successive loads of a trimmed program learn: the
// Program.TypeOnly packages to load on chain (promoted) and the trimmed
// packages whose check fails, with the error (fatal).
type trimState struct {
	promoted map[string]bool
	fatal    map[string]error
}

// trimFatal is a trimmed package's type error in a reached declaration.
type trimFatal struct {
	path string
	err  error
}

func newTrimLoader(p *programImporter, program *Program, state *trimState) *trimLoader {
	t := &trimLoader{p: p, program: program, patterns: program.Trim, typeOnlySrc: map[string]PackageSources{}, promoted: state.promoted, fatal: state.fatal,
		typeOnly: map[string]*types.Package{}, used: map[string]map[string]bool{}, errors: map[string][]types.Error{}, files: map[string][]*ast.File{}}
	for _, unit := range program.TypeOnly {
		t.typeOnlySrc[unit.Path] = unit
	}
	return t
}

// trimmed reports whether package path is trimmed: it matches a pattern
// and is not part of the standard-library overlay or the SDK. The entry
// package is trimmed only when a pattern names it exactly.
func (t *trimLoader) trimmed(path string) bool {
	if stdlibPackage(path) || path == "gosvm/solana" || path == bitsPath {
		return false
	}
	if path == t.program.Entry {
		for _, p := range t.patterns {
			if p == path {
				return true
			}
		}
		return false
	}
	return matchTrim(t.patterns, path)
}

// promoteImport is called for an on-chain import of a package not loaded
// on chain: when only trimmed packages had imported it, the load restarts
// with it on chain.
func (t *trimLoader) promoteImport(path string) error {
	if _, ok := t.typeOnlySrc[path]; ok && !t.promoted[path] {
		t.restart = append(t.restart, path)
		return errRestart
	}
	return nil
}

// trimImporter imports for a trimmed package: on-chain packages as usual,
// everything else as type information.
type trimImporter struct {
	t    *trimLoader
	from string
}

func (i trimImporter) Import(path string) (*types.Package, error) {
	p := i.t.p
	if path == "gosvm/solana" || path == bitsPath || stdlibPackage(path) {
		return p.Import(path)
	}
	if _, ok := p.units[path]; ok {
		return p.Import(path)
	}
	if i.t.used[i.from] == nil {
		i.t.used[i.from] = map[string]bool{}
	}
	i.t.used[i.from][path] = true
	return i.t.typeOnlyImport(i.from, path)
}

// typeOnlyImport is the type information of a package compiled nowhere.
func (t *trimLoader) typeOnlyImport(from, path string) (*types.Package, error) {
	if path == "unsafe" {
		return types.Unsafe, nil
	}
	if _, ok := t.typeOnlySrc[path]; ok {
		return t.typeOnlyModule(path)
	}
	if standardPackage(path) {
		return importRealStd(path)
	}
	return nil, fmt.Errorf("package %q imports %q, which must be in the module cache for type information even when only dropped declarations use it (docs/GO_PACKAGES.md#trimming)", from, path)
}

// typeOnlyDeps imports for a type-only module package.
type typeOnlyDeps struct{ t *trimLoader }

func (d typeOnlyDeps) Import(path string) (*types.Package, error) {
	if _, ok := d.t.p.units[path]; ok {
		return d.t.p.Import(path)
	}
	return d.t.typeOnlyImport("", path)
}

// typeOnlyModule type-checks a type-only module package without function
// bodies, ignoring errors: it is never compiled.
func (t *trimLoader) typeOnlyModule(path string) (*types.Package, error) {
	if pkg, ok := t.typeOnly[path]; ok {
		if pkg == nil {
			return nil, fmt.Errorf("import cycle involving %q", path)
		}
		return pkg, nil
	}
	t.typeOnly[path] = nil
	var files []*ast.File
	for _, src := range t.typeOnlySrc[path].Sources {
		file, err := parser.ParseFile(t.p.f.fset, src.Name, src.Data, parser.SkipObjectResolution)
		if err != nil {
			delete(t.typeOnly, path)
			return nil, err
		}
		files = append(files, file)
	}
	conf := types.Config{Importer: typeOnlyDeps{t}, IgnoreFuncBodies: true, FakeImportC: true, Context: t.p.ctxt,
		Sizes: &types.StdSizes{WordSize: 8, MaxAlign: 8}, Error: func(error) {}}
	pkg, _ := conf.Check(path, t.p.f.fset, files, nil)
	if len(t.restart) > 0 {
		return nil, errRestart
	}
	if pkg == nil {
		delete(t.typeOnly, path)
		return nil, fmt.Errorf("type-checking %q failed", path)
	}
	t.typeOnly[path] = pkg
	return pkg, nil
}

// checkTrimmed type-checks a trimmed package, keeping its type errors for
// reachability to decide.
func (t *trimLoader) checkTrimmed(conf *types.Config, path string, files []*ast.File) (*types.Package, error) {
	conf.Importer = trimImporter{t, path}
	var errs []types.Error
	var hard error
	conf.Error = func(err error) {
		if te, ok := err.(types.Error); ok {
			errs = append(errs, te)
		} else if hard == nil {
			hard = err
		}
	}
	pkg, _ := conf.Check(path, t.p.f.fset, files, t.p.f.info)
	if len(t.restart) > 0 {
		return nil, errRestart
	}
	if hard != nil {
		return nil, hard
	}
	if err := t.fatal[path]; err != nil {
		return nil, err
	}
	if pkg == nil {
		return nil, fmt.Errorf("type-checking %q failed", path)
	}
	if path != t.p.entryPath && pkg.Name() == "main" {
		return nil, fmt.Errorf("%s: package %q is a program, not importable", t.p.f.fset.Position(files[0].Package), path)
	}
	t.errors[path], t.files[path] = errs, files
	t.order = append(t.order, path)
	return pkg, nil
}

// checkPromoted applies the on-chain import rules to a package that only
// trimmed packages imported, before it is loaded on chain: what resolution
// checks for every other package.
func checkPromoted(program *Program, path string) error {
	var unit *PackageSources
	for i := range program.TypeOnly {
		if program.TypeOnly[i].Path == path {
			unit = &program.TypeOnly[i]
		}
	}
	if unit == nil {
		return fmt.Errorf("internal error: no sources for %q", path)
	}
	hint := trimHint(program, path)
	if reason := program.TypeOnlyUnsupported[path]; reason != "" {
		return fmt.Errorf("package %q, which reached declarations of a trimmed package use: %s%s", path, reason, hint)
	}
	if matchTrim(program.Trim, path) {
		return nil
	}
	imports, err := sourceImports(unit.Sources)
	if err != nil {
		return err
	}
	for _, im := range imports {
		if !compilerImport(im.path) && standardPackage(im.path) {
			return fmt.Errorf("%s: %s%s", im.position, stdlibImportRule(im.path), hint)
		}
	}
	return nil
}

// Reachability.

// trimDecl is one top-level declaration of a trimmed package: a function
// or method, or one type, var or const specification.
type trimDecl struct {
	pkg  string
	file *ast.File
	decl ast.Decl
	spec ast.Spec // nil for a function
	kept bool
}

func (d *trimDecl) node() ast.Node {
	if d.spec != nil {
		return d.spec
	}
	return d.decl
}

// reacher computes the reachable declarations of trimmed packages.
type reacher struct {
	info    *types.Info
	index   map[types.Object]*trimDecl
	queue   []ast.Node
	seen    map[types.Type]bool
	named   []*types.Named                // reached named types of trimmed packages (origins)
	methods map[string][]*types.Signature // methods of reached interfaces, by name
}

func (r *reacher) keep(d *trimDecl) {
	if d == nil || d.kept {
		return
	}
	d.kept = true
	r.queue = append(r.queue, d.node())
}

func (r *reacher) object(o types.Object) {
	switch x := o.(type) {
	case *types.Func:
		o = x.Origin()
	case *types.Var:
		if x.IsField() {
			return
		}
		o = x.Origin()
	case *types.TypeName:
		r.typ(x.Type())
	}
	r.keep(r.index[o])
}

// typ reaches the declarations a type names, and the methods of named
// types that reached interfaces may call.
func (r *reacher) typ(t types.Type) {
	if t == nil || r.seen[t] {
		return
	}
	r.seen[t] = true
	switch t := t.(type) {
	case *types.Named:
		r.keep(r.index[t.Obj()])
		for i := 0; i < t.TypeArgs().Len(); i++ {
			r.typ(t.TypeArgs().At(i))
		}
		r.typ(t.Underlying())
		origin := t.Origin()
		if origin != t {
			r.typ(origin)
			return
		}
		if origin.NumMethods() > 0 && r.index[origin.Obj()] != nil {
			r.named = append(r.named, origin)
			for i := 0; i < origin.NumMethods(); i++ {
				r.method(origin, origin.Method(i))
			}
		}
	case *types.Pointer:
		r.typ(t.Elem())
	case *types.Slice:
		r.typ(t.Elem())
	case *types.Array:
		r.typ(t.Elem())
	case *types.Chan:
		r.typ(t.Elem())
	case *types.Map:
		r.typ(t.Key())
		r.typ(t.Elem())
	case *types.Struct:
		for i := 0; i < t.NumFields(); i++ {
			r.typ(t.Field(i).Type())
		}
	case *types.Tuple:
		for i := 0; i < t.Len(); i++ {
			r.typ(t.At(i).Type())
		}
	case *types.Signature:
		r.typ(t.Params())
		r.typ(t.Results())
		for i := 0; i < t.TypeParams().Len(); i++ {
			r.typ(t.TypeParams().At(i))
		}
	case *types.TypeParam:
		r.typ(t.Constraint())
	case *types.Interface:
		for i := 0; i < t.NumEmbeddeds(); i++ {
			r.typ(t.EmbeddedType(i))
		}
		for i := 0; i < t.NumMethods(); i++ {
			m := t.Method(i)
			sig := m.Type().(*types.Signature)
			r.typ(sig)
			r.interfaceMethod(m.Name(), sig)
		}
	case *types.Union:
		for i := 0; i < t.Len(); i++ {
			r.typ(t.Term(i).Type())
		}
	}
}

// interfaceMethod records a method of a reached interface: methods of
// reached named types with its name and signature may be called through it.
func (r *reacher) interfaceMethod(name string, sig *types.Signature) {
	for _, s := range r.methods[name] {
		if types.Identical(s, sig) {
			return
		}
	}
	r.methods[name] = append(r.methods[name], sig)
	for _, n := range r.named {
		for i := 0; i < n.NumMethods(); i++ {
			if m := n.Method(i); m.Name() == name {
				r.method(n, m)
			}
		}
	}
}

// method keeps method m of reached named type n when a reached interface
// has a method of its name and signature (any signature for a generic n,
// whose method signatures mention its type parameters).
func (r *reacher) method(n *types.Named, m *types.Func) {
	sigs := r.methods[m.Name()]
	if len(sigs) == 0 {
		return
	}
	if n.TypeParams().Len() > 0 {
		r.object(m)
		return
	}
	for _, s := range sigs {
		if types.Identical(s, m.Type()) {
			r.object(m)
			return
		}
	}
}

// visit reaches everything node refers to.
func (r *reacher) visit(node ast.Node) {
	ast.Inspect(node, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.Ident:
			if o := r.info.Uses[n]; o != nil {
				r.object(o)
			}
			if o := r.info.Defs[n]; o != nil {
				r.typ(o.Type())
			}
			if inst, ok := r.info.Instances[n]; ok {
				r.typ(inst.Type)
			}
		case *ast.SelectorExpr:
			if sel := r.info.Selections[n]; sel != nil {
				r.object(sel.Obj())
				r.typ(sel.Recv())
			}
		}
		if e, ok := n.(ast.Expr); ok {
			if tv, ok := r.info.Types[e]; ok {
				r.typ(tv.Type)
			}
		}
		return true
	})
}

// finish computes reachability over a completed load, reports the trimmed
// packages' fatal type errors and reached uses of unsupported imports, and
// returns the type-only module packages reached declarations use (the load
// must then be repeated with them on chain). With none, it removes the
// unreachable declarations from the loaded files and returns the report.
func (t *trimLoader) finish() (promote []string, report *TrimReport, fatal *trimFatal, err error) {
	p := t.p
	info := p.f.info
	r := &reacher{info: info, index: map[types.Object]*trimDecl{}, seen: map[types.Type]bool{}, methods: map[string][]*types.Signature{}}
	var decls []*trimDecl
	trimmedFile := map[*ast.File]string{}
	for _, path := range t.order {
		for _, file := range t.files[path] {
			trimmedFile[file] = path
			for _, d := range file.Decls {
				switch d := d.(type) {
				case *ast.FuncDecl:
					td := &trimDecl{pkg: path, file: file, decl: d}
					decls = append(decls, td)
					if o := info.Defs[d.Name]; o != nil {
						r.index[o] = td
					}
				case *ast.GenDecl:
					if d.Tok == token.IMPORT {
						continue
					}
					for _, spec := range d.Specs {
						td := &trimDecl{pkg: path, file: file, decl: d, spec: spec}
						decls = append(decls, td)
						switch s := spec.(type) {
						case *ast.TypeSpec:
							if o := info.Defs[s.Name]; o != nil {
								r.index[o] = td
							}
						case *ast.ValueSpec:
							for _, name := range s.Names {
								if o := info.Defs[name]; o != nil {
									r.index[o] = td
								}
							}
						}
					}
				}
			}
		}
	}
	// Roots: every declaration of the other on-chain packages, Process
	// when the entry package is trimmed, and every init function.
	for _, file := range p.files {
		if _, trimmed := trimmedFile[file]; !trimmed {
			r.queue = append(r.queue, file)
		}
	}
	if t.trimmed(p.entryPath) {
		if pkg := p.checked[p.entryPath]; pkg != nil {
			if process := pkg.Scope().Lookup("Process"); process != nil {
				r.object(process)
			}
		}
	}
	for _, d := range decls {
		if fn, ok := d.decl.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Name.Name == "init" {
			r.keep(d)
		}
	}
	for len(r.queue) > 0 {
		n := r.queue[0]
		r.queue = r.queue[1:]
		r.visit(n)
	}

	fset := p.f.fset
	within := func(path string, pos token.Pos) (inside bool, kept bool) {
		for _, d := range decls {
			if d.pkg != path {
				continue
			}
			n := d.node()
			if d.spec != nil {
				// A specification's errors may be reported at its
				// declaration's keyword.
				if gen := d.decl.(*ast.GenDecl); len(gen.Specs) == 1 {
					n = gen
				}
			}
			if n.Pos() <= pos && pos < n.End() {
				return true, d.kept
			}
		}
		return false, false
	}
	// Type errors: fatal in reached declarations and outside declarations
	// (imports); ignored in dropped ones, which are never compiled (they
	// come from the overlay's subset of the standard library).
	for _, path := range t.order {
		for _, te := range t.errors[path] {
			if inside, kept := within(path, te.Pos); !inside || kept {
				return nil, nil, &trimFatal{path, stdlibTypeError(te, t.files[path], fset)}, nil
			}
		}
	}
	// Imports reached declarations use must be on chain.
	needed := map[string]map[string]bool{}
	for _, d := range decls {
		if !d.kept {
			continue
		}
		var bad error
		ast.Inspect(d.node(), func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok || bad != nil {
				return bad == nil
			}
			pn, ok := info.Uses[id].(*types.PkgName)
			if !ok {
				return true
			}
			path := pn.Imported().Path()
			if !t.used[d.pkg][path] {
				return true
			}
			if _, module := t.typeOnlySrc[path]; module {
				if needed[d.pkg] == nil {
					needed[d.pkg] = map[string]bool{}
				}
				if !needed[d.pkg][path] {
					needed[d.pkg][path] = true
					promote = append(promote, path)
				}
				return true
			}
			bad = fmt.Errorf("%s: %s", fset.Position(importSpec(d.file, path).Pos()), stdlibImportRule(path))
			return false
		})
		if bad != nil {
			return nil, nil, nil, bad
		}
	}
	if len(promote) > 0 {
		return promote, nil, nil, nil
	}

	// Remove the unreachable declarations.
	report = &TrimReport{Patterns: append([]string(nil), t.patterns...)}
	keptSpec := map[ast.Spec]bool{}
	byPkg := map[string]*TrimmedPackage{}
	for _, path := range t.order {
		byPkg[path] = &TrimmedPackage{Path: path, Dropped: []TrimmedDecl{}}
		for dep := range t.used[path] {
			byPkg[path].TypeOnlyImports = append(byPkg[path].TypeOnlyImports, dep)
		}
		sort.Strings(byPkg[path].TypeOnlyImports)
	}
	for _, d := range decls {
		if d.kept {
			if d.spec != nil {
				keptSpec[d.spec] = true
			}
			continue
		}
		byPkg[d.pkg].Dropped = append(byPkg[d.pkg].Dropped, describeDecl(fset, info, d))
	}
	for _, file := range p.files {
		path, trimmed := trimmedFile[file]
		if !trimmed {
			continue
		}
		var kept []ast.Decl
		for _, d := range file.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if td := r.index[info.Defs[d.Name]]; td != nil && td.kept {
					kept = append(kept, d)
				}
			case *ast.GenDecl:
				var specs []ast.Spec
				for _, spec := range d.Specs {
					if d.Tok == token.IMPORT {
						im := spec.(*ast.ImportSpec)
						dep, _ := strconv.Unquote(im.Path.Value)
						if !t.used[path][dep] {
							specs = append(specs, spec)
						}
					} else if keptSpec[spec] {
						specs = append(specs, spec)
					}
				}
				if len(specs) > 0 {
					d.Specs = specs
					kept = append(kept, d)
				}
			}
		}
		file.Decls = kept
		var imports []*ast.ImportSpec
		for _, im := range file.Imports {
			dep, _ := strconv.Unquote(im.Path.Value)
			if !t.used[path][dep] {
				imports = append(imports, im)
			}
		}
		file.Imports = imports
	}
	p.decls = nil
	for _, file := range p.files {
		p.decls = append(p.decls, file.Decls...)
	}
	// Initializers of dropped specifications do not run.
	specOf := map[token.Pos]ast.Spec{}
	for _, d := range decls {
		if s, ok := d.spec.(*ast.ValueSpec); ok {
			for _, name := range s.Names {
				specOf[name.Pos()] = s
			}
		}
	}
	for path, inits := range p.initOrder {
		if byPkg[path] == nil {
			continue
		}
		var kept []*types.Initializer
		for _, init := range inits {
			if s := specOf[init.Lhs[0].Pos()]; s == nil || keptSpec[s] {
				kept = append(kept, init)
			}
		}
		p.initOrder[path] = kept
	}
	for _, path := range sortedKeys(byPkg) {
		report.Packages = append(report.Packages, *byPkg[path])
	}
	return nil, report, nil, nil
}

// importSpec is file's import of path.
func importSpec(file *ast.File, path string) ast.Node {
	for _, im := range file.Imports {
		if dep, _ := strconv.Unquote(im.Path.Value); dep == path {
			return im
		}
	}
	return file.Name
}

// describeDecl names a dropped declaration for the report.
func describeDecl(fset *token.FileSet, info *types.Info, d *trimDecl) TrimmedDecl {
	pos := fset.Position(d.node().Pos())
	out := TrimmedDecl{Position: fmt.Sprintf("%s:%d", filepath.Base(pos.Filename), pos.Line)}
	switch s := d.spec.(type) {
	case nil:
		fn := d.decl.(*ast.FuncDecl)
		out.Name, out.Kind = fn.Name.Name, "func"
		if fn.Recv != nil && len(fn.Recv.List) == 1 {
			if f, ok := info.Defs[fn.Name].(*types.Func); ok {
				if named := recvNamed(f); named != nil {
					out.Name, out.Kind = named.Obj().Name()+"."+fn.Name.Name, "method"
				}
			}
		}
	case *ast.TypeSpec:
		out.Name, out.Kind = s.Name.Name, "type"
	case *ast.ValueSpec:
		var names []string
		for _, n := range s.Names {
			names = append(names, n.Name)
		}
		out.Name = strings.Join(names, ", ")
		out.Kind = "const"
		if d.decl.(*ast.GenDecl).Tok == token.VAR {
			out.Kind, out.Initializer = "var", len(s.Values) > 0
		}
	}
	return out
}

// recvNamed is a method's receiver base type.
func recvNamed(f *types.Func) *types.Named {
	recv := f.Type().(*types.Signature).Recv()
	if recv == nil {
		return nil
	}
	t := recv.Type()
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	named, _ := t.(*types.Named)
	return named
}
