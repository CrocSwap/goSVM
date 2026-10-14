package library

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	target "gosvm/internal/targets"
)

// BendDependencyCommit is the Bend language commit Bend's library build
// requires in <checkout>/.deps/bend. GoSVM checks it but never fetches it.
const BendDependencyCommit = "7561656155a4285c1e4ccfcb3505ab59524de973"

// BendCheckoutEnv names a bendSVM checkout when a declaration records none.
const BendCheckoutEnv = "GOSVM_BEND"

// BendCheckout is the state of a bendSVM checkout that identifies a build.
type BendCheckout struct {
	Dir    string // absolute checkout directory
	Head   string // git HEAD commit
	Status string // `git status --porcelain` output; empty when clean
	Dirty  string // SHA-256 over uncommitted tracked and untracked content; "" when clean
}

// BendSourceOptions declares one Bend library build: a PROOF.bend file and a
// checked pure root, built by Bend's own build:library command for each target.
type BendSourceOptions struct {
	Checkout string   // bendSVM checkout
	Source   string   // PROOF.bend (absolute)
	Root     string   // namespace.function
	Output   string   // build directory; bundles land in <Output>/<profile>/<tools>/library.*
	Targets  []string // target profiles (fogo-v0, solana-v3)
	LLVMBin  string   // platform-tools v1.57 llvm/bin for SBF_V157_TOOLS (default from DefaultPlatformTools)
	Log      func(format string, args ...any)
}

// BendBuildResult is Bend's --json object for one successful target build.
type BendBuildResult struct {
	OK                bool   `json:"ok"`
	Error             string `json:"error"`
	Manifest          string `json:"manifest"`
	ManifestSHA256    string `json:"manifestSha256"`
	TargetProfile     string `json:"targetProfile"`
	PlatformTools     string `json:"platformTools"`
	Root              string `json:"root"`
	Symbol            string `json:"symbol"`
	SchemaFingerprint string `json:"schemaFingerprint"`
}

// InspectBendCheckout checks the bendSVM checkout prerequisites and returns its
// identifying state. It is a variable only so tests can substitute a fake
// checkout; production code never reassigns it.
var InspectBendCheckout = inspectBendCheckout

// RunBendBuild runs Bend's build:library for one target and returns its
// stdout (the --json object), its stderr and the exit error. It is a variable
// only so tests can substitute a fake build; production code never reassigns it.
var RunBendBuild = runBendBuild

func inspectBendCheckout(dir string) (BendCheckout, error) {
	var c BendCheckout
	abs, err := filepath.Abs(dir)
	if err != nil {
		return c, err
	}
	c.Dir = abs
	pkg, err := os.ReadFile(filepath.Join(abs, "package.json"))
	if err != nil {
		return c, fmt.Errorf("Bend checkout %s: %w (pass -bend-checkout or set %s to a bendSVM checkout)", abs, err, BendCheckoutEnv)
	}
	var manifest struct {
		Scripts map[string]string `json:"scripts"`
	}
	if json.Unmarshal(pkg, &manifest) != nil || manifest.Scripts["build:library"] == "" {
		return c, fmt.Errorf("Bend checkout %s has no build:library script; is it a bendSVM checkout?", abs)
	}
	if _, err = exec.LookPath("npm"); err != nil {
		return c, fmt.Errorf("Bend library builds need npm on PATH (Node 22): %w", err)
	}
	if _, err = os.Stat(filepath.Join(abs, "node_modules", ".bin", "bun")); err != nil {
		return c, fmt.Errorf("Bend checkout %s has no installed dependencies (node_modules/.bin/bun); run: npm --prefix %s ci", abs, abs)
	}
	deps := filepath.Join(abs, ".deps", "bend")
	if _, err = os.Stat(filepath.Join(deps, ".git")); err != nil {
		return c, fmt.Errorf("Bend checkout %s is missing its pinned Bend dependency .deps/bend (commit %s); GoSVM does not fetch it. Set it up in the Bend checkout following its developer workflow", abs, BendDependencyCommit)
	}
	head, err := gitOutput(deps, "rev-parse", "HEAD")
	if err != nil {
		return c, fmt.Errorf("Bend dependency %s: %w", deps, err)
	}
	if strings.TrimSpace(head) != BendDependencyCommit {
		return c, fmt.Errorf("Bend dependency %s is at %s, Bend requires %s; restore it in the Bend checkout", deps, strings.TrimSpace(head), BendDependencyCommit)
	}
	if status, err := gitOutput(deps, "status", "--porcelain"); err != nil || status != "" {
		return c, fmt.Errorf("Bend dependency %s must be clean (%v); restore it in the Bend checkout", deps, err)
	}
	cc := os.Getenv("CC")
	if cc == "" {
		cc = "clang"
	}
	if _, err = exec.LookPath(cc); err != nil {
		return c, fmt.Errorf("Bend library builds need a host C compiler (%s) for the host object: %w", cc, err)
	}
	return readBendCheckout(abs)
}

