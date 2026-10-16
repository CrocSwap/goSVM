// Package testvm selects the local integration-test engine. The lightweight
// runner is an opt-in compatibility experiment; the validator remains default.
package testvm

import (
	"fmt"
	"gosvm/internal/runner"
	"os"
	"os/exec"
	"strings"
	"time"
)

// RunnerVersion is the current LiteSVM 0.8.2 runner (fogo-v0, legacy);
// HistoricalRunnerVersion is the 0.5.0 runner it superseded, still accepted.
const RunnerVersion = runner.FogoIdentity
const HistoricalRunnerVersion = runner.Identity

func IsRunner() bool { return os.Getenv("GOSVM_TEST_RUNNER") != "" }

func ReadyPoll() time.Duration {
	if IsRunner() {
		return 10 * time.Millisecond
	}
	return 200 * time.Millisecond
}

func Command(args ...string) *exec.Cmd {
	name := os.Getenv("GOSVM_TEST_RUNNER")
	if name == "" {
		name = "solana-test-validator"
	}
	return exec.Command(name, args...)
}

func Engine() string {
	if IsRunner() {
		return "litesvm"
	}
	return "validator"
}

func CheckVersion(version string) error {
	v := strings.TrimSpace(version)
	if IsRunner() {
		if v != RunnerVersion && v != HistoricalRunnerVersion {
			return fmt.Errorf("require compatibility-spike runner %q (or %q); found %q", RunnerVersion, HistoricalRunnerVersion, v)
		}
	} else if !strings.Contains(v, " 3.0.15 ") && !strings.HasSuffix(v, " 3.0.15") {
		return fmt.Errorf("require validator 3.0.15; found %s", v)
	}
	return nil
}
