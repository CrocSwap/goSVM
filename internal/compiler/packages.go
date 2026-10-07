package compiler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// PackageSources identifies ordinary Go sources by their module import path.
type PackageSources struct {
	Path    string
	Sources []Source
}

// Program contains the entry package and only its on-chain import graph.
// Metadata records module files used by resolution, for cache invalidation.
type Program struct {
	Entry    string
	Packages []PackageSources
	Metadata []Source
	SDK      int // 0/2: current SDK; 1: exact legacy snapshot
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
	imports, err := sourceImports(sources)
	if err != nil {
		return nil, err
	}
	p := &Program{Entry: "program", Packages: []PackageSources{{"program", sources}}}
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
		if im.path != "gosvm/solana" {
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
	active := map[string]bool{}
	metadata := map[string]bool{}
	for _, source := range p.Metadata {
		metadata[source.Name] = true
	}
	var visit func(string, []Source, *listedPackage) error
	visit = func(packagePath string, src []Source, listed *listedPackage) error {
		seen[packagePath], active[packagePath] = true, true
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
		for _, im := range imports {
			if im.path == "gosvm/solana" {
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
			dependency, e := resolver.resolve(im.path)
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
		p.Packages = append(p.Packages, PackageSources{packagePath, src})
		return nil
	}
	if err = visit(p.Entry, sources, root); err != nil {
		return nil, err
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
func (r *moduleResolver) resolve(path string) (*listedPackage, error) {
	cmd := exec.Command("go", "list", "-find", "-json", "-mod=readonly", "-modfile="+r.modFile, "--", path)
	cmd.Dir = r.dir
	// Fix host-dependent workspace/tool flags, prohibit implicit downloads and
	// toolchain switching, and use the caller's existing module/cache locations.
	overrides := map[string]string{"GO111MODULE": "on", "GOWORK": "off", "GOPROXY": "off", "GOSUMDB": "off", "GOTOOLCHAIN": "local", "GOFLAGS": ""}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if _, ok := overrides[key]; !ok {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	for key, value := range overrides {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
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
	if pkg.Module == nil || pkg.Dir == "" {
		return nil, fmt.Errorf("standard library and non-module imports unsupported (%q)", path)
	}
	return &pkg, nil
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
	g         *generator
	units     map[string]PackageSources
	checked   map[string]*types.Package
	active    map[string]bool
	files     []*ast.File
	decls     []ast.Decl
	sdk       sdkImporter
	hasSDK    bool
	entryPath string
}

func (p *programImporter) Import(path string) (*types.Package, error) {
	if path == "gosvm/solana" {
		p.hasSDK = true
		return p.sdk.Import(path)
	}
	if pkg := p.checked[path]; pkg != nil {
		return pkg, nil
	}
	if p.active[path] {
		return nil, fmt.Errorf("import cycle involving %q", path)
	}
	unit, ok := p.units[path]
	if !ok {
		return nil, fmt.Errorf("unsupported import %q; use a module-backed program for user packages", path)
	}
	p.active[path] = true
	sources := append([]Source(nil), unit.Sources...)
	sort.Slice(sources, func(i, j int) bool { return sources[i].Name < sources[j].Name })
	var files []*ast.File
	for _, source := range sources {
		file, err := parser.ParseFile(p.g.fset, source.Name, source.Data, 0)
		if err != nil {
			return nil, err
		}
		for _, im := range file.Imports {
			if im.Name != nil && (im.Name.Name == "." || im.Name.Name == "_") {
				p.g.fail(im, "dot and blank imports unsupported")
			}
			dependency, _ := strconv.Unquote(im.Path.Value)
			if !internalImportAllowed(path, dependency) {
				p.g.fail(im, "use of internal package %q not allowed", dependency)
			}
		}
		files = append(files, file)
	}
	conf := types.Config{Sizes: &types.StdSizes{WordSize: 8, MaxAlign: 8}, Importer: p}
	pkg, err := conf.Check(path, p.g.fset, files, p.g.info)
	if err != nil {
		return nil, err
	}
	if path != p.entryPath && pkg.Name() == "main" {
		return nil, fmt.Errorf("%s: package %q is a program, not importable", p.g.fset.Position(files[0].Package), path)
	}
	p.checked[path], p.active[path] = pkg, false
	p.files = append(p.files, files...)
	for _, file := range files {
		p.decls = append(p.decls, file.Decls...)
	}
	return pkg, nil
}
