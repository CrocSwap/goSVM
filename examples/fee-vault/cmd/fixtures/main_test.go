package main

import (
	"bytes"
	"os"
	"testing"
)

// The committed fixtures must match what the generator writes.
func TestFixturesUpToDate(t *testing.T) {
	want, err := suiteJSON()
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("../../testdata/svm.json")
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("testdata/svm.json is stale (%v); run: go run ./cmd/fixtures > testdata/svm.json", err)
	}
}
