package project

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gosvm/internal/library"
)

// Bend's published mixed.evaluate bundles, relative to a bendSVM checkout.
var publishedBend = map[string]string{
	"fogo-v0":   "build/interop/fogo-v0/20261009T191953Z-0295b5f8/fogo-v0/v1.57/library.manifest.json",
	"solana-v3": "build/interop/solana-v3/20261009T185443Z-f45b18ca/solana-v3/v1.57/library.manifest.json",
}

func TestBendSourceDeclarationsValidate(t *testing.T) {
	src := &BendSource{File: "bend/PROOF.bend", Root: "mixed.evaluate"}
	if err := validateLibraries(Config{Libraries: map[string]LibraryDeclaration{"bend": {Function: "Evaluate", BendSource: src}}}); err != nil {
		t.Fatal(err)
	}
	pin := LibraryPin{Manifest: "m.json", SHA256: strings.Repeat("a", 64)}
	for name, l := range map[string]LibraryDeclaration{
		"pins":   {Function: "Evaluate", BendSource: src, Bend: map[string]LibraryPin{"fogo": pin}},
		"rust":   {Function: "Evaluate", BendSource: src, Rust: &RustCrate{Crate: "q"}},
		"nofile": {Function: "Evaluate", BendSource: &BendSource{Root: "mixed.evaluate"}},
		"suffix": {Function: "Evaluate", BendSource: &BendSource{File: "PROOF.txt", Root: "mixed.evaluate"}},
		"noroot": {Function: "Evaluate", BendSource: &BendSource{File: "PROOF.bend"}},
		"root":   {Function: "Evaluate", BendSource: &BendSource{File: "PROOF.bend", Root: "evaluate"}},
	} {
		if err := validateLibraries(Config{Libraries: map[string]LibraryDeclaration{"bend": l}}); err == nil {
			t.Fatalf("accepted %s: %+v", name, l)
		}
	}
}

func newTargetProject(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "app")
	if err := NewModuleTarget(dir, "example.com/app", Schema, 1, "fogo,solana"); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestBendSourceNeedsCheckout(t *testing.T) {
	t.Setenv(library.BendCheckoutEnv, "")
	dir := newTargetProject(t)
	os.WriteFile(filepath.Join(dir, "PROOF.bend"), []byte("def Mixed.evaluate(x):\n  x\n"), 0644)
	err := AddBendSourceLibrary(dir, "bend", "Evaluate", filepath.Join(dir, "PROOF.bend"), "mixed.evaluate", "")
	if err == nil || !strings.Contains(err.Error(), library.BendCheckoutEnv) {
		t.Fatal(err)
	}
	if c, _ := Load(dir); len(c.Libraries) != 0 {
		t.Fatal("recorded a library that failed to build")
	}
}

// bendCheckoutForTest returns $GOSVM_BEND when it holds Bend's published
// bundles and mixed-library sources, else skips.
func bendCheckoutForTest(t *testing.T) string {
	t.Helper()
	checkout := os.Getenv(library.BendCheckoutEnv)
	if checkout == "" {
		t.Skip("set GOSVM_BEND to a bendSVM checkout for Bend source integration")
	}
	for _, rel := range append([]string{"tests/mixed-library/PROOF.bend"}, publishedBend["fogo-v0"], publishedBend["solana-v3"]) {
		if _, err := os.Stat(filepath.Join(checkout, rel)); err != nil {
			t.Skip("Bend checkout lacks", rel)
		}
	}
	return checkout
}

func copyBendSources(t *testing.T, checkout, dir string) string {
	t.Helper()
	os.MkdirAll(filepath.Join(dir, "bend"), 0755)
	files, _ := filepath.Glob(filepath.Join(checkout, "tests/mixed-library/*.bend"))
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(dir, "bend", filepath.Base(f)), data, 0644)
	}
	return filepath.Join(dir, "bend", "PROOF.bend")
}

func copyDir(t *testing.T, from, to string) {
	t.Helper()
	os.MkdirAll(to, 0755)
	entries, err := os.ReadDir(from)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(from, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(to, e.Name()), data, 0644)
	}
}

