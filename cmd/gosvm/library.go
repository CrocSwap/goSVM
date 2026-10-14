package main

import (
	"flag"
	"fmt"
	"strings"

	"gosvm/internal/library"
	"gosvm/internal/project"
)

const libraryAddUsage = "usage: gosvm library add -name <package> [-function F] (-rust crate-dir | -bend-source PROOF.bend -root ns.fn [-bend-checkout dir] | bend-library.manifest.json...)"

// libraryAdd records Bend producer manifests, a Bend source or a Rust crate in gosvm.json and
// generates libs/<name>.
func libraryAdd(args []string) error {
	fs := flag.NewFlagSet("gosvm library add", flag.ContinueOnError)
	name := fs.String("name", "", "Go package name; the library is imported as <module>/libs/<name>")
	function := fs.String("function", "", "exported Go function name (default Evaluate for a new library)")
	dir := fs.String("dir", ".", "project directory")
	crate := fs.String("rust", "", "Rust crate directory with [package.metadata.gosvm-library], built for every target")
	bendSource := fs.String("bend-source", "", "Bend PROOF.bend file, built by the Bend checkout's build:library for every target")
	bendRoot := fs.String("root", "", "checked Bend root (namespace.function) for -bend-source")
	bendCheckout := fs.String("bend-checkout", "", "bendSVM checkout that builds -bend-source (default $"+library.BendCheckoutEnv+")")
	if err := fs.Parse(args); err != nil {
		return err
	}
	forms := 0
	for _, given := range []bool{fs.NArg() != 0, *crate != "", *bendSource != ""} {
		if given {
			forms++
		}
	}
	if *name == "" || forms != 1 || (*bendSource == "") != (*bendRoot == "") || (*bendCheckout != "" && *bendSource == "") {
		return fmt.Errorf("%s", libraryAddUsage)
	}
	root, err := project.Find(*dir)
	if err != nil {
		return err
	}
	c, err := project.Load(root)
	if err != nil {
		return err
	}
	if *function == "" && c.Libraries[*name].Function == "" {
		*function = "Evaluate"
	}
	if *bendSource != "" {
		if err = project.AddBendSourceLibrary(root, *name, *function, *bendSource, *bendRoot, *bendCheckout); err != nil {
			return err
		}
		fmt.Printf("Bound Bend %s from %s for every target; import %q\n", *bendRoot, *bendSource, c.Module+"/libs/"+*name)
		return nil
	}
	if *crate != "" {
		if err = project.AddRustLibrary(root, *name, *function, *crate); err != nil {
			return err
		}
		fmt.Printf("Bound Rust crate %s for every target; import %q\n", *crate, c.Module+"/libs/"+*name)
		return nil
	}
	added, err := project.AddBendLibrary(root, *name, *function, fs.Args())
	if err != nil {
		return err
	}
	fmt.Printf("Bound %s for %s; import %q\n", *name, strings.Join(added, ", "), c.Module+"/libs/"+*name)
	return nil
}

func libraryCommand(args []string) error {
	if len(args) > 0 && args[0] == "add" {
		return libraryAdd(args[1:])
	}
	if len(args) > 0 && args[0] == "build-rust" {
		return rustLibraryCommand(args[1:])
	}
	if len(args) == 0 || args[0] != "bind" {
		return fmt.Errorf("%s\n       gosvm library bind -manifest contract.json -output package-directory", libraryAddUsage)
	}
	fs := flag.NewFlagSet("gosvm library bind", flag.ContinueOnError)
	digest := fs.String("sha256", "", "pinned producer manifest SHA-256 (required)")
	pkg := fs.String("package", "bend", "generated Go package name")
	function := fs.String("function", "Evaluate", "exported Go function name")
	tools := fs.String("allow-library-tools", "", "explicit producer pin to qualify with the Go compiler (v1.53 or v1.57 for fogo-v0, v1.57 for solana-v3)")
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
