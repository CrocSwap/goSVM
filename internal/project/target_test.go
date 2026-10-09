package project

import (
	"bytes"
	"encoding/json"
	target "gosvm/internal/targets"
	"os"
	"path/filepath"
	"testing"
)

func TestExplicitTargetMigrationPreservesApplicationAndWire(t *testing.T) {
	for _, schema := range []int{Schema, MultiSchema} {
		dir := filepath.Join(t.TempDir(), "app")
		if err := NewModuleSDK(dir, "example.com/app", schema, 1); err != nil {
			t.Fatal(err)
		}
		before := map[string][]byte{}
		filepath.WalkDir(dir, func(path string, e os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !e.IsDir() && filepath.Base(path) != "gosvm.json" {
				before[path], err = os.ReadFile(path)
			}
			return err
		})
		old, err := Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		p, err := old.TargetProfile()
		if err != nil || p.Name != target.Legacy {
			t.Fatal("historical target reinterpreted", p, err)
		}
		for _, name := range []string{target.Fogo, target.Solana, target.Legacy} {
			if err = MigrateTarget(dir, name); err != nil {
				t.Fatal(err)
			}
			if err = Generate(dir); err != nil {
				t.Fatal(err)
			}
			now, err := Load(dir)
			if err != nil {
				t.Fatal(err)
			}
			got, err := now.TargetProfile()
			if err != nil || got.Name != name {
				t.Fatal(got, err)
			}
			for path, want := range before {
				data, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(data, want) {
					t.Fatalf("migration changed %s: %v", path, err)
				}
			}
		}
		data, _ := os.ReadFile(filepath.Join(dir, "gosvm.json"))
		var config map[string]any
		json.Unmarshal(data, &config)
		config["target"] = target.Solana
		config["platform_tools"] = Tools
		data, _ = json.Marshal(config)
		os.WriteFile(filepath.Join(dir, "gosvm.json"), data, 0644)
		if _, err = Load(dir); err == nil {
			t.Fatal("accepted modern target with historical toolchain")
		}
	}
}
