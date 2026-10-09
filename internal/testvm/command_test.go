package testvm

import (
	"gosvm/internal/runner"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSelectionAndVersion(t *testing.T) {
	t.Setenv("GOSVM_TEST_RUNNER", "")
	if Command("--version").Args[0] != "solana-test-validator" || Engine() != "validator" {
		t.Fatal("validator must remain the default")
	}
	if CheckVersion("solana-test-validator 3.0.15 (src:test)") != nil || CheckVersion("solana-test-validator 3.0.14") == nil {
		t.Fatal("validator pin not enforced")
	}
	t.Setenv("GOSVM_TEST_RUNNER", "/tmp/explicit-runner")
	if Command("--version").Args[0] != "/tmp/explicit-runner" || Engine() != "litesvm" {
		t.Fatal("explicit runner not selected")
	}
	if CheckVersion(RunnerVersion) != nil || CheckVersion("solana-test-validator 3.0.15") == nil {
		t.Fatal("runner identity not enforced")
	}
}

func TestRunnerSelectionPrecedenceAndDamagedManagedCache(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("GOSVM_CACHE", cache)
	t.Setenv("GOSVM_TEST_RUNNER", "")
	bin := t.TempDir()
	pathRunner := filepath.Join(bin, "gosvm-svm-runner")
	if err := os.WriteFile(pathRunner, []byte("runner"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	if p, err := RunnerPath(""); err != nil || p != pathRunner {
		t.Fatalf("PATH fallback: %s %v", p, err)
	}
	if err := os.MkdirAll(runner.Root(), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := RunnerPath(""); err == nil {
		t.Fatal("damaged managed cache fell back to PATH")
	}
	explicit := filepath.Join(bin, "explicit")
	if err := os.WriteFile(explicit, []byte("runner"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOSVM_TEST_RUNNER", pathRunner)
	if p, err := RunnerPath(""); err != nil || p != pathRunner {
		t.Fatalf("environment override: %s %v", p, err)
	}
	if p, err := RunnerPath(explicit); err != nil || p != explicit {
		t.Fatalf("explicit override: %s %v", p, err)
	}
	if _, err := RunnerPath(filepath.Join(bin, "missing")); err == nil {
		t.Fatal("missing explicit runner fell back")
	}
}

func TestRunnerSelectionPerProfile(t *testing.T) {
	t.Setenv("GOSVM_CACHE", t.TempDir())
	t.Setenv("GOSVM_TEST_RUNNER", "")
	t.Setenv("GOSVM_TEST_FEATURES", "")
	t.Setenv("PATH", t.TempDir())
	qualifiedHost := runtime.GOOS == "darwin" && runtime.GOARCH == "arm64"
	for profile, want := range map[string]string{"solana-v3": "gosvm-svm-runner-0.6.0-darwin-arm64.tar.gz", "fogo-v0": "gosvm-svm-runner-0.5.0-darwin-arm64.tar.gz", "": "gosvm-svm-runner-0.5.0-darwin-arm64.tar.gz"} {
		_, err := RunnerFor("", profile)
		if err == nil || (qualifiedHost && !strings.Contains(err.Error(), "gosvm runner install -archive "+want)) {
			t.Fatalf("%s: missing runner hint: %v", profile, err)
		}
	}
	if !qualifiedHost {
		return
	}
	// A damaged modern slot affects only profiles that may select it.
	if err := os.MkdirAll(runner.RootFor(runner.ModernVersion), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := RunnerFor("", "solana-v3"); err == nil || !strings.Contains(err.Error(), "integrity") {
		t.Fatalf("damaged modern runner: %v", err)
	}
	if _, err := RunnerFor("", "fogo-v0"); err == nil || strings.Contains(err.Error(), "integrity") {
		t.Fatalf("fogo-v0 selected the modern engine without explicit features: %v", err)
	}
	t.Setenv("GOSVM_TEST_FEATURES", filepath.Join(t.TempDir(), "features.json"))
	if _, err := RunnerFor("", "fogo-v0"); err == nil || !strings.Contains(err.Error(), "integrity") {
		t.Fatalf("fogo-v0 with explicit features ignored the modern slot: %v", err)
	}
	if v := ManagedRunnerVersions("legacy-v1.51-v3"); len(v) != 1 || v[0] != runner.Version {
		t.Fatal(v)
	}
}
