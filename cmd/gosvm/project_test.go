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
	if e := os.WriteFile(filepath.Join(dir, "bad.go"), []byte("package program\nvar UnsupportedGlobal uint64\n"), 0644); e != nil {
		t.Fatal(e)
	}
	if e := projectCommand([]string{"test", "--svm", "-dir", dir}); e == nil {
		t.Fatal("unsupported source accepted")
	}
	if _, e := os.Stat(report); !os.IsNotExist(e) {
		t.Fatal("generation failure retained a stale passing report")
	}
}
