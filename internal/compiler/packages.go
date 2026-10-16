package compiler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"go/types"
	"gosvm/internal/library"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// PackageSources identifies ordinary Go sources by their module import path.
type PackageSources struct {
	Path    string
	Sources []Source
}

// Program contains the entry package and only its on-chain import graph.
// Metadata records module files used by resolution, for cache invalidation.
type Program struct {
	Entry     string
	Packages  []PackageSources
	Metadata  []Source
	Libraries []library.Binding
	SDK       int // 0/2: current SDK; 1: exact legacy snapshot
	// HeapBytes is the invocation heap budget for allocation; 0 selects the
	// 32 KiB default. See ValidateHeapBytes and docs/GO_ALLOCATION.md.
	HeapBytes int `json:",omitempty"`
	// RuntimeInit disables compile-time evaluation of package
	// initialization (docs/GO_GLOBALS.md#compile-time-evaluation): every
	// initializer runs at run time on every invocation. A diagnostic and
	// comparison switch; the observable behaviour is the same either way.
	RuntimeInit bool `json:",omitempty"`
	// Trim lists the import-path patterns (an exact path, or a prefix
	// followed by "/...") of packages whose declarations are checked and
	// compiled only when Process reaches them (docs/GO_PACKAGES.md#trimming).
	// Empty: every declaration of every package is checked (the default).
	Trim []string `json:",omitempty"`
	// TypeOnly holds module packages that only trimmed packages import,
	// with their module dependencies: type information for those imports.
	// Check compiles one on chain only when a reached declaration uses it.
	// Empty without Trim.
	TypeOnly []PackageSources `json:",omitempty"`
	// TypeOnlyUnsupported maps each TypeOnly package whose sources cannot
	// be compiled on chain (build constraints, cgo, platform-specific
	// files) to why. Its Sources are then the files Go selects for
	// linux/amd64 without cgo, used for type information only.
	TypeOnlyUnsupported map[string]string `json:",omitempty"`
}

// ReadOptions are program settings that change resolution.
type ReadOptions struct {
	// Trim is Program.Trim; see ValidateTrim.
	Trim []string
}

// ReadProgramWithOptions is ReadProgram with resolution options.
func ReadProgramWithOptions(path string, o ReadOptions) (*Program, error) {
	sources, err := ReadSources(path)
	if err != nil {
		return nil, err
	}
	return ReadProgramWithSourcesOptions(path, sources, o)
}

// ReadProgram retains the file/directory input convention. Only user imports
// require module resolution. Resolution is offline and never runs user code,
// package initialization, generators, or Go compilation.
func ReadProgram(path string) (*Program, error) {
	sources, err := ReadSources(path)
	if err != nil {
		return nil, err
	}
	return ReadProgramWithSources(path, sources)
}

// ReadProgramWithSources resolves an entry package using caller-supplied source
// bytes. Generators can replace their own adapter in memory before validating
// it, without publishing a partially generated file or consulting stale bindings.
// Dependencies and module metadata are read with ReadProgram's offline policy.
func ReadProgramWithSources(path string, sources []Source) (*Program, error) {
	return ReadProgramWithSourcesOptions(path, sources, ReadOptions{})
}

