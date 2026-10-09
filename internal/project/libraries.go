package project

import (
	"crypto/sha256"
	"fmt"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gosvm/internal/library"
	"gosvm/internal/targets"
)

// LibraryDeclaration binds a pure foreign library as the Go package libs/<name>.
// Bend maps each network (or profile) to its pinned producer manifest; Rust
// names a crate GoSVM builds for every project target.
type LibraryDeclaration struct {
	Function string                `json:"function"`
	Bend     map[string]LibraryPin `json:"bend,omitempty"`
	Rust     *RustCrate            `json:"rust,omitempty"`
}

type RustCrate struct {
	Crate string `json:"crate"`
}

type LibraryPin struct {
	Manifest string `json:"manifest"`
	SHA256   string `json:"sha256"`
}

var libraryNameRE = regexp.MustCompile(`^[a-z][a-z0-9]*$`)

func validateLibraries(c Config) error {
	for name, l := range c.Libraries {
		if !libraryNameRE.MatchString(name) || token.Lookup(name).IsKeyword() || name == "main" {
			return fmt.Errorf("library name %q must be a lowercase Go package name", name)
		}
		if !token.IsIdentifier(l.Function) || !token.IsExported(l.Function) {
			return fmt.Errorf("library %s: function %q must be an exported Go identifier", name, l.Function)
		}
		if (len(l.Bend) == 0) == (l.Rust == nil) {
			return fmt.Errorf("library %s must declare either Bend producer manifests or a Rust crate", name)
		}
		if l.Rust != nil && l.Rust.Crate == "" {
			return fmt.Errorf("library %s: rust requires a crate directory", name)
		}
		seen := map[string]bool{}
		for key, pin := range l.Bend {
			p, err := target.Resolve(key)
			if err != nil {
				return fmt.Errorf("library %s: %w", name, err)
			}
			if p.Name == target.Legacy || seen[p.Name] {
				return fmt.Errorf("library %s: %q must name a distinct fogo or solana target", name, key)
			}
			seen[p.Name] = true
			if pin.Manifest == "" || len(pin.SHA256) != 64 {
				return fmt.Errorf("library %s/%s requires manifest and sha256", name, key)
			}
		}
	}
	return nil
}

// LibraryDir is the generated Go package for a declared library.
func LibraryDir(dir, name string) string { return filepath.Join(dir, "libs", name) }

// SyncLibraries regenerates libs/<name> whenever its producer pins, package or
// function differ from gosvm.json; up-to-date packages are only revalidated.
func SyncLibraries(dir string, c Config) error {
	for name, l := range c.Libraries {
		if l.Rust != nil {
			if err := syncRust(dir, c, name, l); err != nil {
				return fmt.Errorf("library %s: %w", name, err)
			}
			continue
		}
		pins := map[string]library.ProducerPin{}
		for key, pin := range l.Bend {
			p, _ := target.Resolve(key)
			manifest := pin.Manifest
			if !filepath.IsAbs(manifest) {
				manifest = filepath.Join(dir, manifest)
			}
			pins[p.Name] = library.ProducerPin{Manifest: manifest, SHA256: pin.SHA256}
		}
		out := LibraryDir(dir, name)
		if current(out, name, l.Function, pins) {
			continue
		}
		if err := library.GenerateLibrary(pins, name, l.Function, out); err != nil {
			return fmt.Errorf("library %s: %w", name, err)
		}
	}
	return nil
}

func current(out, pkg, function string, pins map[string]library.ProducerPin) bool {
	if _, err := os.Stat(filepath.Join(out, library.Filename)); err != nil {
		return false
	}
	b, err := library.Read(filepath.Join(out, library.Filename))
	if err != nil || b.Package != pkg || b.Function != function || len(b.Targets) != len(pins) {
		return false
	}
	for name, pin := range pins {
		object, ok := b.Targets[name]
		if !ok || object.Producer == nil || object.Producer.SHA256 != pin.SHA256 {
			return false
		}
	}
	return true
}

