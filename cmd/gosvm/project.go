package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"gosvm/internal/compiler"
	"gosvm/internal/project"
	"gosvm/internal/sbftest"
	"gosvm/internal/targets"
	"gosvm/internal/testvm"
	"gosvm/internal/toolchain"
)

const help = `gosvm experimental project tools

  gosvm library bind       Generate Go/native bindings from a pinned pure-library contract
  gosvm new <directory>    Create a typed bounded-swap program and tests
  gosvm toolchain install Import the content-pinned modern backend; legacy downloads remain available
  gosvm toolchain status  Verify the managed backend
  gosvm runner install    Verify and install a pinned local runner package
  gosvm runner status     Verify the managed experimental runner
  gosvm doctor            Check Go, pinned LLVM, and optional validator
  gosvm generate          Generate account checks, codecs, client, and IDL
  gosvm check             Generate and check the on-chain Go subset (no LLVM)
  gosvm migrate-target    Explicitly migrate the target/toolchain configuration
  gosvm build             Generate and build every configured target
  gosvm test              Generate and run ordinary native Go tests
  gosvm test --sbf        Also run byte-exact local-validator fixtures and CU
  gosvm test --svm        Also run fast compiled-ELF fixtures with an experimental runner
  gosvm svm-test          Run general fixtures against an already-compiled ELF

Projects are discovered from the current directory or a parent.
new accepts -module <import/path> and -schema 2 for the multi-state prototype.
test forwards Go flags after --, for example:
  gosvm test -- -run TestSwap -count=1
Project commands accept -dir <directory>. Build/test accept -llvm <directory>
and -no-cache. New projects target both fogo and solana; build and test --svm
cover every configured target. -target fogo|solana selects one for a command.
SBF_LLVM overrides the selected profile backend directory.
Build/test accept -timings to print JSON phase durations for workflow diagnostics.
Fast tests accept -svm-run <fixture regex> and -svm-runner <binary path>.
Runner selection: explicit flag, GOSVM_TEST_RUNNER, verified managed cache, PATH.
Use -svm-sysvars <JSON file> for explicit Clock/Rent values in fast tests.
Use -svm-fixtures <JSON file> for general multi-account project fixtures.
Programs are copied to build/fogo/program.so and build/solana/program.so;
isolated outputs/reports are under build/<profile>/<tools>/<backend>/deps-<dependencies>/.
Historical outputs retain build/program.so and build/svm-results.json.
Low level: gosvm [-target <profile>] [-o program.so] [-emit-c] source.go|directory
Raw -arch v0|v3 retains historical low-level semantics.
`