// readBendCheckout records HEAD, porcelain status and a hash of uncommitted
// content. It uses no optional locks, so it does not rewrite the index.
func readBendCheckout(abs string) (BendCheckout, error) {
	c := BendCheckout{Dir: abs}
	head, err := gitOutput(abs, "rev-parse", "HEAD")
	if err != nil {
		return c, fmt.Errorf("Bend checkout %s is not a git checkout: %w", abs, err)
	}
	c.Head = strings.TrimSpace(head)
	if c.Status, err = gitOutput(abs, "status", "--porcelain"); err != nil {
		return c, err
	}
	if c.Status == "" {
		return c, nil
	}
	h := sha256.New()
	diff, err := gitOutput(abs, "diff", "HEAD", "--binary")
	if err != nil {
		return c, err
	}
	fmt.Fprintf(h, "diff\x00%s", diff)
	untracked, err := gitOutput(abs, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return c, err
	}
	for _, name := range strings.Split(untracked, "\x00") {
		if name == "" {
			continue
		}
		digest, err := fileHash(filepath.Join(abs, name))
		if err != nil {
			digest = "unreadable"
		}
		fmt.Fprintf(h, "untracked\x00%s\x00%s\n", name, digest)
	}
	c.Dirty = fmt.Sprintf("%x", h.Sum(nil))
	return c, nil
}

func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"--no-optional-locks", "-C", dir}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return string(out), nil
}

