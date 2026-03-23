package install

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/taedi90/deck/internal/errcode"
	"github.com/taedi90/deck/internal/executil"
	"github.com/taedi90/deck/internal/stepspec"
)

func runWaitDecoded(parent context.Context, kind string, decoded stepspec.Wait, timeout time.Duration) error {
	interval := 500 * time.Millisecond
	if raw := firstNonEmpty(decoded.Interval, decoded.PollInterval); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed <= 0 {
			return fmt.Errorf("invalid Wait interval %q", raw)
		}
		interval = parsed
	}
	initialDelay := time.Duration(0)
	if raw := decoded.InitialDelay; raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed < 0 {
			return fmt.Errorf("invalid Wait initialDelay %q", raw)
		}
		initialDelay = parsed
	}
	if parent == nil {
		return fmt.Errorf("context is nil")
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	if initialDelay > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(initialDelay):
		}
	}
	for {
		ok, err := waitConditionMet(ctx, kind, decoded)
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		select {
		case <-ctx.Done():
			detail := kind
			if p := decoded.Path; p != "" {
				if kind == "WaitForFile" || kind == "WaitForMissingFile" {
					detail = waitPathExpectedCondition(p, map[string]string{"WaitForFile": "exists", "WaitForMissingFile": "absent"}[kind], waitPathType(decoded), decoded.NonEmpty)
				} else {
					detail = fmt.Sprintf("%s (%s)", kind, p)
				}
			} else if kind == "WaitForMissingFile" {
				detail = waitMissingPathExpectedCondition(decoded)
			}
			return errcode.Newf(errCodeInstallWaitTimeout, "timed out after %s for %s", timeout, detail)
		case <-time.After(interval):
		}
	}
}

func waitConditionMet(ctx context.Context, kind string, spec stepspec.Wait) (bool, error) {
	switch kind {
	case "WaitForFile":
		return waitPathConditionMet(spec.Path, "exists", waitPathType(spec), spec.NonEmpty)
	case "WaitForMissingFile":
		return waitMissingPathConditionMet(spec)
	case "WaitForService":
		name := spec.Name
		if name == "" {
			return false, fmt.Errorf("wait.service-active requires name")
		}
		err := executil.RunSystemctl(ctx, "is-active", "--quiet", name)
		if err == nil {
			return true, nil
		}
		if executil.IsExitError(err) {
			return false, nil
		}
		return false, err
	case "WaitForCommand":
		cmd := spec.Command
		if len(cmd) == 0 {
			return false, fmt.Errorf("wait.command-success requires command")
		}
		err := executil.RunWorkflowCommand(ctx, cmd[0], cmd[1:]...)
		if err == nil {
			return true, nil
		}
		if executil.IsExitError(err) {
			return false, nil
		}
		return false, err
	case "WaitForMissingTCPPort", "WaitForTCPPort":
		address := spec.Address
		if address == "" {
			address = "127.0.0.1"
		}
		port := spec.Port
		if port == "" {
			return false, fmt.Errorf("%s requires port", kind)
		}
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(address, port), 500*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return kind == "WaitForTCPPort", nil
		}
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			return false, nil
		}
		if os.IsTimeout(err) {
			return false, nil
		}
		return kind == "WaitForMissingTCPPort", nil
	default:
		return false, fmt.Errorf("unsupported wait kind %q", kind)
	}
}

func waitMissingPathConditionMet(spec stepspec.Wait) (bool, error) {
	if spec.Path != "" {
		return waitPathConditionMet(spec.Path, "absent", waitPathType(spec), false)
	}
	if len(spec.Paths) > 0 {
		for _, path := range spec.Paths {
			ok, err := waitPathConditionMet(path, "absent", waitPathType(spec), false)
			if err != nil {
				return false, err
			}
			if !ok {
				return false, nil
			}
		}
		return true, nil
	}
	if spec.Glob != "" {
		matches, err := filepath.Glob(spec.Glob)
		if err != nil {
			return false, err
		}
		return len(matches) == 0, nil
	}
	return false, fmt.Errorf("wait.file-absent requires path, paths, or glob")
}

func waitPathType(spec stepspec.Wait) string {
	pathType := spec.Type
	if pathType == "" {
		pathType = "any"
	}
	return pathType
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
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
	case "any":
	case "file":
		if info.IsDir() {
			return false, nil
		}
	case "dir":
		if !info.IsDir() {
			return false, nil
		}
	default:
		return false, fmt.Errorf("invalid Wait type %q", pathType)
	}
	if nonEmpty && info.Size() == 0 {
		return false, nil
	}
	return true, nil
}

func waitPathExpectedCondition(path, state, pathType string, nonEmpty bool) string {
	condition := "exist"
	switch {
	case state == "absent":
		condition = "be absent"
	case pathType == "file":
		condition = "exist as a file"
	case pathType == "dir":
		condition = "exist as a directory"
	}
	if nonEmpty {
		condition += " and be non-empty"
	}
	return fmt.Sprintf("%s to %s", path, condition)
}

func waitMissingPathExpectedCondition(spec stepspec.Wait) string {
	if len(spec.Paths) > 0 {
		return fmt.Sprintf("%d paths to be absent", len(spec.Paths))
	}
	if spec.Glob != "" {
		return fmt.Sprintf("glob %s to have no matches", spec.Glob)
	}
	return "path to be absent"
}