func llvmDefault() string {
	if p := os.Getenv("SBF_LLVM"); p != "" {
		return p
	}
	if toolchain.Available() {
		return toolchain.LLVM()
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache/solana", project.Tools, "platform-tools/llvm")
}
func backendCheck(llvm string) error {
	for _, tool := range []string{"clang", "ld.lld"} {
		b, e := exec.Command(filepath.Join(llvm, "bin", tool), "--version").CombinedOutput()
		if e != nil {
			return fmt.Errorf("backend %s unavailable at %s; run gosvm toolchain install (macOS arm64), or set SBF_LLVM to platform-tools %s: %w", tool, llvm, project.Tools, e)
		}
		if !strings.Contains(string(b), "19.1.7") {
			return fmt.Errorf("backend %s is not the expected platform-tools %s LLVM 19.1.7: %s", tool, project.Tools, b)
		}
	}
	return nil
}
func projectCommand(args []string) error {
	command := args[0]
	if command == "library" {
		return libraryCommand(args[1:])
	}
	if command == "runner" {
		return runnerCommand(args[1:])
	}
	if command == "svm-test" {
		return svmTestCommand(args[1:])
	}
	if command == "toolchain" {
		return toolchainCommand(args[1:])
	}
	if command == "help" {
		fmt.Print(help)
		return nil
	}
	if command == "migrate-target" {
		fs := flag.NewFlagSet("gosvm migrate-target", flag.ContinueOnError)
		name := fs.String("target", "", "destination networks (fogo,solana) or profile")
		dir := fs.String("dir", ".", "project directory")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 0 || *name == "" {
			return fmt.Errorf("usage: gosvm migrate-target -target fogo,solana|fogo|solana|legacy-v1.51-v3 [-dir project]")
		}
		root, err := project.Find(*dir)
		if err != nil {
			return err
		}
		if err = project.MigrateTarget(root, *name); err != nil {
			return err
		}
		fmt.Println("Migrated target configuration; application source, layouts and clients unchanged")
		return nil
	}
	if command == "version" {
		fmt.Println("gosvm experimental (fogo = fogo-v0, solana = solana-v3; legacy-v1.51-v3 retained)")
		return nil
	}
	if command == "new" {
		fs := flag.NewFlagSet("gosvm new", flag.ContinueOnError)
		profileName := fs.String("target", "fogo,solana", "networks to target: fogo, solana, or both (comma-separated)")
		module := fs.String("module", "", "Go module import path (default example.com/<name>)")
		schema := fs.Int("schema", project.Schema, "project schema (1 bounded swap; 2 multi-state prototype)")
		sdkVersion := fs.Int("sdk", 1, "SDK version (1 legacy; 2 bounded CPI/PDA, requires schema 2)")
		if e := fs.Parse(args[1:]); e != nil {
			if e == flag.ErrHelp {
				return nil
			}
			return e
		}
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: gosvm new [-module example.org/my/swap] [-schema 1|2] <directory>")
		}
		if e := project.NewModuleTarget(fs.Arg(0), *module, *schema, *sdkVersion, *profileName); e != nil {
			return e
		}
		fmt.Printf("Created %s\nNext: cd %q && gosvm check && go test ./... && gosvm build\n", fs.Arg(0), fs.Arg(0))
		return nil
	}
	fs := flag.NewFlagSet("gosvm "+command, flag.ContinueOnError)
	profileName := fs.String("target", "", "explicit target override (does not modify configuration)")
	dir := fs.String("dir", ".", "project directory")
	llvmValue := llvmDefault()
	sbfValue, noCacheValue := false, false
	svmValue := false
	showTimings := false
	svmPattern, svmRunner, svmSysvars, svmFixtures := "", "", "", ""
	llvm, sbf, noCache := &llvmValue, &sbfValue, &noCacheValue
	if command == "build" || command == "test" || command == "doctor" {
		fs.StringVar(llvm, "llvm", *llvm, "platform-tools/llvm directory")
	}
	if command == "build" || command == "test" {
		fs.BoolVar(noCache, "no-cache", false, "force LLVM rebuild")
		fs.BoolVar(&showTimings, "timings", false, "print JSON phase durations")
	}
	if command == "test" {
		fs.BoolVar(sbf, "sbf", false, "also test compiled ELF on a local validator")
		fs.BoolVar(&svmValue, "svm", false, "also test compiled ELF with a fast SVM runner")
		fs.StringVar(&svmPattern, "svm-run", "", "fixture name regular expression (requires -svm)")
		fs.StringVar(&svmRunner, "svm-runner", "", "runner binary (or GOSVM_TEST_RUNNER/PATH)")
		fs.StringVar(&svmSysvars, "svm-sysvars", "", "explicit clock/rent JSON file (requires -svm)")
		fs.StringVar(&svmFixtures, "svm-fixtures", "", "general multi-account fixture JSON file (requires -svm)")
	}
	parseArgs := args[1:]
	var testArgs []string
	for i, a := range parseArgs {
		if a == "--" {
			if command != "test" {
				return fmt.Errorf("only test accepts arguments after --")
			}
			testArgs = parseArgs[i+1:]
			parseArgs = parseArgs[:i]
			break
		}
	}
	if e := fs.Parse(parseArgs); e != nil {
		if e == flag.ErrHelp {
			return nil
		}
		return e
	}
	if *sbf && svmValue {
		return fmt.Errorf("choose either -sbf or -svm")
	}
	if !svmValue && (svmPattern != "" || svmRunner != "" || svmSysvars != "" || svmFixtures != "") {
		return fmt.Errorf("-svm-run, -svm-runner, -svm-sysvars, and -svm-fixtures require -svm")
	}
	if svmFixtures != "" && svmSysvars != "" {
		return fmt.Errorf("general fixtures declare sysvars in their JSON; choose -svm-fixtures or -svm-sysvars")
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v; put Go test flags after --", fs.Args())
	}
	resolved, findErr := project.Find(*dir)
	if findErr == nil {
		*dir = resolved
	} else if command != "doctor" {
		return findErr
	}
	if svmValue {
		if err := os.Remove(filepath.Join(*dir, "build/svm-results.json")); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	var profiles []target.Profile
	if findErr == nil {
		config, err := project.Load(*dir)
		if err != nil {
			return err
		}
		if profiles, err = config.TargetProfiles(); err != nil {
			return err
		}
	} else {
		p, _ := target.Resolve(target.Legacy)
		profiles = []target.Profile{p}
	}
	if *profileName != "" {
		p, err := target.Resolve(*profileName)
		if err != nil {
			return err
		}
		profiles = []target.Profile{p}
	}
	if *sbf && len(profiles) > 1 {
		return fmt.Errorf("validator tests (-sbf) run one target; add -target fogo or -target solana")
	}
	explicitLLVM := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "llvm" {
			explicitLLVM = true
		}
	})
	defaultLLVM := *llvm
	llvmFor := func(p target.Profile) string {
		if explicitLLVM || p.Tools == project.Tools {
			return defaultLLVM
		}
		return llvmForTools(p.Tools)
	}
	selected := profiles[0]
	*llvm = llvmFor(selected)
	building := command == "build" || command == "test" && (*sbf || svmValue)
	outputDirs := map[string]string{}
	for _, p := range profiles {
		outputDir := filepath.Join(*dir, "build")
		if p.Name != target.Legacy && building {
			var err error
			if outputDir, err = p.OutputDir(*dir, llvmFor(p)); err != nil {
				return err
			}
		}
		outputDirs[p.Name] = outputDir
		if svmValue {
			// Clear before generation/native tests/build, as any of them can fail.
			if err := os.Remove(filepath.Join(outputDir, "svm-results.json")); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	if command == "doctor" {
		failed := false
		for _, tool := range []struct {
			name     string
			args     []string
			optional bool
		}{{"go", []string{"version"}, false}, {"solana-test-validator", []string{"--version"}, true}} {
			b, e := exec.Command(tool.name, tool.args...).CombinedOutput()
			if e != nil {
				fmt.Printf("MISSING %s", tool.name)
				if tool.optional {
					fmt.Print(" (only needed for test --sbf)")
				} else {
					failed = true
				}
				fmt.Println()
				continue
			}
			fmt.Println(strings.TrimSpace(string(b)))
			if tool.optional && !strings.Contains(string(b), " "+project.Validator+" ") {
				fmt.Printf("Expected validator %s for SBF tests\n", project.Validator)
			}
		}
		if path, e := testvm.RunnerPath(""); e != nil {
			fmt.Printf("SVM runner unavailable (optional; only needed for test --svm): %v\n", e)
		} else {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			version, e := exec.CommandContext(ctx, path, "--version").CombinedOutput()
			cancel()
			if e != nil || !testvm.RunnerCompatible(strings.TrimSpace(string(version)), selected.Name) {
				fmt.Printf("SVM runner incompatible (optional): %s: %v %s\n", path, e, version)
			} else {
				fmt.Printf("OK optional SVM runner: %s (%s)\n", path, strings.TrimSpace(string(version)))
			}
		}
		if e := selected.CheckBackend(*llvm); e != nil {
			fmt.Println(e)
			failed = true
		} else {
			fmt.Printf("OK %s / platform-tools %s / LLVM %s: %s\n", selected.Name, selected.Tools, selected.LLVM, *llvm)
		}
		if _, e := os.Stat(filepath.Join(*dir, "gosvm.json")); e == nil {
			if _, e = project.Load(*dir); e != nil {
				return e
			}
			fmt.Println("OK project manifest and SDK snapshot")
		}
		if filepath.Clean(*llvm) == filepath.Clean(toolchain.LLVM()) && toolchain.Available() {
			if _, e := toolchain.Verify(toolchain.Root()); e != nil {
				return e
			}
			fmt.Println("OK managed backend content hashes")
		}
		if failed {
			return fmt.Errorf("required tools missing; see diagnostics above")
		}
		return nil
	}
	start := time.Now()
	timings := commandTimings{Enabled: showTimings, Schema: 1, Command: command, Phases: map[string]float64{}}
	if showTimings {
		defer timings.emit(os.Stdout, start)
	}
	if command == "generate" || command == "check" {
		// Generate validates the full package before replacing any generated files.
		if e := project.Generate(*dir); e != nil {
			return e
		}
		fmt.Printf("%s OK (%s)\n", command, time.Since(start).Round(time.Millisecond))
		return nil
	}
	if e := timings.measure("manifest", func() error { _, e := project.Load(*dir); return e }); e != nil {
		return e
	}
	if e := timings.measure("generation", func() error { return project.Generate(*dir) }); e != nil {
		return e
	}
	if building {
		key, err := compiler.DependencyKey(*dir)
		if err != nil {
			return err
		}
		for _, p := range profiles {
			if p.Name == target.Legacy {
				continue
			}
			outputDirs[p.Name] = filepath.Join(outputDirs[p.Name], "deps-"+key[:16])
			if svmValue {
				if err := os.Remove(filepath.Join(outputDirs[p.Name], "svm-results.json")); err != nil && !os.IsNotExist(err) {
					return err
				}
			}
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if command == "test" {
		cmd := exec.CommandContext(ctx, "go", append([]string{"test", "./..."}, testArgs...)...)
		cmd.Dir = *dir
		cmd.Env = append(os.Environ(), "GOWORK=off")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if e := timings.measure("native_tests", cmd.Run); e != nil {
			return e
		}
		if !*sbf && !svmValue {
			timings.Passed = true
			return nil
		}
	}
	var failed []string
	for _, p := range profiles {
		phase := func(name string) string {
			if len(profiles) > 1 {
				return p.Network() + "." + name
			}
			return name
		}
		if e := buildTarget(ctx, *dir, p, llvmFor(p), outputDirs[p.Name], phase, &timings, *noCache, *sbf, svmValue, start,
			sbftest.FastOptions{Runner: svmRunner, Pattern: svmPattern, Sysvars: svmSysvars, Fixtures: svmFixtures}); e != nil {
			if len(profiles) == 1 {
				return e
			}
			fmt.Fprintf(os.Stderr, "%s: %v\n", p.Network(), e)
			failed = append(failed, p.Network())
		}
	}
	if len(failed) != 0 {
		return fmt.Errorf("failed targets: %s", strings.Join(failed, ", "))
	}
	timings.Passed = true
	return nil
}

// buildTarget compiles one profile into its isolated output directory, publishes
// a stable build/<network>/program.so copy, then runs any requested SBF tests.
func buildTarget(ctx context.Context, dir string, p target.Profile, llvm, outputDir string, phase func(string) string, timings *commandTimings, noCache, sbf, svm bool, start time.Time, opts sbftest.FastOptions) error {
	if e := timings.measure(phase("backend_check"), func() error { return p.CheckBackend(llvm) }); e != nil {
		return e
	}
	output := filepath.Join(outputDir, "program.so")
	e := timings.measure(phase("sbf_build"), func() error {
		if noCache {
			return compiler.Build(dir, output, llvm, p.Name)
		}
		return compiler.BuildCached(dir, output, llvm, p.Name)
	})
	if e != nil {
		return e
	}
	st, e := os.Stat(output)
	if e != nil {
		return e
	}
	fmt.Printf("Built %s (%d bytes, %s)\n", output, st.Size(), time.Since(start).Round(time.Millisecond))
	if network := p.Network(); network != "" {
		stable := filepath.Join(dir, "build", network, "program.so")
		if e = copyAtomic(output, stable); e != nil {
			return e
		}
		fmt.Printf("%s program: %s\n", network, stable)
	}
	if svm {
		opts.Profile, opts.PlatformTools, opts.ReportPath = p.Name, p.Tools, filepath.Join(outputDir, "svm-results.json")
		e = timings.measure(phase("svm_tests"), func() error { return sbftest.RunFast(ctx, dir, output, os.Stdout, opts) })
	}
	if sbf {
		e = timings.measure(phase("validator_tests"), func() error {
			return sbftest.RunProfileTo(ctx, dir, output, filepath.Join(outputDir, "sbf-results.json"), os.Stdout, p.Name)
		})
	}
	return e
}

func copyAtomic(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(dst), ".program-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Chmod(f.Name(), 0644); err != nil {
		return err
	}
	return os.Rename(f.Name(), dst)
}

func toolchainCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: gosvm toolchain install [-archive file.tar.bz2] | status")
	}
	fs := flag.NewFlagSet("gosvm toolchain "+args[0], flag.ContinueOnError)
	profileName := fs.String("target", target.Legacy, "backend profile (legacy retains official archive installer)")
	localLLVM := fs.String("llvm", "", "existing qualified modern backend directory to import")
	archive := ""
	if args[0] == "install" {
		fs.StringVar(&archive, "archive", "", "use a local official archive (same SHA-256 verification)")
	}
	if e := fs.Parse(args[1:]); e != nil {
		if e == flag.ErrHelp {
			return nil
		}
		return e
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected toolchain arguments")
	}
	p, err := target.Resolve(*profileName)
	if err != nil {
		return err
	}
	if p.Name != target.Legacy {
		switch args[0] {
		case "status":
			if err = toolchain.VerifyModern(toolchain.ModernRoot()); err != nil {
				return err
			}
			fmt.Println("Verified platform-tools", p.Tools, toolchain.ModernRoot())
			return nil
		case "install":
			if archive != "" {
				return fmt.Errorf("modern archive downloads are not qualified; use -llvm with an existing content-pinned v1.57 backend")
			}
			if *localLLVM == "" {
				*localLLVM = llvmForTools(p.Tools)
			}
			return toolchain.InstallModern(context.Background(), toolchain.ModernRoot(), *localLLVM, os.Stdout)
		default:
			return fmt.Errorf("unknown toolchain command %q", args[0])
		}
	}
	if *localLLVM != "" {
		return fmt.Errorf("-llvm import applies to modern profiles only")
	}
	switch args[0] {
	case "status":
		if _, e := toolchain.Verify(toolchain.Root()); e != nil {
			return fmt.Errorf("managed toolchain unavailable: %w; run gosvm toolchain install", e)
		}
		fmt.Printf("Verified %s: %s\n", toolchain.Version, toolchain.LLVM())
		return nil
	case "install":
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		return toolchain.Install(ctx, toolchain.Root(), archive, os.Stdout, downloadBackend)
	default:
		return fmt.Errorf("unknown toolchain command %q", args[0])
	}
}

func downloadBackend(ctx context.Context, url string, out io.Writer) error {
	req, e := http.NewRequestWithContext(ctx, "GET", url, nil)
	if e != nil {
		return e
	}
	client := http.Client{Timeout: 15 * time.Minute}
	response, e := client.Do(req)
	if e != nil {
		return e
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("backend download: HTTP %s", response.Status)
	}
	_, e = io.Copy(out, response.Body)
	return e
}

func llvmForTools(version string) string {
	if p := os.Getenv("SBF_LLVM"); p != "" {
		return p
	}
	if version == project.Tools {
		return llvmDefault()
	}
	if _, err := os.Stat(filepath.Join(toolchain.ModernRoot(), "receipt.json")); err == nil {
		return filepath.Join(toolchain.ModernRoot(), "llvm")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache/solana", version, "platform-tools/llvm")
}
