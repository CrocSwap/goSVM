package library

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestProducerRequiresExplicitPinnedContract(t *testing.T) {
	base := Producer{Version: 1, Kind: "bend-library-v1", Target: "fogo-v0", ABI: 1}
	authorized := false
	base.Authorized = &authorized
	base.Toolchains.Clang.Tools = "v1.53"
	base.Toolchains.Clang.CPU = "generic"
	tests := map[string]func(*Producer){
		"missing target":        func(p *Producer) { p.Target = "" },
		"wrong target":          func(p *Producer) { p.Target = "solana-v3" },
		"historical manifest":   func(p *Producer) { p.Kind = "" },
		"wrong ABI":             func(p *Producer) { p.ABI = 2 },
		"wrong tools":           func(p *Producer) { p.Toolchains.Clang.Tools = "v1.51" },
		"missing authorization": func(p *Producer) { p.Authorized = nil },
		"wrong CPU":             func(p *Producer) { p.Toolchains.Clang.CPU = "v3" },
		"wrong ISA":             func(p *Producer) { p.Toolchains.Clang.Version = 3 },
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			p := base
			change(&p)
			data, _ := json.Marshal(p)
			path := filepath.Join(t.TempDir(), "manifest.json")
			os.WriteFile(path, data, 0600)
			if _, err := ImportBend(path, fmt.Sprintf("%x", sha256.Sum256(data)), "bend", "Evaluate", "v1.53"); err == nil {
				t.Fatal("accepted incompatible contract")
			}
		})
	}
	path := filepath.Join(t.TempDir(), "manifest.json")
	data, _ := json.Marshal(base)
	os.WriteFile(path, data, 0600)
	if _, err := ImportBend(path, fmt.Sprintf("%064d", 0), "bend", "Evaluate", "v1.53"); err == nil {
		t.Fatal("accepted wrong manifest pin")
	}
	if _, err := ImportBend(path, fmt.Sprintf("%x", sha256.Sum256(data)), "bend", "Evaluate", ""); err == nil {
		t.Fatal("accepted implicit producer compiler")
	}
}

// The opt-in integration consumes the producer's actual immutable generation,
// rather than checking invented consumer artifacts into GoSVM.
func TestPublishedBendContract(t *testing.T) {
	manifest := os.Getenv("BEND_LIBRARY_MANIFEST")
	if manifest == "" {
		t.Skip("set BEND_LIBRARY_MANIFEST and BEND_LIBRARY_SHA256 for producer integration")
	}
	digest := os.Getenv("BEND_LIBRARY_SHA256")
	dir := filepath.Join(t.TempDir(), "bend")
	if err := GenerateBend(manifest, digest, "bend", "Evaluate", "v1.53", dir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, Filename)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = binding.Objects("fogo-v0"); err != nil {
		t.Fatal(err)
	}
	if _, err = binding.Objects("solana-v3"); err == nil {
		t.Fatal("relabelled v0 as v3")
	}
	for name, change := range map[string]func(*Contract){
		"root":               func(c *Contract) { c.Root = "other.root" },
		"fingerprint":        func(c *Contract) { c.Fingerprint = fmt.Sprintf("%064d", 0) },
		"width":              func(c *Contract) { c.InputBytes++ },
		"host architecture":  func(c *Contract) { c.Host.GOARCH = "wrong" },
		"producer tools":     func(c *Contract) { v := c.Targets["fogo-v0"]; v.Tools = "v1.51"; c.Targets["fogo-v0"] = v },
		"producer selection": func(c *Contract) { c.Header = c.Interface },
		"target relabel":     func(c *Contract) { c.Targets["solana-v3"] = c.Targets["fogo-v0"]; delete(c.Targets, "fogo-v0") },
	} {
		t.Run(name, func(t *testing.T) {
			var c Contract
			json.Unmarshal(data, &c)
			change(&c)
			changed, _ := json.Marshal(c)
			os.WriteFile(path, changed, 0600)
			defer os.WriteFile(path, data, 0600)
			if _, err := Read(path); err == nil {
				t.Fatal("accepted altered dependency")
			}
		})
	}
	object := filepath.Join(dir, binding.Targets["fogo-v0"].Object.Path)
	bytes, _ := os.ReadFile(object)
	changed := append([]byte(nil), bytes...)
	changed[48] = 3
	os.WriteFile(object, changed, 0600)
	if _, err := Read(path); err == nil {
		t.Fatal("accepted modified object")
	}
	os.WriteFile(object, bytes, 0600)
	native := filepath.Join(dir, "bindings.go")
	source, _ := os.ReadFile(native)
	changed = append(source, []byte("\nfunc Unapproved(){}\n")...)
	os.WriteFile(native, changed, 0600)
	var c Contract
	json.Unmarshal(data, &c)
	c.Generated["bindings.go"] = fmt.Sprintf("%x", sha256.Sum256(changed))
	altered, _ := json.Marshal(c)
	os.WriteFile(path, altered, 0600)
	if _, err := Read(path); err == nil {
		t.Fatal("accepted a divergent native adapter after rehash")
	}
	os.WriteFile(path, data, 0600)
	os.WriteFile(native, source, 0600)
	os.WriteFile(filepath.Join(dir, "extra.c"), []byte("void unexpected(){}"), 0600)
	if _, err := Read(path); err == nil {
		t.Fatal("accepted extra native C source")
	}
}

