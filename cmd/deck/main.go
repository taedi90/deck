package main

import (
	"errors"
	"fmt"
	"os"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "deck: %v\n", err)
		if code, ok := extractExitCode(err); ok {
			os.Exit(code)
		}
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return usageError()
	}

	switch args[0] {
	case "-h", "--help", "help":
		return usageError()
	case "pack":
		return runPack(args[1:])
	case "apply":
		return runApply(args[1:])
	case "serve":
		return runServe(args[1:])
	case "bundle":
		return runWorkflowBundle(args[1:])
	case "list":
		return runList(args[1:])
	case "validate":
		return runValidate(args[1:])
	case "diff":
		return runDiff(args[1:])
	case "init":
		return runWorkflowInit(args[1:])
	case "doctor":
		return runDoctor(args[1:])
	case "health":
		return runHealth(args[1:])
	case "logs":
		return runLogs(args[1:])
	case "cache":
		return runCache(args[1:])
	case "node":
		return runNode(args[1:])
	case "site":
		return runSite(args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func usageError() error {
	return errors.New(mainHelpText())
}
