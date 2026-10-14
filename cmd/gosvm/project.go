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
	"gosvm/internal/library"
	"gosvm/internal/project"
	"gosvm/internal/sbftest"
	"gosvm/internal/targets"
	"gosvm/internal/testvm"
	"gosvm/internal/toolchain"
)

const help = `gosvm experimental project tools

  gosvm library bind       Generate Go/native bindings from a pinned pure-library contract
  gosvm library build-rust Build a pure no_std Rust crate into ABI v1 library artifacts
  gosvm new <directory>    Create a typed bounded-swap program and tests
  gosvm toolchain install Download the pinned platform-tools v1.57 backend (fogo-v0, solana-v3)
                          (-archive local copy, -llvm import, -target legacy-v1.51-v3,
                          -rust also the full tree for Rust libraries)
  gosvm toolchain status  Verify the managed backend and the optional Rust toolchain
  gosvm runner install    Install a pinned local runner package: -archive <0.5.0 or 0.6.0 package>
  gosvm runner status     Verify the managed runners (0.6.0 solana-v3; 0.5.0 fogo-v0/legacy)
  gosvm doctor            Check Go, backends and runners for fogo-v0/solana-v3, with fixes
  gosvm generate          Generate account checks, codecs, client, and IDL
  gosvm check             Generate and check the on-chain Go subset (no LLVM)
  gosvm migrate-target    Explicitly migrate the target/toolchain configuration
  gosvm build             Generate and build every configured target
  gosvm test              Generate and run ordinary native Go tests
  gosvm test --sbf        Also run byte-exact local-validator fixtures and CU
  gosvm test --svm        Also run fast compiled-ELF fixtures with an experimental runner
  gosvm svm-test          Run general fixtures against an already-compiled ELF
  gosvm deploy -network N Check the cluster accepts the target, then deploy (fogo-testnet, solana-devnet, ...)
  gosvm call -network N   Simulate (or -send) an instruction to the deployed program; prints return data/CU/logs
  gosvm authority -network N  Show, transfer (-set key.json) or remove (-immutable) the upgrade authority
  gosvm features          Report Go language features and SDK calls as JSON (gosvm-features-v1)
                          (-o out.json, -generate to regenerate first, or a package directory)

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
Runner selection: explicit flag, GOSVM_TEST_RUNNER, verified managed runner for
the target (solana-v3 0.6.0; fogo-v0/legacy 0.5.0), PATH.
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
func projectCommand(args []string) error {
	command := args[0]
	if command == "library" {
		return libraryCommand(args[1:])
	}
	if command == "deploy" {
		return deployCommand(args[1:])
	}
	if command == "call" {
		return callCommand(args[1:])
	}
	if command == "authority" {
		return authorityCommand(args[1:])
	}
	if command == "features" {
		return featuresCommand(args[1:])
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
		// Report both deployable profiles; only the selected backend is required.
		profiles := []string{target.Fogo, target.Solana}
		if selected.Name == target.Legacy {
			profiles = append(profiles, target.Legacy)
		}
		for _, name := range profiles {
			p, _ := target.Resolve(name)
			dir := llvmForTools(p.Tools)
			if name == selected.Name {
				dir = *llvm
			}
			required := ""
			if name == selected.Name {
				required = " (selected)"
			}
			if e := p.CheckBackend(dir); e != nil {
				fmt.Printf("MISSING %s backend%s: %v\n  fix: %s\n", name, required, e, backendInstallCommand(p))
				if name == selected.Name {
					failed = true
				}
			} else {
				fmt.Printf("OK %s backend%s / platform-tools %s / LLVM %s: %s\n", name, required, p.Tools, p.LLVM, dir)
			}
			path, e := testvm.RunnerFor("", name)
			if e != nil {
				fmt.Printf("MISSING %s SVM runner (optional; only for test --svm): %v\n  fix: %s\n", name, e, testvm.RunnerInstallHint(name))
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			version, e := exec.CommandContext(ctx, path, "--version").CombinedOutput()
			cancel()
			if e != nil || !testvm.RunnerCompatible(strings.TrimSpace(string(version)), name) {
				fmt.Printf("INCOMPATIBLE %s SVM runner (optional): %s: %v %s\n  fix: %s\n", name, path, e, strings.TrimSpace(string(version)), testvm.RunnerInstallHint(name))
			} else {
				fmt.Printf("OK %s SVM runner: %s (%s)\n", name, path, strings.TrimSpace(string(version)))
			}
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
		if toolchain.ModernAvailable() {
			if r, e := toolchain.ReadModern(toolchain.ModernRoot()); e != nil {
				fmt.Printf("DAMAGED managed platform-tools %s backend: %v\n  fix: remove %s and run gosvm toolchain install\n", target.ModernTools, e, toolchain.ModernRoot())
			} else {
				fmt.Printf("OK managed platform-tools %s content pins (%s)\n", target.ModernTools, r.Source)
			}
		}
		fmt.Println(rustProducerStatus())
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
		return fmt.Errorf("usage: gosvm toolchain install [-archive file.tar.bz2 | -llvm dir] [-target legacy-v1.51-v3] [-rust] | status")
	}
	fs := flag.NewFlagSet("gosvm toolchain "+args[0], flag.ContinueOnError)
	profileName := fs.String("target", target.Solana, "fogo-v0 / solana-v3 (shared platform-tools v1.57 backend) or legacy-v1.51-v3")
	localLLVM := fs.String("llvm", "", "import Clang/LLD from an existing platform-tools v1.57 llvm directory instead of downloading")
	archive, rust := "", false
	if args[0] == "install" {
		fs.StringVar(&archive, "archive", "", "use a local copy of the official archive (same size/SHA-256/content verification)")
		fs.BoolVar(&rust, "rust", false, "also install the full platform-tools v1.57 tree for Rust libraries (one download)")
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
	if rust && (p.Name == target.Legacy || *localLLVM != "") {
		return fmt.Errorf("-rust installs the platform-tools %s tree from the official archive; it cannot be combined with -llvm or the legacy target", target.ModernTools)
	}
	if p.Name != target.Legacy {
		switch args[0] {
		case "status":
			fmt.Println(rustProducerStatus())
			r, err := toolchain.ReadModern(toolchain.ModernRoot())
			if err != nil {
				return fmt.Errorf("managed platform-tools %s backend (fogo-v0, solana-v3) unavailable: %w; run gosvm toolchain install", p.Tools, err)
			}
			fmt.Printf("Verified platform-tools %s for fogo-v0 and solana-v3 (%s): %s\n", p.Tools, r.Source, toolchain.ModernRoot())
			return nil
		case "install":
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
			defer stop()
			if rust {
				return toolchain.InstallRustArchive(ctx, archive, os.Stdout, downloadBackend)
			}
			if *localLLVM != "" {
				if archive != "" {
					return fmt.Errorf("choose either -archive or -llvm")
				}
				return toolchain.InstallModern(ctx, toolchain.ModernRoot(), *localLLVM, os.Stdout)
			}
			return toolchain.InstallModernArchive(ctx, toolchain.ModernRoot(), archive, os.Stdout, downloadBackend)
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
			return fmt.Errorf("managed legacy toolchain unavailable: %w; run gosvm toolchain install -target legacy-v1.51-v3", e)
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

// rustProducerStatus describes the optional Rust library producer toolchain.
func rustProducerStatus() string {
	const optional = "optional; only needed for Rust libraries"
	if toolchain.RustInstalled() {
		if _, e := toolchain.ReadRust(toolchain.RustRoot()); e != nil {
			return fmt.Sprintf("DAMAGED managed Rust library producer toolchain (%s): %v\n  fix: remove %s and run %s", optional, e, toolchain.RustRoot(), library.RustInstallCommand)
		}
		return fmt.Sprintf("OK Rust library producer: managed platform-tools %s tree: %s", target.ModernTools, toolchain.RustPlatformTools())
	}
	if root := library.DefaultPlatformTools(); library.CheckRustTools(root) == nil {
		return fmt.Sprintf("OK Rust library producer: platform-tools %s (unmanaged): %s", target.ModernTools, root)
	}
	return fmt.Sprintf("MISSING Rust library producer toolchain (%s)\n  fix: %s", optional, library.RustInstallCommand)
}

// backendInstallCommand is the exact command that installs a profile's backend.
func backendInstallCommand(p target.Profile) string {
	if p.Tools == target.ModernTools {
		return "gosvm toolchain install"
	}
	return "gosvm toolchain install -target " + target.Legacy
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
