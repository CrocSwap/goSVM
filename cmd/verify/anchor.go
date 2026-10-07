package main

import (
	"context"
	"encoding/json"
	"fmt"
	"gosvm/internal/sbftest"
	"os"
	"path/filepath"
)

func verificationBuild() string {
	if dir := os.Getenv("GOSVM_VERIFY_BUILD_DIR"); dir != "" {
		return dir
	}
	return "build"
}

func runAnchorBounded() error {
	data, e := os.ReadFile("examples/typed-swap/testdata/sbf.json")
	if e != nil {
		return e
	}
	for _, backend := range []struct{ name, elf string }{{"go", "go-bounded.so"}, {"lean-rust", "anchor_lean_bounded.so"}, {"anchor", "anchor_bounded_bench.so"}} {
		var suite sbftest.Suite
		if e = json.Unmarshal(data, &suite); e != nil {
			return e
		}
		if backend.name == "anchor" {
			codes := map[string]uint64{"owner": 3007, "readonly": 2000, "short state": 3003, "state discriminator": 3002, "instruction discriminator": 101}
			for i := range suite.Cases {
				if code, ok := codes[suite.Cases[i].Name]; ok {
					suite.Cases[i].Error = code
				}
			}
		}
		dir := filepath.Join(verificationBuild(), "anchor", "bounded-"+backend.name)
		if e = os.MkdirAll(filepath.Join(dir, "testdata"), 0755); e != nil {
			return e
		}
		encoded, _ := json.MarshalIndent(suite, "", "  ")
		if e = os.WriteFile(filepath.Join(dir, "testdata/sbf.json"), encoded, 0644); e != nil {
			return e
		}
		fmt.Println("Bounded framework comparison:", backend.name)
		if e = sbftest.Run(context.Background(), dir, filepath.Join(verificationBuild(), "anchor", backend.elf), os.Stdout); e != nil {
			return e
		}
	}
	return nil
}
