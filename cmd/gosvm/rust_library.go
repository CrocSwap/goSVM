package main

import (
	"flag"
	"fmt"
	"gosvm/internal/library"
	"sort"
	"strings"
)

// gosvm library build-rust: produce ABI v1 artifacts from a pure Rust crate.
func rustLibraryCommand(args []string) error {
	fs := flag.NewFlagSet("gosvm library build-rust", flag.ContinueOnError)
	crate := fs.String("crate", ".", "Rust crate directory with [package.metadata.gosvm-library]")
	output := fs.String("output", "", "new output directory (receives <profile>/library.manifest.json)")
	tools := fs.String("platform-tools", "", "platform-tools v1.57 root (default: gosvm toolchain install -rust tree, else ~/.cache/solana/v1.57/platform-tools)")
	targets := fs.String("targets", "fogo-v0,solana-v3", "comma-separated target profiles")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || *output == "" {
		return fmt.Errorf("usage: gosvm library build-rust -crate dir -output new-dir")
	}
	r, err := library.BuildRust(library.RustOptions{Crate: *crate, Output: *output, PlatformTools: *tools, Targets: strings.Split(*targets, ",")})
	if err != nil {
		return err
	}
	fmt.Printf("Built Rust library %s (symbol %s, %d -> %d bytes)\n", r.Root, r.Symbol, r.InputBytes, r.OutputBytes)
	var profiles []string
	for p := range r.Manifests {
		profiles = append(profiles, p)
	}
	sort.Strings(profiles)
	for _, p := range profiles {
		fmt.Printf("  %s: %s\n", p, r.Manifests[p])
	}
	return nil
}
