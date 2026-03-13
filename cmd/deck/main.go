package main

import (
	"bytes"
	"fmt"
	"os"
	"strings"
)

func main() {
	res := execute(os.Args[1:])
	if err := writeResult(res); err != nil {
		fmt.Fprintf(os.Stderr, "deck: %v\n", err)
		os.Exit(1)
	}
	if res.err != nil {
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
	root := newRootCommand()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(args)
	if _, err := root.ExecuteC(); err != nil {
		res := errorResult(err)
		res.stdout = stdout.String() + res.stdout
		res.stderr = formatCLIError(stderr.String(), err)
		return res
	}
	return cliResult{stdout: stdout.String(), stderr: stderr.String()}
}

func formatCLIError(existing string, err error) string {
	formatted := strings.TrimRight(existing, "\n")
	message := fmt.Sprintf("Error: %v", err)
	if formatted == "" {
		return message + "\n"
	}
	if !strings.Contains(formatted, message) {
		formatted += "\n" + message
	}
	return formatted + "\n"
}
