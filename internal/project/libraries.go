package project

import (
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
// Bend maps each network (or profile) to its pinned producer manifest.
type LibraryDeclaration struct {
	Function string                `json:"function"`
	Bend     map[string]LibraryPin `json:"bend,omitempty"`
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
		if len(l.Bend) == 0 {
			return fmt.Errorf("library %s declares no Bend producer manifests", name)
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
		if err := library.GenerateBendTargets(pins, name, l.Function, out); err != nil {
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
