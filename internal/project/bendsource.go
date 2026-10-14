package project

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gosvm/internal/library"
	"gosvm/internal/targets"
)

// bendBuildRecord, written last, marks a completed Bend source build and
// records each target's manifest (relative to the build) and SHA-256.
const bendBuildRecord = "gosvm-bend-build.json"

// projectPath records nearby paths (in or beside the project) relative to it
// and others absolutely.
func projectPath(dir, abs string) string {
	rel, err := filepath.Rel(dir, abs)
	if err != nil || strings.HasPrefix(rel, "../../") {
		return abs
	}
	return filepath.ToSlash(rel)
}

func projectFile(dir, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(dir, filepath.FromSlash(path))
}

// bendCheckoutDir resolves a declaration's checkout, else $GOSVM_BEND.
func bendCheckoutDir(dir string, b *BendSource) (string, error) {
	if b.Checkout != "" {
		return projectFile(dir, b.Checkout), nil
	}
	if env := os.Getenv(library.BendCheckoutEnv); env != "" {
		return filepath.Abs(env)
	}
	return "", fmt.Errorf("no Bend checkout: record one with gosvm library add -bend-checkout <bendSVM dir> or set %s", library.BendCheckoutEnv)
}

// readBendBuild returns a completed build's pins, or nil.
func readBendBuild(out string, targets []string) map[string]library.ProducerPin {
	data, err := os.ReadFile(filepath.Join(out, bendBuildRecord))
	if err != nil {
		return nil
	}
	var recorded map[string]LibraryPin
	if json.Unmarshal(data, &recorded) != nil || len(recorded) != len(targets) {
		return nil
	}
	pins := map[string]library.ProducerPin{}
	for _, t := range targets {
		pin, ok := recorded[t]
		if !ok || filepath.IsAbs(pin.Manifest) || strings.HasPrefix(filepath.Clean(pin.Manifest), "..") {
			return nil
		}
		pins[t] = library.ProducerPin{Manifest: filepath.Join(out, filepath.FromSlash(pin.Manifest)), SHA256: pin.SHA256}
	}
	return pins
}

// syncBendSource builds the Bend source with the checkout's build:library for
// the project's targets (reusing a completed build with the same key under
// build/bend-libraries/<name>/<key16>) and binds the resulting producer
// manifests through the pinned-manifest path, so every consumer contract
// check applies.
func syncBendSource(dir string, c Config, name string, l LibraryDeclaration) error {
	profiles, err := c.TargetProfiles()
	if err != nil {
		return err
	}
	var targets []string
	for _, p := range profiles {
		if p.Name == target.Legacy {
			return fmt.Errorf("Bend source libraries require fogo/solana targets")
		}
		targets = append(targets, p.Name)
	}
	checkoutDir, err := bendCheckoutDir(dir, l.BendSource)
	if err != nil {
		return err
	}
	checkout, err := library.InspectBendCheckout(checkoutDir)
	if err != nil {
		return err
	}
	source := projectFile(dir, l.BendSource.File)
	key, err := library.BendBuildKey(checkout, source, l.BendSource.Root, targets)
	if err != nil {
		return err
	}
	out := filepath.Join(dir, "build", "bend-libraries", name, key[:16])
	pins := readBendBuild(out, targets)
	if pins == nil {
		if checkout.Dirty != "" {
			fmt.Printf("Warning: Bend checkout %s has uncommitted changes; the build key includes them\n", checkout.Dir)
		}
		os.RemoveAll(filepath.Dir(out)) // Drop superseded or incomplete builds of this library.
		if pins, err = library.BuildBendSource(checkout, library.BendSourceOptions{
			Checkout: checkout.Dir, Source: source, Root: l.BendSource.Root, Output: out, Targets: targets,
			Log: func(format string, args ...any) { fmt.Printf(format, args...) },
		}); err != nil {
			return err
		}
		relative := map[string]LibraryPin{}
		for t, pin := range pins {
			rel, err := filepath.Rel(out, pin.Manifest)
			if err != nil {
				return err
			}
			relative[t] = LibraryPin{Manifest: filepath.ToSlash(rel), SHA256: pin.SHA256}
		}
		data, _ := json.MarshalIndent(relative, "", "  ")
		if err = os.WriteFile(filepath.Join(out, bendBuildRecord), append(data, '\n'), 0644); err != nil {
			os.RemoveAll(out)
			return err
		}
	}
	libs := LibraryDir(dir, name)
	if current(libs, name, l.Function, pins) {
		return nil
	}
	return library.GenerateLibrary(pins, name, l.Function, libs)
}

// AddBendSourceLibrary records a Bend source file and root (and optionally its
// checkout) under name, builds it for the project's targets and generates the
// package.
func AddBendSourceLibrary(dir, name, function, file, root, checkout string) error {
	c, err := Load(dir)
	if err != nil {
		return err
	}
	abs, err := filepath.Abs(file)
	if err != nil {
		return err
	}
	b := &BendSource{File: projectPath(dir, abs), Root: root}
	if checkout != "" {
		if checkout, err = filepath.Abs(checkout); err != nil {
			return err
		}
		b.Checkout = projectPath(dir, checkout)
	}
	l := LibraryDeclaration{Function: function, BendSource: b}
	if c.Libraries == nil {
		c.Libraries = map[string]LibraryDeclaration{}
	}
	c.Libraries[name] = l
	if err = validateLibraries(c); err != nil {
		return err
	}
	if err = syncBendSource(dir, c, name, l); err != nil {
		return err
	}
	return Save(dir, c)
}