// AddBendLibrary records producer manifests (one per target) under name and
// generates the package. Each manifest's target and SHA-256 come from the
// manifest itself; existing pins for other targets are kept.
func AddBendLibrary(dir, name, function string, manifests []string) ([]string, error) {
	c, err := Load(dir)
	if err != nil {
		return nil, err
	}
	l := c.Libraries[name]
	if function != "" {
		l.Function = function
	}
	if l.Bend == nil {
		l.Bend = map[string]LibraryPin{}
	}
	var added []string
	for _, manifest := range manifests {
		abs, err := filepath.Abs(manifest)
		if err != nil {
			return nil, err
		}
		profile, digest, err := library.InspectBend(abs)
		if err != nil {
			return nil, err
		}
		p, _ := target.Resolve(profile)
		for key := range l.Bend {
			if q, _ := target.Resolve(key); q.Name == p.Name {
				delete(l.Bend, key)
			}
		}
		// Keep nearby producers (in or beside the project) portable; record others absolutely.
		rel, err := filepath.Rel(dir, abs)
		if err != nil || strings.HasPrefix(rel, "../../") {
			rel = abs
		}
		l.Bend[p.Network()] = LibraryPin{Manifest: filepath.ToSlash(rel), SHA256: digest}
		added = append(added, p.Network())
	}
	if c.Libraries == nil {
		c.Libraries = map[string]LibraryDeclaration{}
	}
	c.Libraries[name] = l
	if err = validateLibraries(c); err != nil {
		return nil, err
	}
	if err = SyncLibraries(dir, Config{Libraries: map[string]LibraryDeclaration{name: l}}); err != nil {
		return nil, err
	}
	return added, Save(dir, c)
}

// syncRust builds the crate for the project's targets (reusing a build with
// the same source/target/compiler key under build/rust-libraries) and binds it.
func syncRust(dir string, c Config, name string, l LibraryDeclaration) error {
	crate := l.Rust.Crate
	if !filepath.IsAbs(crate) {
		crate = filepath.Join(dir, crate)
	}
	profiles, err := c.TargetProfiles()
	if err != nil {
		return err
	}
	var targets []string
	for _, p := range profiles {
		if p.Name == target.Legacy {
			return fmt.Errorf("Rust libraries require fogo/solana targets")
		}
		targets = append(targets, p.Name)
	}
	key, err := library.RustBuildKey(crate, targets)
	if err != nil {
		return err
	}
	out := filepath.Join(dir, "build", "rust-libraries", name, key[:16])
	manifests := map[string]string{}
	for _, t := range targets {
		manifests[t] = filepath.Join(out, t, "library.manifest.json")
	}
	if _, err = os.Stat(out); err != nil {
		os.RemoveAll(filepath.Dir(out)) // Drop superseded builds of this library.
		fmt.Printf("Building Rust library %s for %s\n", name, strings.Join(targets, ", "))
		if _, err = library.BuildRust(library.RustOptions{Crate: crate, Output: out, Targets: targets}); err != nil {
			return err
		}
	}
	pins := map[string]library.ProducerPin{}
	for t, manifest := range manifests {
		data, err := os.ReadFile(manifest)
		if err != nil {
			return fmt.Errorf("incomplete Rust build %s (remove it to rebuild): %w", out, err)
		}
		pins[t] = library.ProducerPin{Manifest: manifest, SHA256: fmt.Sprintf("%x", sha256.Sum256(data))}
	}
	libs := LibraryDir(dir, name)
	if current(libs, name, l.Function, pins) {
		return nil
	}
	return library.GenerateLibrary(pins, name, l.Function, libs)
}

// AddRustLibrary records a Rust crate under name, builds it for the project's
// targets and generates the package.
func AddRustLibrary(dir, name, function, crate string) error {
	c, err := Load(dir)
	if err != nil {
		return err
	}
	abs, err := filepath.Abs(crate)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(dir, abs)
	if err != nil || strings.HasPrefix(rel, "../../") {
		rel = abs
	}
	l := LibraryDeclaration{Function: function, Rust: &RustCrate{Crate: filepath.ToSlash(rel)}}
	if c.Libraries == nil {
		c.Libraries = map[string]LibraryDeclaration{}
	}
	c.Libraries[name] = l
	if err = validateLibraries(c); err != nil {
		return err
	}
	if err = syncRust(dir, c, name, l); err != nil {
		return err
	}
	return Save(dir, c)
}
