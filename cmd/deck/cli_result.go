package main

import (
	"fmt"
	"os"
)

type cliResult struct {
	stdout   string
	stderr   string
	err      error
	exitCode int
}

func errorResult(err error) cliResult {
	if err == nil {
		return cliResult{}
	}
	return cliResult{err: err, exitCode: resolveExitCode(err)}
}

func writeResult(res cliResult) error {
	if res.stdout != "" {
		if _, err := fmt.Fprint(os.Stdout, res.stdout); err != nil {
			return err
		}
	}
	if res.stderr != "" {
		if _, err := fmt.Fprint(os.Stderr, res.stderr); err != nil {
			return err
		}
	}
	return nil
}
