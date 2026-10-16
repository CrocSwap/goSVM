package main

import (
	"context"
	"flag"
	"fmt"
	"gosvm/internal/fetch"
	"gosvm/internal/runner"
	"io"
	"os"
	"os/signal"
	"strings"
)

func runnerCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: gosvm runner install [-version 0.5.0|0.6.0] [-archive pinned-package.tar.gz] | status")
	}
	fs := flag.NewFlagSet("gosvm runner "+args[0], flag.ContinueOnError)
	archive, version := "", ""
	if args[0] == "install" {
		fs.StringVar(&archive, "archive", "", "install a local runner package matching one of the frontend's pinned SHA-256s (offline)")
		fs.StringVar(&version, "version", "", "download only this runner version (0.5.0 fogo-v0/legacy, 0.6.0 solana-v3; default both)")
	}
	if err := fs.Parse(args[1:]); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected runner arguments")
	}
	switch args[0] {
	case "install":
		if archive != "" && version != "" {
			return fmt.Errorf("choose either -archive (identifies its version) or -version (downloads)")
		}
		var versions []string
		if version != "" {
			versions = []string{version}
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		return runner.Install(ctx, archive, versions, os.Stdout, fetch.Get)
	case "status":
		missing := 0
		for _, version := range runner.Versions() {
			root := runner.RootFor(version)
			if _, err := os.Lstat(root); os.IsNotExist(err) {
				missing++
				fmt.Printf("MISSING runner %s (%s): not installed\n  fix: %s\n", version, runnerUse(version), runner.InstallHint(version))
				continue
			}
			pin, err := runner.VerifyVersion(version, root)
			if err != nil {
				missing++
				fmt.Printf("MISSING runner %s (%s): %v\n  fix: %s\n", version, runnerUse(version), err, runner.InstallHint(version))
				continue
			}
			fmt.Printf("Verified %s (%s; %s): %s\n", pin.Identity, pin.Platform, runnerUse(version), root)
		}
		if missing == len(runner.Versions()) {
			return fmt.Errorf("no managed runner installed")
		}
		return nil
	default:
		return fmt.Errorf("unknown runner command %q", args[0])
	}
}

func runnerUse(version string) string {
	if version == runner.ModernVersion {
		return "solana-v3"
	}
	return "fogo-v0, legacy-v1.51-v3"
}

// installRunnersAfterToolchain makes `gosvm toolchain install` a one-step
// setup: after the backend, it downloads the pinned runners the installed
// targets select (verified like `gosvm runner install`). A host without pinned
// runners, or a failed download, leaves the backend installed and says what to run.
func installRunnersAfterToolchain(ctx context.Context, versions []string, out io.Writer) error {
	var available []string
	for _, v := range versions {
		if _, err := runner.ReleaseFor(v); err != nil {
			fmt.Fprintf(out, "Note: this gosvm build pins no released SVM runner %s for %s; gosvm test --svm for %s needs -svm-runner or GOSVM_TEST_RUNNER.\n", v, runner.Host(), runnerUse(v))
			continue
		}
		available = append(available, v)
	}
	if len(available) == 0 {
		return nil
	}
	if err := runner.Install(ctx, "", available, out, fetch.Get); err != nil {
		return fmt.Errorf("the backend is installed, but the SVM runner download failed: %w\n  retry: gosvm runner install (or skip runners with gosvm toolchain install -runners=false)", err)
	}
	fmt.Fprintf(out, "SVM runners ready for gosvm test --svm (%s).\n", strings.Join(available, ", "))
	return nil
}