var bendImportRE = regexp.MustCompile(`^import\s+(\S+)(?:\s+as\s+([A-Za-z_][A-Za-z0-9_]*))?`)
var bendRootRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)+$`)

// ValidBendRoot reports whether root has Bend's namespace.function form.
func ValidBendRoot(root string) bool { return bendRootRE.MatchString(root) }

// BendSourceInputs hashes a Bend source file and every file it imports
// ("import path as Alias" header lines, transitively, as Bend resolves them).
// Base and content-addressed 0x… imports are identified by name.
func BendSourceInputs(source string) (map[string]string, error) {
	inputs := map[string]string{}
	var visit func(string) error
	visit = func(file string) error {
		if _, ok := inputs[file]; ok {
			return nil
		}
		data, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("Bend source: %w", err)
		}
		inputs[file] = fmt.Sprintf("%x", sha256.Sum256(data))
		scanner := bufio.NewScanner(bytes.NewReader(data))
		scanner.Buffer(make([]byte, 1<<20), 1<<20)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			m := bendImportRE.FindStringSubmatch(line)
			if m == nil {
				if line != "" && !strings.HasPrefix(line, "#") {
					break // Imports form the file header, as in Bend's own closure.
				}
				continue
			}
			imported := m[1]
			if m[2] == "" || strings.HasPrefix(imported, "0x") {
				// Base comes from the pinned .deps/bend; 0x… imports are content-addressed.
				inputs["named:"+imported] = imported
				continue
			}
			if !filepath.IsAbs(imported) {
				imported = filepath.Join(filepath.Dir(file), imported)
			}
			if err := visit(filepath.Clean(imported)); err != nil {
				return err
			}
		}
		return scanner.Err()
	}
	abs, err := filepath.Abs(source)
	if err != nil {
		return nil, err
	}
	return inputs, visit(abs)
}

// BendBuildKey identifies a Bend source build: the source and its imports,
// root, targets, Bend checkout HEAD and uncommitted content, and the pinned
// Bend dependency and platform-tools. File names are relative to the source
// directory so moving a project does not force a rebuild.
func BendBuildKey(checkout BendCheckout, source, root string, targets []string) (string, error) {
	inputs, err := BendSourceInputs(source)
	if err != nil {
		return "", err
	}
	base := filepath.Dir(source)
	if abs, err := filepath.Abs(source); err == nil {
		base = filepath.Dir(abs)
	}
	names := make([]string, 0, len(inputs))
	for name := range inputs {
		names = append(names, name)
	}
	sort.Strings(names)
	sorted := append([]string(nil), targets...)
	sort.Strings(sorted)
	h := sha256.New()
	fmt.Fprintf(h, "gosvm-bend-source-build-v1\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\n",
		target.ModernTools, BendDependencyCommit, checkout.Head, checkout.Dirty, root, strings.Join(sorted, ","))
	for _, name := range names {
		label := name
		if rel, err := filepath.Rel(base, name); err == nil && !strings.HasPrefix(name, "named:") {
			label = filepath.ToSlash(rel)
		}
		fmt.Fprintf(h, "%s\x00%s\n", label, inputs[name])
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// BendLLVMBin returns GoSVM's pinned platform-tools v1.57 llvm/bin for Bend's
// SBF_V157_TOOLS, checking the content-pinned Clang/LLD and the presence of
// llvm-readelf (Bend checks it by content).
func BendLLVMBin() (string, error) {
	llvm := filepath.Join(DefaultPlatformTools(), "llvm")
	p, _ := target.Resolve(target.Solana)
	if err := p.CheckBackend(llvm); err != nil {
		return "", fmt.Errorf("Bend library builds need the full platform-tools %s tree (clang, llvm-readelf): %w; run: %s", target.ModernTools, err, RustInstallCommand)
	}
	if _, err := os.Stat(filepath.Join(llvm, "bin", "llvm-readelf")); err != nil {
		return "", fmt.Errorf("Bend library builds need llvm-readelf from platform-tools %s: %w; run: %s", target.ModernTools, err, RustInstallCommand)
	}
	return filepath.Join(llvm, "bin"), nil
}

func runBendBuild(checkout string, args, env []string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(context.Background(), "npm", append([]string{"--prefix", checkout, "run", "-s", "build:library", "--"}, args...)...)
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

// ParseBendBuild checks Bend's --json result for one target build against the
// requested profile, root and stem, and verifies the manifest SHA-256 it reports
// against the file on disk.
func ParseBendBuild(stdout []byte, profile, root, stem string) (BendBuildResult, error) {
	var r BendBuildResult
	lines := strings.Split(strings.TrimSpace(string(stdout)), "\n")
	if len(lines) != 1 || lines[0] == "" {
		return r, fmt.Errorf("Bend build printed %d stdout lines, expected one JSON object", len(lines))
	}
	dec := json.NewDecoder(strings.NewReader(lines[0]))
	if err := dec.Decode(&r); err != nil {
		return r, fmt.Errorf("Bend build result is not JSON: %w", err)
	}
	if !r.OK {
		if r.Error == "" {
			r.Error = "no error message"
		}
		return r, fmt.Errorf("Bend build failed: %s", r.Error)
	}
	if r.TargetProfile != profile {
		return r, fmt.Errorf("Bend build produced target %q, requested %q", r.TargetProfile, profile)
	}
	if r.Root != root {
		return r, fmt.Errorf("Bend build produced root %q, requested %q", r.Root, root)
	}
	if !Accepts("bend-library-v1", profile, r.PlatformTools) {
		return r, fmt.Errorf("Bend build used platform-tools %q, not accepted for %s", r.PlatformTools, profile)
	}
	want := filepath.Join(filepath.Dir(stem), profile, r.PlatformTools, filepath.Base(stem)+".manifest.json")
	if filepath.Clean(r.Manifest) != filepath.Clean(want) {
		return r, fmt.Errorf("Bend build manifest %s is not at the expected %s", r.Manifest, want)
	}
	data, err := os.ReadFile(r.Manifest)
	if err != nil {
		return r, fmt.Errorf("Bend build manifest: %w", err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(data)); got != r.ManifestSHA256 {
		return r, fmt.Errorf("Bend build manifest SHA-256 %s does not match the reported %s", got, r.ManifestSHA256)
	}
	return r, nil
}

// BuildBendSource runs Bend's build:library once per target into o.Output
// (which must not exist) and returns each target's verified producer pin. The
// checkout's git status must be unchanged afterwards: GoSVM never writes there.
func BuildBendSource(checkout BendCheckout, o BendSourceOptions) (map[string]ProducerPin, error) {
	if o.Log == nil {
		o.Log = func(string, ...any) {}
	}
	source, err := filepath.Abs(o.Source)
	if err != nil {
		return nil, err
	}
	output, err := filepath.Abs(o.Output)
	if err != nil {
		return nil, err
	}
	if !ValidBendRoot(o.Root) {
		return nil, fmt.Errorf("Bend root %q must have the form namespace.function", o.Root)
	}
	if _, err = os.Lstat(output); err == nil {
		return nil, fmt.Errorf("Bend build output %s already exists", output)
	}
	if o.LLVMBin == "" {
		if o.LLVMBin, err = BendLLVMBin(); err != nil {
			return nil, err
		}
	}
	if err = os.MkdirAll(output, 0755); err != nil {
		return nil, err
	}
	env := []string{}
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "SBF_V157_TOOLS=") && !strings.HasPrefix(kv, "SBF_V3_TOOLS=") {
			env = append(env, kv)
		}
	}
	env = append(env, "SBF_V157_TOOLS="+o.LLVMBin)
	stem := filepath.Join(output, "library")
	pins := map[string]ProducerPin{}
	fail := func(err error) (map[string]ProducerPin, error) {
		os.RemoveAll(output)
		return nil, err
	}
	for _, profile := range o.Targets {
		o.Log("Building Bend library %s (%s) for %s\n", o.Root, filepath.Base(source), profile)
		stdout, stderr, runErr := RunBendBuild(checkout.Dir, []string{source, o.Root, stem, "--target", profile, "--json"}, env)
		r, err := ParseBendBuild(stdout, profile, o.Root, stem)
		if err == nil && runErr != nil {
			err = fmt.Errorf("Bend build exited with %v despite reporting success", runErr)
		}
		if err != nil {
			return fail(fmt.Errorf("%s: %w%s", profile, err, tail(stderr, 20)))
		}
		pins[profile] = ProducerPin{Manifest: r.Manifest, SHA256: r.ManifestSHA256}
	}
	after, err := readBendCheckout(checkout.Dir)
	if err != nil {
		return fail(err)
	}
	if after.Head != checkout.Head || after.Status != checkout.Status || after.Dirty != checkout.Dirty {
		return fail(fmt.Errorf("Bend checkout %s changed during the build (git status before:\n%safter:\n%s); GoSVM discarded the build", checkout.Dir, checkout.Status, after.Status))
	}
	return pins, nil
}

func tail(stderr []byte, n int) string {
	lines := strings.Split(strings.TrimRight(string(stderr), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return ""
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return "\nBend build log (last lines):\n" + strings.Join(lines, "\n")
}
