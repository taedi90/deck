package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/taedi90/deck/internal/config"
	"github.com/taedi90/deck/internal/diagnose"
	"github.com/taedi90/deck/internal/install"
	"github.com/taedi90/deck/internal/prepare"
	"github.com/taedi90/deck/internal/validate"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "deck: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return usageError()
	}

	switch args[0] {
	case "validate":
		return runValidate(args[1:])
	case "run":
		return runRun(args[1:])
	case "resume":
		return runResume(args[1:])
	case "diagnose":
		return runDiagnose(args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runValidate(args []string) error {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	file := fs.String("f", "", "path to workflow file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *file == "" {
		return errors.New("-f is required")
	}

	if err := validate.File(*file); err != nil {
		return err
	}

	fmt.Fprintln(os.Stdout, "validate: ok")
	return nil
}

func runRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	file := fs.String("file", "", "path to workflow file")
	phase := fs.String("phase", "", "phase to execute (prepare/install)")
	bundle := fs.String("bundle", "", "bundle output path")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *file == "" {
		return errors.New("--file is required")
	}
	if *phase == "" {
		return errors.New("--phase is required")
	}

	if err := validate.File(*file); err != nil {
		return err
	}

	wf, err := config.Load(*file)
	if err != nil {
		return err
	}

	switch *phase {
	case "prepare":
		if err := prepare.Run(wf, prepare.RunOptions{BundleRoot: *bundle}); err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, "run prepare: ok")
		return nil
	case "install":
		if err := install.Run(wf, install.RunOptions{BundleRoot: *bundle}); err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, "run install: ok")
		return nil
	default:
		return fmt.Errorf("unsupported phase: %s", *phase)
	}
}

func runResume(args []string) error {
	fs := flag.NewFlagSet("resume", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	file := fs.String("file", "", "path to workflow file")
	bundle := fs.String("bundle", "", "bundle path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *file == "" {
		return errors.New("--file is required")
	}

	if err := validate.File(*file); err != nil {
		return err
	}

	wf, err := config.Load(*file)
	if err != nil {
		return err
	}
	if err := install.Run(wf, install.RunOptions{BundleRoot: *bundle}); err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, "resume install: ok")
	return nil
}

func runDiagnose(args []string) error {
	fs := flag.NewFlagSet("diagnose", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	file := fs.String("file", "", "path to workflow file")
	bundle := fs.String("bundle", "", "bundle path")
	preflight := fs.Bool("preflight", false, "run preflight checks")
	out := fs.String("out", "reports/diagnose.json", "diagnose report output path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *file == "" {
		return errors.New("--file is required")
	}
	if !*preflight {
		return errors.New("only --preflight mode is supported")
	}

	if err := validate.File(*file); err != nil {
		return err
	}

	wf, err := config.Load(*file)
	if err != nil {
		return err
	}

	report, err := diagnose.Preflight(wf, diagnose.RunOptions{WorkflowPath: *file, BundleRoot: *bundle, OutputPath: *out})
	if err != nil {
		fmt.Fprintf(os.Stdout, "diagnose preflight: failed (%d failed checks)\n", report.Summary.Failed)
		return err
	}

	fmt.Fprintf(os.Stdout, "diagnose preflight: ok (%d checks)\n", report.Summary.Passed)
	fmt.Fprintf(os.Stdout, "diagnose report: %s\n", *out)
	return nil
}

func usageError() error {
	return errors.New("usage: deck validate -f <file> | deck run --file <file> --phase <phase> | deck resume --file <file> | deck diagnose --preflight --file <file>")
}
