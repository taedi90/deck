package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/taedi90/deck/internal/config"
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
	default:
		return fmt.Errorf("unsupported phase: %s", *phase)
	}
}

func usageError() error {
	return errors.New("usage: deck validate -f <file> | deck run --file <file> --phase <phase>")
}
