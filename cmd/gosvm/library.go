package main

import (
	"flag"
	"fmt"
	"gosvm/internal/library"
)

func libraryCommand(args []string) error {
	if len(args) > 0 && args[0] == "build-rust" {
		return rustLibraryCommand(args[1:])
	}
	if len(args) == 0 || args[0] != "bind" {
		return fmt.Errorf("usage: gosvm library bind -manifest contract.json -output package-directory")
	}
	fs := flag.NewFlagSet("gosvm library bind", flag.ContinueOnError)
	digest := fs.String("sha256", "", "pinned producer manifest SHA-256 (required)")
	pkg := fs.String("package", "bend", "generated Go package name")
	function := fs.String("function", "Evaluate", "exported Go function name")
	tools := fs.String("allow-library-tools", "", "explicit producer pin to qualify with the Go compiler (currently v1.53)")
	manifest := fs.String("manifest", "", "agreed Bend producer ABI v1 manifest")
	output := fs.String("output", "", "new generated Go package directory")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 || *manifest == "" || *output == "" {
		return fmt.Errorf("require -manifest and -output")
	}
	if err := library.GenerateBend(*manifest, *digest, *pkg, *function, *tools, *output); err != nil {
		return err
	}
	fmt.Println("Generated matching SBF/native Go Bend-library binding:", *output)
	return nil
}
