package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLibraryDeclarationsValidate(t *testing.T) {
	pin := LibraryPin{Manifest: "m.json", SHA256: strings.Repeat("a", 64)}
	good := Config{Libraries: map[string]LibraryDeclaration{"rules": {Function: "Evaluate", Bend: map[string]LibraryPin{"fogo": pin, "solana-v3": pin}}}}
	if err := validateLibraries(good); err != nil {
		t.Fatal(err)
	}
	for name, l := range map[string]LibraryDeclaration{
		"Rules": {Function: "Evaluate", Bend: map[string]LibraryPin{"fogo": pin}},
		"rules": {Function: "evaluate", Bend: map[string]LibraryPin{"fogo": pin}},
		"empty": {Function: "Evaluate"},
		"dup":   {Function: "Evaluate", Bend: map[string]LibraryPin{"fogo": pin, "fogo-v0": pin}},
		"old":   {Function: "Evaluate", Bend: map[string]LibraryPin{"legacy-v1.51-v3": pin}},
		"nosha": {Function: "Evaluate", Bend: map[string]LibraryPin{"solana": {Manifest: "m.json"}}},
		"func":  {Function: "Evaluate", Bend: map[string]LibraryPin{"eclipse": pin}},
	} {
		if err := validateLibraries(Config{Libraries: map[string]LibraryDeclaration{name: l}}); err == nil {
			t.Fatalf("accepted %s: %+v", name, l)
		}
	}
}

// Opt-in: the producer's real manifest is recorded and bound by AddBendLibrary.
func TestAddBendLibrary(t *testing.T) {
	manifest := os.Getenv("BEND_LIBRARY_MANIFEST")
	if manifest == "" {
		t.Skip("set BEND_LIBRARY_MANIFEST for producer integration")
	}
	dir := filepath.Join(t.TempDir(), "app")
	if err := NewModuleTarget(dir, "example.com/app", Schema, 1, "fogo,solana"); err != nil {
		t.Fatal(err)
	}
	added, err := AddBendLibrary(dir, "rules", "Evaluate", []string{manifest})
	if err != nil || len(added) != 1 || added[0] != "fogo" {
		t.Fatal(added, err)
	}
	c, err := Load(dir)
	if err != nil || c.Libraries["rules"].Bend["fogo"].SHA256 != os.Getenv("BEND_LIBRARY_SHA256") {
		t.Fatal(c.Libraries, err)
	}
	if err = Generate(dir); err != nil {
		t.Fatal(err)
	}
}
