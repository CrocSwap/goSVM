package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"gosvm/internal/features"
)

func TestFeaturesCommandProjectAndPackage(t *testing.T) {
	example := filepath.Join("..", "..", "examples", "full-swap")
	dir := t.TempDir()
	projectOut, packageOut := filepath.Join(dir, "project.json"), filepath.Join(dir, "package.json")
	if err := projectCommand([]string{"features", "-dir", example, "-o", projectOut}); err != nil {
		t.Fatal(err)
	}
	if err := projectCommand([]string{"features", "-o", packageOut, example}); err != nil {
		t.Fatal(err)
	}
	var fromProject, fromPackage features.Usage
	for path, u := range map[string]*features.Usage{projectOut: &fromProject, packageOut: &fromPackage} {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(b, u); err != nil {
			t.Fatal(err)
		}
	}
	if fromProject.Schema != features.Schema || fromProject.Program.Module != "example.org/gosvm/full-swap" || len(fromProject.Program.Targets) != 1 || fromProject.Program.Targets[0].Profile != "legacy-v1.51-v3" {
		t.Fatalf("project identity: %+v", fromProject.Program)
	}
	if fromPackage.Program.Module != fromProject.Program.Module || len(fromPackage.Program.Targets) != 0 || len(fromPackage.SDKCalls) != len(fromProject.SDKCalls) {
		t.Fatalf("package form: %+v", fromPackage.Program)
	}
}
