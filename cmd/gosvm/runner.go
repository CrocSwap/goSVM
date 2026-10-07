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
		return fmt.Errorf("usage: gosvm runner install [-archive pinned-package.tar.gz] | status")
	}
	fs := flag.NewFlagSet("gosvm runner "+args[0], flag.ContinueOnError)
	archive := ""
	if args[0] == "install" {
		fs.StringVar(&archive, "archive", "", "local package matching the frontend's pinned SHA-256")
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
		return runner.Install(ctx, runner.Root(), archive, os.Stdout)
	case "status":
		pin, err := runner.Verify(runner.Root())
		if err != nil {
			return fmt.Errorf("managed runner unavailable: %w; run gosvm runner install -archive <pinned-package>", err)
		}
		fmt.Printf("Verified %s (%s): %s\n", pin.Identity, pin.Platform, runner.Root())
		return nil
	default:
		return fmt.Errorf("unknown runner command %q", args[0])
	}
}
