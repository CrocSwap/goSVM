package library

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func writeBendTree(t *testing.T, dir, mixed string) string {
	t.Helper()
	os.MkdirAll(dir, 0755)
	files := map[string]string{
		"PROOF.bend": "import ./LAWS.bend as Laws\nimport ./mixed.bend as Mixed\n\ndef Laws.x():\n  {==}\nimport ./ignored.bend as Late\n",
		"LAWS.bend":  "# laws\nimport ./mixed.bend as Mixed\n",
		"mixed.bend": mixed,
	}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(dir, "PROOF.bend")
}

// The key covers the source and its transitive header imports (not text
// after the header), root, targets (order-free) and the checkout state, and
// not the project's location.
func TestBendBuildKey(t *testing.T) {
	tmp := t.TempDir()
	source := writeBendTree(t, filepath.Join(tmp, "a"), "import Base\n\ndef Mixed.evaluate(x):\n  x\n")
	inputs, err := BendSourceInputs(source)
	if err != nil || len(inputs) != 4 || inputs["named:Base"] != "Base" {
		t.Fatal(inputs, err)
	}
	co := BendCheckout{Head: strings.Repeat("1", 40)}
	targets := []string{"fogo-v0", "solana-v3"}
	key := func(co BendCheckout, source, root string, targets []string) string {
		k, err := BendBuildKey(co, source, root, targets)
		if err != nil {
			t.Fatal(err)
		}
		return k
	}
	base := key(co, source, "mixed.evaluate", targets)
	moved := writeBendTree(t, filepath.Join(tmp, "b"), "import Base\n\ndef Mixed.evaluate(x):\n  x\n")
	if key(co, moved, "mixed.evaluate", []string{"solana-v3", "fogo-v0"}) != base {
		t.Fatal("key depends on location or target order")
	}
	changed := writeBendTree(t, filepath.Join(tmp, "c"), "import Base\n\ndef Mixed.evaluate(x):\n  0\n")
	dirty := co
	dirty.Dirty = "d"
	moved2 := co
	moved2.Head = strings.Repeat("2", 40)
	for name, k := range map[string]string{
		"import":  key(co, changed, "mixed.evaluate", targets),
		"root":    key(co, source, "mixed.other", targets),
		"targets": key(co, source, "mixed.evaluate", targets[:1]),
		"dirty":   key(dirty, source, "mixed.evaluate", targets),
		"head":    key(moved2, source, "mixed.evaluate", targets),
	} {
		if k == base {
			t.Fatalf("%s change kept the key", name)
		}
	}
	if _, err = BendSourceInputs(filepath.Join(tmp, "missing.bend")); err == nil {
		t.Fatal("accepted a missing source")
	}
	os.Remove(filepath.Join(tmp, "a", "mixed.bend"))
	if _, err = BendBuildKey(co, source, "mixed.evaluate", targets); err == nil {
		t.Fatal("accepted a missing import")
	}
}

func bendResult(manifest, sha, profile, tools, root string) []byte {
	return []byte(fmt.Sprintf(`{"ok":true,"manifest":%q,"manifestSha256":%q,"targetProfile":%q,"platformTools":%q,"root":%q,"symbol":"bend_library_v1_call_x","schemaFingerprint":"x"}`+"\n",
		manifest, sha, profile, tools, root))
}

func TestParseBendBuild(t *testing.T) {
	stem := filepath.Join(t.TempDir(), "library")
	manifest := filepath.Join(filepath.Dir(stem), "solana-v3", "v1.57", "library.manifest.json")
	os.MkdirAll(filepath.Dir(manifest), 0755)
	os.WriteFile(manifest, []byte("{}"), 0644)
	sha := fmt.Sprintf("%x", sha256.Sum256([]byte("{}")))
	good := bendResult(manifest, sha, "solana-v3", "v1.57", "mixed.evaluate")
	if r, err := ParseBendBuild(good, "solana-v3", "mixed.evaluate", stem); err != nil || r.ManifestSHA256 != sha {
		t.Fatal(r, err)
	}
	other := filepath.Join(filepath.Dir(stem), "elsewhere.manifest.json")
	os.WriteFile(other, []byte("{}"), 0644)
	for name, out := range map[string][]byte{
		"failed":    []byte(`{"ok":false,"error":"Library root was not emitted: mixed.nosuch"}`),
		"empty":     nil,
		"two lines": append(append([]byte{}, good...), good...),
		"not json":  []byte("Built library\n"),
		"sha":       bendResult(manifest, strings.Repeat("0", 64), "solana-v3", "v1.57", "mixed.evaluate"),
		"profile":   bendResult(manifest, sha, "fogo-v0", "v1.57", "mixed.evaluate"),
		"root":      bendResult(manifest, sha, "solana-v3", "v1.57", "mixed.other"),
		"tools":     bendResult(manifest, sha, "solana-v3", "v1.53", "mixed.evaluate"),
		"path":      bendResult(other, sha, "solana-v3", "v1.57", "mixed.evaluate"),
		"missing":   bendResult(manifest+".gone", sha, "solana-v3", "v1.57", "mixed.evaluate"),
	} {
		if _, err := ParseBendBuild(out, "solana-v3", "mixed.evaluate", stem); err == nil {
			t.Fatalf("accepted %s result", name)
		}
	}
	if _, err := ParseBendBuild([]byte(`{"ok":false,"error":"Library root was not emitted: mixed.nosuch"}`), "solana-v3", "mixed.nosuch", stem); err == nil || !strings.Contains(err.Error(), "mixed.nosuch") {
		t.Fatal("Bend's error was not reported:", err)
	}
}

