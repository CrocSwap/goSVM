package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"time"

	"gosvm/internal/sbftest"
	"gosvm/internal/targets"
)

func svmTestCommand(args []string) error {
	fs := flag.NewFlagSet("gosvm svm-test", flag.ContinueOnError)
	profile := fs.String("target", target.Legacy, "ELF target profile")
	elf := fs.String("elf", "", "already-compiled SBF ELF matching -target (required)")
	fixtures := fs.String("fixtures", "", "general fixture JSON file (required)")
	report := fs.String("report", "build/svm-results.json", "report path (removed before execution)")
	runner := fs.String("runner", "", "runner binary (or GOSVM_TEST_RUNNER/PATH)")
	pattern := fs.String("run", "", "scenario name regular expression")
	if e := fs.Parse(args); e != nil {
		if e == flag.ErrHelp {
			return nil
		}
		return e
	}
	if fs.NArg() != 0 || *elf == "" || *fixtures == "" || *report == "" {
		return fmt.Errorf("svm-test requires -elf and -fixtures, and accepts no positional arguments")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	p, err := target.Resolve(*profile)
	if err != nil {
		return err
	}
	return sbftest.RunGeneral(ctx, *elf, *fixtures, *report, os.Stdout, sbftest.FastOptions{Runner: *runner, Pattern: *pattern, Profile: p.Name, PlatformTools: p.Tools})
}
