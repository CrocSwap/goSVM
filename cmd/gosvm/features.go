package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gosvm/internal/compiler"
	"gosvm/internal/features"
	"gosvm/internal/project"
)

// featuresCommand reports Go language and SDK usage as JSON. With a positional
// package directory (or file) it reads that package directly; otherwise it
// reads the discovered project, including its existing generated adapter.
func featuresCommand(args []string) error {
	fs := flag.NewFlagSet("gosvm features", flag.ContinueOnError)
	dir := fs.String("dir", ".", "project directory")
	output := fs.String("o", "", "output JSON file (default stdout)")
	generate := fs.Bool("generate", false, "run gosvm generate before reporting (default: read existing generated files)")
	var trim trimFlag
	fs.Var(&trim, "trim", "low-level form: opt in to trimming for packages matching this pattern (repeatable; docs/GO_PACKAGES.md#trimming)")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if fs.NArg() > 1 {
		return fmt.Errorf("usage: gosvm features [-dir project] [-generate] [-o out.json] [-trim pattern]... [package-directory|source.go]")
	}
	var usage *features.Usage
	var err error
	if fs.NArg() == 1 {
		if *generate {
			return fmt.Errorf("-generate applies to projects; omit the package directory")
		}
		usage, err = packageFeatures(fs.Arg(0), trim)
	} else {
		if len(trim) > 0 {
			return fmt.Errorf("-trim applies to the low-level form; projects set \"trim\" in gosvm.json")
		}
		usage, err = projectFeatures(*dir, *generate)
	}
	if err != nil {
		return err
	}
	b, err := features.Marshal(usage)
	if err != nil {
		return err
	}
	if *output == "" {
		_, err = os.Stdout.Write(b)
		return err
	}
	if err = os.MkdirAll(filepath.Dir(*output), 0755); err != nil {
		return err
	}
	return os.WriteFile(*output, b, 0644)
}

func packageFeatures(path string, trim []string) (*features.Usage, error) {
	program, err := compiler.ReadProgramWithOptions(path, compiler.ReadOptions{Trim: trim})
	if err != nil {
		return nil, err
	}
	tp, err := compiler.TypeCheckProgram(program)
	if err != nil {
		return nil, err
	}
	u, err := features.Report(tp)
	if err != nil {
		return nil, err
	}
	checked, checkErr := compiler.Check(program)
	features.ApplyCheck(u, checked, checkErr)
	u.Program.Module = goModule(path)
	return u, nil
}

func projectFeatures(dir string, generate bool) (*features.Usage, error) {
	root, err := project.Find(dir)
	if err != nil {
		return nil, err
	}
	config, err := project.Load(root)
	if err != nil {
		return nil, err
	}
	if generate {
		if err = project.Generate(root); err != nil {
			return nil, err
		}
	}
	profiles, err := config.TargetProfiles()
	if err != nil {
		return nil, err
	}
	u, err := packageFeatures(root, config.Trim)
	if err != nil {
		return nil, err
	}
	u.Program.Module = config.Module
	for _, p := range profiles {
		u.Program.Targets = append(u.Program.Targets, features.Target{Network: p.Network(), Profile: p.Name})
	}
	return u, nil
}

// goModule returns the module path declared by the nearest go.mod, if any.
func goModule(path string) string {
	dir, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	if st, err := os.Stat(dir); err == nil && !st.IsDir() {
		dir = filepath.Dir(dir)
	}
	for {
		if f, err := os.Open(filepath.Join(dir, "go.mod")); err == nil {
			defer f.Close()
			s := bufio.NewScanner(f)
			for s.Scan() {
				if fields := strings.Fields(s.Text()); len(fields) >= 2 && fields[0] == "module" {
					return strings.Trim(fields[1], `"`)
				}
			}
			return ""
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
