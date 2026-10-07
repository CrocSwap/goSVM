package sbftest

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestFastSelectionRejectsEmptyAndInvalidMatches(t *testing.T) {
	s := Suite{Schema: 1, Cases: []Case{{Name: "swap"}, {Name: "atomic rollback"}}}
	for _, pattern := range []string{"[", "^missing$"} {
		if _, e := selectCases(s, pattern); e == nil {
			t.Fatalf("accepted %q", pattern)
		}
	}
	cases, e := selectCases(s, "rollback$")
	if e != nil || len(cases) != 1 || cases[0].Name != "atomic rollback" {
		t.Fatalf("bad selection: %v %v", cases, e)
	}
}
func TestFastFailureRemovesPreviousPassingReport(t *testing.T) {
	dir := t.TempDir()
	if e := os.Mkdir(filepath.Join(dir, "build"), 0755); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(dir, "build/svm-results.json")
	if e := os.WriteFile(path, []byte(`{"cases":[]}`), 0644); e != nil {
		t.Fatal(e)
	}
	if e := RunFast(context.Background(), dir, "missing.so", io.Discard, FastOptions{}); e == nil {
		t.Fatal("accepted missing fixture file")
	}
	if _, e := os.Stat(path); !os.IsNotExist(e) {
		t.Fatal("stale passing report retained")
	}
}
