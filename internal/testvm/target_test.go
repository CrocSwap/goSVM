package testvm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestTargetRuntimeContract(t *testing.T) {
	for _, tc := range []struct {
		version, profile string
		want             bool
	}{
		{RunnerVersion, "legacy-v1.51-v3", true}, {ModernRunnerVersion, "legacy-v1.51-v3", false},
		{RunnerVersion, "solana-v3", false}, {ModernRunnerVersion, "solana-v3", true},
		{RunnerVersion, "fogo-v0", true}, {"unqualified", "fogo-v0", false},
		{HistoricalRunnerVersion, "fogo-v0", true}, {HistoricalRunnerVersion, "legacy-v1.51-v3", true},
		{HistoricalRunnerVersion, "solana-v3", false}, {ModernRunnerVersion, "fogo-v0", true},
	} {
		if got := RunnerCompatible(tc.version, tc.profile); got != tc.want {
			t.Fatalf("%s / %s: %v", tc.version, tc.profile, got)
		}
	}
	t.Setenv("GOSVM_TEST_FEATURES", "")
	modern, err := FeaturesFor("solana-v3")
	if err != nil || string(modern) != "null" {
		t.Fatal(string(modern), err)
	}
	legacy, err := FeaturesFor("fogo-v0")
	if err != nil || !json.Valid(legacy) || string(legacy) == "null" {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "features.json")
	os.WriteFile(path, []byte("[]"), 0600)
	t.Setenv("GOSVM_TEST_FEATURES", path)
	explicit, err := FeaturesFor("solana-v3")
	if err != nil || string(explicit) != "[]" {
		t.Fatal(string(explicit), err)
	}
}