// ReadProgramWithSourcesOptions is ReadProgramWithSources with resolution
// options.
func ReadProgramWithSourcesOptions(path string, sources []Source, o ReadOptions) (*Program, error) {
	if err := ValidateTrim(o.Trim); err != nil {
		return nil, err
	}
	imports, err := sourceImports(sources)
	if err != nil {
		return nil, err
	}
	p := &Program{Entry: "program", Packages: []PackageSources{{Path: "program", Sources: sources}}, Trim: append([]string(nil), o.Trim...)}
	// Project snapshots select the exact API used by their ordinary Go modules.
	dir, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(dir)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		dir = filepath.Dir(dir)
	}
	for root := dir; ; root = filepath.Dir(root) {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			snapshot := filepath.Join(root, ".gosvm/sdk/solana/api.go")
			b, err := os.ReadFile(snapshot)
			if err == nil {
				if bytes.Equal(b, []byte(sdkSource(1))) {
					p.SDK = 1
				} else if bytes.Equal(b, []byte(sdkSource(2))) {
					p.SDK = 2
				} else {
					return nil, fmt.Errorf("project SDK snapshot differs from this compiler")
				}
				p.Metadata = append(p.Metadata, Source{snapshot, b})
			} else if !os.IsNotExist(err) {
				return nil, err
			}
			break
		} else if !os.IsNotExist(err) {
			return nil, err
		}
		if filepath.Dir(root) == root {
			break
		}
	}
	userImports := false
	for _, im := range imports {
		if !compilerImport(im.path) {
			userImports = true
		}
	}
	if !userImports {
		return p, nil
	}
	resolver, err := newModuleResolver(dir)
	if err != nil {
		return nil, fmt.Errorf("%s: user imports require a Go module: %w", sources[0].Name, err)
	}
	defer os.RemoveAll(filepath.Dir(resolver.modFile))
	root, err := resolver.resolve(".")
	if err != nil {
		return nil, fmt.Errorf("%s: user imports require a Go module: %w", sources[0].Name, err)
	}
	p.Entry = root.ImportPath
	p.Packages = nil
	p.Metadata = append(p.Metadata, resolver.metadata...)
	seen := map[string]bool{}
	// Module imports of trimmed packages are optional: resolved after the
	// on-chain graph, for type information (trim.go).
	var optional []sourceImport
	optionalFrom := map[string]string{}
	active := map[string]bool{}
	metadata := map[string]bool{}
	for _, source := range p.Metadata {
		metadata[source.Name] = true
	}
	var visit func(string, []Source, *listedPackage) error
	visit = func(packagePath string, src []Source, listed *listedPackage) error {
		seen[packagePath], active[packagePath] = true, true
		if _, err := os.Stat(filepath.Join(listed.Dir, library.Filename)); err == nil {
			b, err := library.Read(filepath.Join(listed.Dir, library.Filename))
			if err != nil {
				return err
			}
			b.ImportPath = packagePath
			p.Libraries = append(p.Libraries, b)
			for path, data := range b.Metadata {
				if !metadata[path] {
					metadata[path] = true
					p.Metadata = append(p.Metadata, Source{path, data})
				}
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		for _, module := range []*listedModule{listed.Module, replacement(listed.Module)} {
			if module == nil || module.GoMod == "" || module.GoMod == resolver.modFile {
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
		imports, e := sourceImports(src)
		if e != nil {
			return e
		}
		trimmed := matchTrim(p.Trim, packagePath)
		for _, im := range imports {
			if compilerImport(im.path) {
				continue
			}
			if trimmed && !seen[im.path] && !active[im.path] && im.path != p.Entry {
				// Only reached declarations of a trimmed package need its
				// imports on chain: a standard-library package is type
				// information from GOROOT, a module package is resolved
				// below and compiled only if Check finds it used.
				if !standardPackage(im.path) {
					if _, dup := optionalFrom[im.path]; !dup {
						optionalFrom[im.path] = packagePath
						optional = append(optional, im)
					}
				}
				continue
			}
			if im.path == p.Entry || active[im.path] {
				return fmt.Errorf("%s: import cycle involving %q", im.position, im.path)
			}
			if !internalImportAllowed(packagePath, im.path) {
				return fmt.Errorf("%s: use of internal package %q not allowed from %q", im.position, im.path, packagePath)
			}
			if seen[im.path] {
				continue
			}
			if standardPackage(im.path) {
				return fmt.Errorf("%s: %s%s", im.position, stdlibImportRule(im.path), trimHint(p, packagePath))
			}
			dependency, e := resolver.resolve(im.path)
			if _, ok := e.(standardImportError); ok {
				return fmt.Errorf("%s: %s%s", im.position, stdlibImportRule(im.path), trimHint(p, packagePath))
			}
			if e != nil {
				return fmt.Errorf("%s: resolve import %q: %w", im.position, im.path, e)
			}
			if dependency.Name == "main" {
				return fmt.Errorf("%s: package %q is a program, not importable", im.position, im.path)
			}
			depSources, e := ReadSources(dependency.Dir)
			if e != nil {
				return fmt.Errorf("%s: import %q: %w", im.position, im.path, e)
			}
			if e = visit(im.path, depSources, dependency); e != nil {
				return e
			}
		}
		active[packagePath] = false
		p.Packages = append(p.Packages, PackageSources{Path: packagePath, Sources: src})
		return nil
	}
	if err = visit(p.Entry, sources, root); err != nil {
		return nil, err
	}
	var pending []sourceImport
	for _, im := range optional {
		if !seen[im.path] {
			pending = append(pending, im)
		}
	}
	if len(pending) > 0 {
		if err = resolveTypeOnly(p, resolver, pending, optionalFrom, seen, metadata); err != nil {
			return nil, err
		}
	}
	sort.Slice(p.Metadata, func(i, j int) bool { return p.Metadata[i].Name < p.Metadata[j].Name })
	return p, nil
}

type sourceImport struct {
	path     string
	position token.Position
}

func sourceImports(sources []Source) ([]sourceImport, error) {
	var imports []sourceImport
	fset := token.NewFileSet()
	for _, source := range sources {
		file, err := parser.ParseFile(fset, source.Name, source.Data, parser.ImportsOnly)
		if err != nil {
			return nil, err
		}
		for _, im := range file.Imports {
			path, err := strconv.Unquote(im.Path.Value)
			if err != nil {
				return nil, err
			}
			if im.Name != nil && (im.Name.Name == "." || im.Name.Name == "_") {
				return nil, fmt.Errorf("%s: dot and blank imports unsupported in on-chain packages", fset.Position(im.Pos()))
			}
			imports = append(imports, sourceImport{path, fset.Position(im.Pos())})
		}
	}
	return imports, nil
}

type listedModule struct {
	GoMod   string
	Replace *listedModule
}
type listedPackage struct {
	ImportPath, Dir, Name string
	Standard              bool
	Module                *listedModule
	Error                 *struct{ Err string }
}

func replacement(m *listedModule) *listedModule {
	if m == nil {
		return nil
	}
	return m.Replace
}

type moduleResolver struct {
	dir, modFile string
	metadata     []Source
}

// -mod=readonly protects go.mod but can still update go.sum. A private alternate
// mod/sum pair keeps even checksum bookkeeping away from the user's project.
func newModuleResolver(dir string) (*moduleResolver, error) {
	root := dir
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			break
		} else if !os.IsNotExist(err) {
			return nil, err
		}
		parent := filepath.Dir(root)
		if parent == root {
			return nil, fmt.Errorf("no go.mod found")
		}
		root = parent
	}
	stage, err := os.MkdirTemp("", "gosvm-modules-")
	if err != nil {
		return nil, err
	}
	r := &moduleResolver{dir: dir, modFile: filepath.Join(stage, "graph.mod")}
	for _, name := range []string{"go.mod", "go.sum"} {
		original := filepath.Join(root, name)
		data, err := os.ReadFile(original)
		if os.IsNotExist(err) && name == "go.sum" {
			continue
		}
		if err != nil {
			os.RemoveAll(stage)
			return nil, err
		}
		r.metadata = append(r.metadata, Source{original, data})
		target := r.modFile
		if name == "go.sum" {
			target = filepath.Join(stage, "graph.sum")
		}
		if err = os.WriteFile(target, data, 0600); err != nil {
			os.RemoveAll(stage)
			return nil, err
		}
	}
	return r, nil
}

// command is an offline go command in the resolver's module; extra
// environment settings override the defaults.
func (r *moduleResolver) command(extra map[string]string, args ...string) *exec.Cmd {
	cmd := exec.Command("go", args...)
	cmd.Dir = r.dir
	// Fix host-dependent workspace/tool flags, prohibit implicit downloads and
	// toolchain switching, and use the caller's existing module/cache locations.
	overrides := map[string]string{"GO111MODULE": "on", "GOWORK": "off", "GOPROXY": "off", "GOSUMDB": "off", "GOTOOLCHAIN": "local", "GOFLAGS": ""}
	for key, value := range extra {
		overrides[key] = value
	}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if _, ok := overrides[key]; !ok {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	for key, value := range overrides {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	return cmd
}

func (r *moduleResolver) resolve(path string) (*listedPackage, error) {
	cmd := r.command(nil, "list", "-find", "-json", "-mod=readonly", "-modfile="+r.modFile, "--", path)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("offline go list: %s", strings.TrimSpace(stderr.String()))
	}
	var pkg listedPackage
	if err := json.Unmarshal(out.Bytes(), &pkg); err != nil {
		return nil, fmt.Errorf("go list metadata: %w", err)
	}
	if pkg.Error != nil {
		return nil, fmt.Errorf("%s", pkg.Error.Err)
	}
	if pkg.Standard {
		return nil, standardImportError{path}
	}
	if pkg.Module == nil || pkg.Dir == "" {
		return nil, fmt.Errorf("standard library and non-module imports unsupported (%q)", path)
	}
	return &pkg, nil
}

// compilerImport reports whether the compiler itself provides the package
// (the SDK boundary, math/bits and the standard-library overlay packages),
// so it needs no module resolution.
func compilerImport(path string) bool {
	return path == "gosvm/solana" || path == bitsPath || stdlibPackage(path)
}

func internalImportAllowed(from, to string) bool {
	parts := strings.Split(to, "/")
	for i, part := range parts {
		if part != "internal" {
			continue
		}
		parent := strings.Join(parts[:i], "/")
		if parent == "" || from != parent && !strings.HasPrefix(from, parent+"/") {
			return false
		}
	}
	return true
}

// programImporter shares one type universe and one Info map across all packages,
// preserving named-type identity rather than flattening import names into text.
type programImporter struct {
	f       *frontend
	units   map[string]PackageSources
	checked map[string]*types.Package
	active  map[string]bool
	files   []*ast.File
	decls   []ast.Decl
	// filePaths[i] is the import path of files[i]'s package; initOrder is
	// each user package's go/types variable initialization order
	// (docs/GO_GLOBALS.md).
	filePaths []string
	initOrder map[string][]*types.Initializer
	sdk       sdkImporter
	hasSDK    bool
	bits      *types.Package // math/bits declarations, once imported
	entryPath string
	ctxt      *types.Context // shared, so each generic type instance is one *types.Named
	// trim is the trimming state (trim.go); nil without Program.Trim.
	// trimmed is its report once the load is complete.
	trim    *trimLoader
	trimmed *TrimReport
}

func (p *programImporter) Import(path string) (*types.Package, error) {
	if path == "gosvm/solana" {
		p.hasSDK = true
		return p.sdk.Import(path)
	}
	if path == bitsPath {
		if p.bits == nil {
			pkg, err := importBits()
			if err != nil {
				return nil, err
			}
			p.bits = pkg
		}
		return p.bits, nil
	}
	if pkg := p.checked[path]; pkg != nil {
		return pkg, nil
	}
	if p.active[path] {
		return nil, fmt.Errorf("import cycle involving %q", path)
	}
	unit, ok := p.units[path]
	if !ok && stdlibPackage(path) {
		// A standard-library overlay package (stdlib.go).
		unit, ok = PackageSources{Path: path, Sources: stdlibSources(path)}, true
	}
	if !ok && p.trim != nil {
		// A module package only trimmed packages import, which this
		// package needs on chain (trim.go).
		if err := p.trim.promoteImport(path); err != nil {
			return nil, err
		}
	}
	if !ok {
		if standardPackage(path) {
			return nil, fmt.Errorf("%s", stdlibImportRule(path))
		}
		return nil, fmt.Errorf("unsupported import %q; use a module-backed program for user packages", path)
	}
	p.active[path] = true
	sources := append([]Source(nil), unit.Sources...)
	sort.Slice(sources, func(i, j int) bool { return sources[i].Name < sources[j].Name })
	var files []*ast.File
	for _, source := range sources {
		// Overlay sources keep their comments: //gosvm: directives mark
		// intrinsics (docs/GO_STDLIB.md#intrinsics).
		mode := parser.Mode(0)
		if isStdlibSource(source.Name) {
			mode = parser.ParseComments
		}
		file, err := parser.ParseFile(p.f.fset, source.Name, source.Data, mode)
		if err != nil {
			return nil, err
		}
		for _, im := range file.Imports {
			if im.Name != nil && (im.Name.Name == "." || im.Name.Name == "_") {
				p.f.fail(im, "dot and blank imports unsupported")
			}
			dependency, _ := strconv.Unquote(im.Path.Value)
			// Standard-library overlay packages may import the overlay's
			// internal packages, as in Go (docs/GO_STDLIB.md).
			if !internalImportAllowed(path, dependency) && !(stdlibPackage(path) && stdlibPackage(dependency)) {
				p.f.fail(im, "use of internal package %q not allowed", dependency)
			}
		}
		files = append(files, file)
	}
	conf := types.Config{Sizes: &types.StdSizes{WordSize: 8, MaxAlign: 8}, Importer: p, Context: p.ctxt}
	if p.trim != nil && p.trim.trimmed(path) {
		// A trimmed package: its type errors are decided once reachability
		// is known (trim.go).
		pkg, err := p.trim.checkTrimmed(&conf, path, files)
		if err != nil {
			return nil, err
		}
		p.checked[path], p.active[path] = pkg, false
		p.finish(path, files)
		return pkg, nil
	}
	pkg, err := conf.Check(path, p.f.fset, files, p.f.info)
	if err != nil {
		// A reference to a standard-library symbol the overlay leaves out
		// is reported by name (docs/GO_STDLIB.md).
		return nil, stdlibTypeError(err, files, p.f.fset)
	}
	if path != p.entryPath && pkg.Name() == "main" {
		return nil, fmt.Errorf("%s: package %q is a program, not importable", p.f.fset.Position(files[0].Package), path)
	}
	p.checked[path], p.active[path] = pkg, false
	p.finish(path, files)
	return pkg, nil
}

// finish records a checked package's initialization order and files.
func (p *programImporter) finish(path string, files []*ast.File) {
	// The shared Info's InitOrder is this package's until the next package
	// finishes checking; keep a copy.
	if len(p.f.info.InitOrder) > 0 {
		if p.initOrder == nil {
			p.initOrder = map[string][]*types.Initializer{}
		}
		p.initOrder[path] = append([]*types.Initializer(nil), p.f.info.InitOrder...)
	}
	p.files = append(p.files, files...)
	for _, file := range files {
		p.decls = append(p.decls, file.Decls...)
		p.filePaths = append(p.filePaths, path)
	}
}

// standardImportError is resolve's error for a standard-library package
// (go list reports it as Standard): one the overlay does not provide.
type standardImportError struct{ path string }

func (e standardImportError) Error() string { return stdlibImportRule(e.path) }

// standardPackage reports whether path names a package of the Go standard
// library (errors, fmt, strings, ...), which on-chain programs cannot import
// apart from math/bits and the overlay packages (docs/GO_STDLIB.md).
func standardPackage(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	if strings.Contains(first, ".") {
		return false
	}
	root := goroot()
	if root == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(root, "src", filepath.FromSlash(path)))
	return err == nil && info.IsDir()
}

// goroot is the Go installation's root: go/build's default, or, for a
// compiler built with -trimpath (which leaves it empty), `go env GOROOT`.
var goroot = sync.OnceValue(func() string {
	if build.Default.GOROOT != "" {
		return build.Default.GOROOT
	}
	out, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
})
