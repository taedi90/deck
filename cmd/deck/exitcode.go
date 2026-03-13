package main

import "errors"

const defaultExitCode = 1

type exitCodeError struct {
	code int
	err  error
}

func (e *exitCodeError) Error() string {
	return e.err.Error()
}

func (e *exitCodeError) Unwrap() error {
	return e.err
}

func extractExitCode(err error) (int, bool) {
	var coded *exitCodeError
	if !errors.As(err, &coded) {
		return 0, false
	}
	if coded.code <= 0 {
		return defaultExitCode, true
	}
	return coded.code, true
}

func resolveExitCode(err error) int {
	if code, ok := extractExitCode(err); ok {
		return code
	}
	return defaultExitCode
}