// With a fake Bend build that publishes copies of Bend's bundles, a declared
// Bend source is recorded relative to the project, built once per target,
// bound through the pinned-manifest checks, reused while unchanged and
// rebuilt (dropping the superseded build) when an import changes.
func TestBendSourceSyncFake(t *testing.T) {
	checkout := bendCheckoutForTest(t)
	fakeCheckout := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "x"}} {
		if out, err := exec.Command("git", append([]string{"-C", fakeCheckout}, args...)...).CombinedOutput(); err != nil {
			t.Skip("git unavailable:", err, string(out))
		}
	}
	calls := 0
	inspect, run := library.InspectBendCheckout, library.RunBendBuild
	t.Cleanup(func() { library.InspectBendCheckout, library.RunBendBuild = inspect, run })
	library.InspectBendCheckout = func(dir string) (library.BendCheckout, error) {
		head, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
		return library.BendCheckout{Dir: dir, Head: strings.TrimSpace(string(head))}, err
	}
	library.RunBendBuild = func(dir string, args, env []string) ([]byte, []byte, error) {
		calls++
		profile := args[4]
		out := filepath.Join(filepath.Dir(args[2]), profile, "v1.57")
		copyDir(t, filepath.Dir(filepath.Join(checkout, publishedBend[profile])), out)
		_, _, digest, err := library.InspectBend(filepath.Join(out, "library.manifest.json"))
		if err != nil {
			t.Fatal(err)
		}
		result, _ := json.Marshal(library.BendBuildResult{OK: true, Manifest: filepath.Join(out, "library.manifest.json"), ManifestSHA256: digest,
			TargetProfile: profile, PlatformTools: "v1.57", Root: args[1]})
		return append(result, '\n'), nil, nil
	}
	dir := newTargetProject(t)
	source := copyBendSources(t, checkout, dir)
	if err := AddBendSourceLibrary(dir, "bend", "Evaluate", source, "mixed.evaluate", fakeCheckout); err != nil {
		t.Fatal(err)
	}
	c, err := Load(dir)
	if err != nil || c.Libraries["bend"].BendSource == nil || c.Libraries["bend"].BendSource.File != "bend/PROOF.bend" || calls != 2 {
		t.Fatal(c.Libraries, calls, err)
	}
	b, err := library.Read(filepath.Join(LibraryDir(dir, "bend"), library.Filename))
	if err != nil || len(b.Targets) != 2 {
		t.Fatal(b, err)
	}
	builds, _ := filepath.Glob(filepath.Join(dir, "build/bend-libraries/bend/*"))
	if err = Generate(dir); err != nil || calls != 2 {
		t.Fatal("unchanged Bend source was rebuilt", calls, err)
	}
	f, _ := os.OpenFile(filepath.Join(dir, "bend", "mixed.bend"), os.O_APPEND|os.O_WRONLY, 0644)
	f.WriteString("\n# changed\n")
	f.Close()
	if err = Generate(dir); err != nil || calls != 4 {
		t.Fatal("changed import did not rebuild", calls, err)
	}
	again, _ := filepath.Glob(filepath.Join(dir, "build/bend-libraries/bend/*"))
	if len(builds) != 1 || len(again) != 1 || builds[0] == again[0] {
		t.Fatal("superseded build kept or key unchanged", builds, again)
	}
}

// Opt-in (GOSVM_BEND): the real Bend build:library builds the mixed fixture for
// both targets; objects, sources and headers match Bend's published bundles;
// the checkout is unchanged (BuildBendSource refuses otherwise).
func TestBendSourceIntegration(t *testing.T) {
	checkout := bendCheckoutForTest(t)
	if _, err := library.InspectBendCheckout(checkout); err != nil {
		t.Skip("Bend checkout prerequisites:", err)
	}
	if _, err := library.BendLLVMBin(); err != nil {
		t.Skip(err)
	}
	dir := newTargetProject(t)
	source := copyBendSources(t, checkout, dir)
	if err := AddBendSourceLibrary(dir, "bend", "Evaluate", source, "mixed.evaluate", checkout); err != nil {
		t.Fatal(err)
	}
	builds, _ := filepath.Glob(filepath.Join(dir, "build/bend-libraries/bend/*"))
	if len(builds) != 1 {
		t.Fatal(builds)
	}
	for profile, rel := range publishedBend {
		want, got := producerArtifacts(t, filepath.Join(checkout, rel)), producerArtifacts(t, filepath.Join(builds[0], profile, "v1.57", "library.manifest.json"))
		if len(want) == 0 || len(got) != len(want) {
			t.Fatal(profile, want, got)
		}
		for name, sha := range want {
			if strings.HasSuffix(name, ".su") {
				continue // Stack-usage reports may embed the output directory.
			}
			if got[name] != sha {
				t.Fatalf("%s %s differs from Bend's published bundle", profile, name)
			}
		}
	}
	if err := Generate(dir); err != nil {
		t.Fatal(err)
	}
	if again, _ := filepath.Glob(filepath.Join(dir, "build/bend-libraries/bend/*")); len(again) != 1 || again[0] != builds[0] {
		t.Fatal("unchanged Bend source was rebuilt", again)
	}
}

func producerArtifacts(t *testing.T, manifest string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Artifacts map[string]struct {
			SHA256 string `json:"sha256"`
		} `json:"artifacts"`
	}
	if err = json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for name, a := range m.Artifacts {
		out[name] = a.SHA256
	}
	return out
}
