package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gosvm/internal/project"
)

func TestFastTestFlagsRejectConflictingModesBeforeProjectWork(t *testing.T) {
	for _, args := range [][]string{{"test", "--svm", "--sbf"}, {"test", "--svm-run", "swap"}, {"test", "--svm-runner", "/absent"}, {"test", "--svm-sysvars", "/absent"}, {"test", "--svm-fixtures", "/absent"}} {
		e := projectCommand(args)
		if e == nil || (!strings.Contains(e.Error(), "choose either") && !strings.Contains(e.Error(), "require -svm")) {
			t.Fatalf("flags accepted: %v %v", args, e)
		}
	}
}

func TestFastGenerationFailureRemovesPreviousReport(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "swap")
	if e := project.New(dir); e != nil {
		t.Fatal(e)
	}
	if e := os.Mkdir(filepath.Join(dir, "build"), 0755); e != nil {
		t.Fatal(e)
	}
	report := filepath.Join(dir, "build/svm-results.json")
	if e := os.WriteFile(report, []byte(`{"cases":[]}`), 0644); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(dir, "bad.go"), []byte("package program\nvar UnsupportedFloat float64\n"), 0644); e != nil {
		t.Fatal(e)
	}
	if e := projectCommand([]string{"test", "--svm", "-dir", dir}); e == nil {
		t.Fatal("unsupported source accepted")
	}
	if _, e := os.Stat(report); !os.IsNotExist(e) {
		t.Fatal("generation failure retained a stale passing report")
	}
}

func TestSVMFixturesDefault(t *testing.T) {
	starter := filepath.Join(t.TempDir(), "swap")
	if e := project.New(starter); e != nil {
		t.Fatal(e)
	}
	if got, e := defaultSVMFixtures(starter); got != "" || e != nil {
		t.Fatalf("schema-1 starter: %q, %v", got, e)
	}
	// A fresh SDK-2 project has generated fixtures.
	vaults := filepath.Join(t.TempDir(), "vaults")
	if e := project.NewModuleSDK(vaults, "", 2, 2); e != nil {
		t.Fatal(e)
	}
	if got, e := defaultSVMFixtures(vaults); got != filepath.Join(vaults, "testdata", "svm.json") || e != nil {
		t.Fatalf("SDK-2 scaffold: %q, %v", got, e)
	}
	general := filepath.Join(t.TempDir(), "ledger")
	if e := project.NewModuleSDK(general, "", 2, 1); e != nil {
		t.Fatal(e)
	}
	// A fresh SDK-1 schema-2 project has no fixtures: fail before building, naming the fix.
	e := projectCommand([]string{"test", "--svm", "-dir", general})
	if e == nil || !strings.Contains(e.Error(), "add testdata/svm.json") || !strings.Contains(e.Error(), "-svm-fixtures") {
		t.Fatalf("missing fixtures: %v", e)
	}
	if _, e := os.Stat(filepath.Join(general, "build")); !os.IsNotExist(e) {
		t.Fatal("built before reporting missing fixtures")
	}
	fixtures := filepath.Join(general, "testdata", "svm.json")
	if e := os.MkdirAll(filepath.Dir(fixtures), 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(fixtures, []byte(`{}`), 0644); e != nil {
		t.Fatal(e)
	}
	if got, e := defaultSVMFixtures(general); got != fixtures || e != nil {
		t.Fatalf("schema-2 project: %q, %v", got, e)
	}
}
