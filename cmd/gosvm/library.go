package main

import (
	"flag"
	"fmt"
	"strings"

	"gosvm/internal/library"
	"gosvm/internal/project"
)

// libraryAdd records Bend producer manifests in gosvm.json and generates libs/<name>.
func libraryAdd(args []string) error {
	fs := flag.NewFlagSet("gosvm library add", flag.ContinueOnError)
	name := fs.String("name", "", "Go package name; the library is imported as <module>/libs/<name>")
	function := fs.String("function", "", "exported Go function name (default Evaluate for a new library)")
	dir := fs.String("dir", ".", "project directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *name == "" || fs.NArg() == 0 {
		return fmt.Errorf("usage: gosvm library add -name <package> [-function F] library.manifest.json...")
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
		return fmt.Errorf("usage: gosvm library add -name <package> [-function F] library.manifest.json...\n       gosvm library bind -manifest contract.json -output package-directory")
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
