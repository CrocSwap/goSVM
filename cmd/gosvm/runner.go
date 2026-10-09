package main

import (
	"context"
	"flag"
	"fmt"
	"gosvm/internal/runner"
	"os"
	"os/signal"
)

func runnerCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: gosvm runner install -archive pinned-package.tar.gz | status")
	}
	fs := flag.NewFlagSet("gosvm runner "+args[0], flag.ContinueOnError)
	archive := ""
	if args[0] == "install" {
		fs.StringVar(&archive, "archive", "", "local runner package matching one of the frontend's pinned SHA-256s (0.5.0 or 0.6.0)")
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
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		return runner.Install(ctx, archive, os.Stdout)
	case "status":
		missing := 0
		for _, version := range runner.Versions() {
			root := runner.RootFor(version)
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
