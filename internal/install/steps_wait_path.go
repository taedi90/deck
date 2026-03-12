package install

import (
	"context"
	"fmt"
	"os"
	"time"
)

func runWaitPath(parent context.Context, spec map[string]any) error {
	path := stringValue(spec, "path")
	if path == "" {
		return fmt.Errorf("%s: WaitPath requires path", errCodeInstallWaitPathRequired)
	}

	state := stringValue(spec, "state")
	if state != "exists" && state != "absent" {
		return fmt.Errorf("%s: WaitPath state must be exists or absent", errCodeInstallWaitPathState)
	}

	pathType := stringValue(spec, "type")
	if pathType == "" {
		pathType = "any"
	}
	if pathType != "any" && pathType != "file" && pathType != "dir" {
		return fmt.Errorf("%s: WaitPath type must be any, file, or dir", errCodeInstallWaitPathType)
	}

	nonEmpty := boolValue(spec, "nonEmpty")
	if nonEmpty && state != "exists" {
		return fmt.Errorf("%s: WaitPath nonEmpty is only supported when state is exists", errCodeInstallWaitPathState)
	}

	pollInterval := 500 * time.Millisecond
	if raw := stringValue(spec, "pollInterval"); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed <= 0 {
			return fmt.Errorf("%s: invalid pollInterval %q", errCodeInstallWaitPathPoll, raw)
		}
		pollInterval = parsed
	}

	timeout := commandTimeout(spec)
	if parent == nil {
		parent = context.Background()
	}
	if err := parent.Err(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	expected := waitPathExpectedCondition(path, state, pathType, nonEmpty)
	for {
		matched, err := waitPathConditionMet(path, state, pathType, nonEmpty)
		if err != nil {
			return err
		}
		if matched {
			return nil
		}

		select {
		case <-ctx.Done():
			if parent.Err() != nil {
				return parent.Err()
			}
			if ctx.Err() == context.DeadlineExceeded {
				return fmt.Errorf("%s: timed out after %s waiting for %s", errCodeInstallWaitPathTimeout, timeout, expected)
			}
			return ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

func waitPathConditionMet(path, state, pathType string, nonEmpty bool) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return state == "absent", nil
		}
		return false, err
	}

	if state == "absent" {
		return false, nil
	}

	switch pathType {
	case "file":
		if !info.Mode().IsRegular() {
			return false, nil
		}
	case "dir":
		if !info.IsDir() {
			return false, nil
		}
	}

	if nonEmpty {
		if !info.Mode().IsRegular() {
			return false, nil
		}
		if info.Size() <= 0 {
			return false, nil
		}
	}

	return true, nil
}

func waitPathExpectedCondition(path, state, pathType string, nonEmpty bool) string {
	if state == "absent" {
		return fmt.Sprintf("path %q to be absent", path)
	}

	suffix := "exist"
	if nonEmpty {
		suffix = "exist as a non-empty file"
	} else {
		switch pathType {
		case "file":
			suffix = "exist as a file"
		case "dir":
			suffix = "exist as a directory"
		}
	}

	return fmt.Sprintf("path %q to %s", path, suffix)
}
