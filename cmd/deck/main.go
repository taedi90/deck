package main

import (
	"fmt"
	"os"
)

func main() {
	res := execute(os.Args[1:])
	if err := writeResult(res); err != nil {
		fmt.Fprintf(os.Stderr, "deck: %v\n", err)
		os.Exit(1)
	}
	if res.err != nil {
		fmt.Fprintf(os.Stderr, "deck: %v\n", res.err)
		os.Exit(res.exitCode)
	}
}

func run(args []string) error {
	res := execute(args)
	if err := writeResult(res); err != nil {
		return err
	}
	return res.err
}

func execute(args []string) cliResult {
	if len(args) == 0 {
		return helpResult(mainHelpText())
	}

	if args[0] == "-h" || args[0] == "--help" {
		return helpResult(mainHelpText())
	}
	if args[0] == "help" {
		text, err := renderHelp(args[1:])
		if err != nil {
			return errorResult(err)
		}
		return helpResult(text)
	}

	var err error
	switch args[0] {
	case "pack":
		err = runPack(args[1:])
	case "apply":
		err = runApply(args[1:])
	case "serve":
		err = runServe(args[1:])
	case "bundle":
		err = runWorkflowBundle(args[1:])
	case "list":
		err = runList(args[1:])
	case "validate":
		err = runValidate(args[1:])
	case "diff":
		err = runDiff(args[1:])
	case "init":
		err = runWorkflowInit(args[1:])
	case "doctor":
		err = runDoctor(args[1:])
	case "health":
		err = runHealth(args[1:])
	case "logs":
		err = runLogs(args[1:])
	case "cache":
		err = runCache(args[1:])
	case "node":
		err = runNode(args[1:])
	case "site":
		err = runSite(args[1:])
	default:
		err = fmt.Errorf("unknown command %q", args[0])
	}
	return errorResult(err)
}