func gitCheckout(t *testing.T) BendCheckout {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "x"}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Skip("git unavailable:", err, string(out))
		}
	}
	c, err := readBendCheckout(dir)
	if err != nil || c.Head == "" || c.Dirty != "" {
		t.Fatal(c, err)
	}
	return c
}

// BuildBendSource invokes one build per target with the documented arguments
// and SBF_V157_TOOLS, verifies each result and refuses a build that touched
// the checkout, discarding its output.
func TestBuildBendSourceFake(t *testing.T) {
	checkout := gitCheckout(t)
	source := writeBendTree(t, filepath.Join(t.TempDir(), "src"), "def Mixed.evaluate(x):\n  x\n")
	var calls [][]string
	touch := false
	RunBendBuild = func(dir string, args, env []string) ([]byte, []byte, error) {
		calls = append(calls, args)
		if dir != checkout.Dir || len(args) != 6 || args[0] != source || args[1] != "mixed.evaluate" || args[3] != "--target" || args[5] != "--json" {
			t.Fatalf("unexpected invocation %s %v", dir, args)
		}
		if env[len(env)-1] != "SBF_V157_TOOLS=/tools/llvm/bin" {
			t.Fatalf("tools not passed: %v", env[len(env)-1])
		}
		manifest := filepath.Join(filepath.Dir(args[2]), args[4], "v1.57", "library.manifest.json")
		os.MkdirAll(filepath.Dir(manifest), 0755)
		data := []byte(args[4])
		os.WriteFile(manifest, data, 0644)
		if touch {
			os.WriteFile(filepath.Join(checkout.Dir, "stray"), nil, 0644)
		}
		return bendResult(manifest, fmt.Sprintf("%x", sha256.Sum256(data)), args[4], "v1.57", args[1]), []byte("log\n"), nil
	}
	defer func() { RunBendBuild = runBendBuild }()
	out := filepath.Join(t.TempDir(), "build", "key")
	o := BendSourceOptions{Source: source, Root: "mixed.evaluate", Output: out, Targets: []string{"fogo-v0", "solana-v3"}, LLVMBin: "/tools/llvm/bin"}
	pins, err := BuildBendSource(checkout, o)
	if err != nil || len(pins) != 2 || len(calls) != 2 || pins["fogo-v0"].Manifest != filepath.Join(out, "fogo-v0/v1.57/library.manifest.json") {
		t.Fatal(pins, calls, err)
	}
	if _, err = BuildBendSource(checkout, o); err == nil {
		t.Fatal("reused an existing output directory")
	}
	touch = true
	o.Output = filepath.Join(t.TempDir(), "touched")
	if _, err = BuildBendSource(checkout, o); err == nil || !strings.Contains(err.Error(), "changed during the build") {
		t.Fatal("accepted a build that wrote into the checkout:", err)
	}
	if _, err = os.Stat(o.Output); !os.IsNotExist(err) {
		t.Fatal("kept a discarded build")
	}
	touch = false
	os.Remove(filepath.Join(checkout.Dir, "stray"))
	RunBendBuild = func(string, []string, []string) ([]byte, []byte, error) {
		return []byte(`{"ok":false,"error":"proof gate failed"}` + "\n"), []byte("details\n"), fmt.Errorf("exit status 1")
	}
	o.Output = filepath.Join(t.TempDir(), "failed")
	if _, err = BuildBendSource(checkout, o); err == nil || !strings.Contains(err.Error(), "proof gate failed") || !strings.Contains(err.Error(), "details") {
		t.Fatal("failure not reported with Bend's error and log:", err)
	}
	o.Root = "evaluate"
	if _, err = BuildBendSource(checkout, o); err == nil {
		t.Fatal("accepted a root without a namespace")
	}
}

// Prerequisites fail with actionable messages before any build runs.
func TestInspectBendCheckoutPrerequisites(t *testing.T) {
	dir := t.TempDir()
	if _, err := inspectBendCheckout(filepath.Join(dir, "none")); err == nil || !strings.Contains(err.Error(), BendCheckoutEnv) {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"scripts":{}}`), 0644)
	if _, err := inspectBendCheckout(dir); err == nil || !strings.Contains(err.Error(), "build:library") {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"scripts":{"build:library":"bun scripts/build-library.ts"}}`), 0644)
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("npm unavailable")
	}
	if _, err := inspectBendCheckout(dir); err == nil || !strings.Contains(err.Error(), "ci") {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(dir, "node_modules", ".bin"), 0755)
	os.WriteFile(filepath.Join(dir, "node_modules", ".bin", "bun"), nil, 0755)
	if _, err := inspectBendCheckout(dir); err == nil || !strings.Contains(err.Error(), ".deps/bend") || !strings.Contains(err.Error(), "does not fetch") {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".deps")); !os.IsNotExist(err) {
		t.Fatal("prerequisite check created .deps")
	}
	deps := filepath.Join(dir, ".deps", "bend")
	os.MkdirAll(deps, 0755)
	for _, args := range [][]string{{"init", "-q"}, {"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "x"}} {
		if out, err := exec.Command("git", append([]string{"-C", deps}, args...)...).CombinedOutput(); err != nil {
			t.Skip("git unavailable:", err, string(out))
		}
	}
	if _, err := inspectBendCheckout(dir); err == nil || !strings.Contains(err.Error(), BendDependencyCommit) {
		t.Fatal("accepted a wrong Bend dependency commit:", err)
	}
}
