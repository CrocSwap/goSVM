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

const RunnerVersion = runner.Identity

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
		if v != RunnerVersion {
			return fmt.Errorf("require compatibility-spike runner %q; found %q", RunnerVersion, v)
		}
	} else if !strings.Contains(v, " 3.0.15 ") && !strings.HasSuffix(v, " 3.0.15") {
		return fmt.Errorf("require validator 3.0.15; found %s", v)
	}
	return nil
}
