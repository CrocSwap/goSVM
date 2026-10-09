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

func TestNetworkTargetsResolveAndValidate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "app")
	if err := NewModuleTarget(dir, "example.com/app", Schema, 1, "fogo,solana"); err != nil {
		t.Fatal(err)
	}
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	ps, err := c.TargetProfiles()
	if err != nil || len(ps) != 2 || ps[0].Name != target.Fogo || ps[1].Name != target.Solana {
		t.Fatal(ps, err)
	}
	if ps[0].Network() != "fogo" || ps[1].Network() != "solana" {
		t.Fatal(ps[0].Network(), ps[1].Network())
	}
	if c.Target != "" || c.SBF != "" || c.PlatformTools != target.ModernTools {
		t.Fatalf("multi-target config records a single ISA: %+v", c)
	}
	for _, bad := range []Config{
		{Targets: []string{"fogo", "fogo-v0"}, PlatformTools: target.ModernTools},
		{Targets: []string{"fogo", target.Legacy}, PlatformTools: target.ModernTools},
		{Targets: []string{"fogo"}, PlatformTools: target.ModernTools, SBF: "v0"},
		{Targets: []string{"solana"}, PlatformTools: Tools},
		{Targets: []string{"solana"}, Target: target.Solana, PlatformTools: target.ModernTools},
		{Targets: []string{"eclipse"}, PlatformTools: target.ModernTools},
	} {
		if _, err := bad.TargetProfiles(); err == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
	// A single network name keeps the explicit single-profile form.
	if err = MigrateTarget(dir, "solana"); err != nil {
		t.Fatal(err)
	}
	if c, err = Load(dir); err != nil || c.Target != target.Solana || c.SBF != "v3" || len(c.Targets) != 0 {
		t.Fatal(c, err)
	}
}