// Packages bound before multi-target support keep a flat layout with one
// contract-level producer; they must continue to validate unchanged.
func TestPublishedBendContractFlatLayout(t *testing.T) {
	manifest := os.Getenv("BEND_LIBRARY_MANIFEST")
	if manifest == "" {
		t.Skip("set BEND_LIBRARY_MANIFEST and BEND_LIBRARY_SHA256 for producer integration")
	}
	dir := filepath.Join(t.TempDir(), "bend")
	if err := GenerateBend(manifest, os.Getenv("BEND_LIBRARY_SHA256"), "bend", "Evaluate", "v1.53", dir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, Filename)
	b, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	flat := func(a Artifact) Artifact {
		os.Rename(filepath.Join(dir, a.Path), filepath.Join(dir, filepath.Base(a.Path)))
		return Artifact{filepath.Base(a.Path), a.SHA256}
	}
	c := b.Contract
	object := c.Targets["fogo-v0"]
	producer := flat(*object.Producer)
	c.Producer, object.Producer = &producer, nil
	object.Object = flat(object.Object)
	for i := range object.Support {
		object.Support[i] = flat(object.Support[i])
	}
	c.Targets["fogo-v0"] = object
	c.Interface, c.Header, c.Host.Object = flat(c.Interface), flat(c.Header), flat(c.Host.Object)
	source, err := nativeSource(c)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "bindings.go"), source, 0644)
	c.Generated["bindings.go"] = fmt.Sprintf("%x", sha256.Sum256(source))
	data, _ := json.Marshal(c)
	os.WriteFile(path, data, 0644)
	if _, err = Read(path); err != nil {
		t.Fatal("flat single-producer package no longer validates:", err)
	}
	object.Producer = &producer
	c.Targets["fogo-v0"] = object
	data, _ = json.Marshal(c)
	os.WriteFile(path, data, 0644)
	if _, err = Read(path); err == nil {
		t.Fatal("accepted ambiguous producer pins")
	}
}

// fogo-v0 v1.57 Bend producers are accepted ahead of publication and flagged
// as untested; the qualified pairings are not.
func TestBendFogoV157Accepted(t *testing.T) {
	p := Producer{Version: 1, Kind: "bend-library-v1", Target: "fogo-v0", ABI: 1, Symbol: "bend_library_v1_call_x", Fingerprint: "x"}
	authorized := false
	p.Authorized = &authorized
	p.Toolchains.Clang.Tools = "v1.57"
	p.Toolchains.Clang.CPU = "generic"
	if err := p.check("fogo-v0"); err != nil {
		t.Fatal(err)
	}
	if !Untested("fogo-v0", "v1.57") || Untested("fogo-v0", "v1.53") || Untested("solana-v3", "v1.57") {
		t.Fatal("wrong untested pairings")
	}
	if Accepts("bend-library-v1", "solana-v3", "v1.53") {
		t.Fatal("accepted v1.53 solana-v3 producer")
	}
}
