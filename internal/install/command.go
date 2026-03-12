package install

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/taedi90/deck/internal/workflowexec"
)

type runCommandSpec struct {
	Command []string `json:"command"`
	Timeout string   `json:"timeout"`
}

func runCommand(ctx context.Context, spec map[string]any) error {
	decoded, err := workflowexec.DecodeSpec[runCommandSpec](spec)
	if err != nil {
		return fmt.Errorf("decode RunCommand spec: %w", err)
	}
	cmdArgs := decoded.Command
	if len(cmdArgs) == 0 {
		return fmt.Errorf("%s: RunCommand requires command", errCodeInstallCommandMissing)
	}

	err = runTimedCommandWithContext(ctx, cmdArgs[0], cmdArgs[1:], commandTimeout(spec))
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%s: command timed out after %s", errCodeInstallCommandTimeout, commandTimeout(spec))
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return fmt.Errorf("%s: command exited non-zero: %w", errCodeInstallCommandFailed, err)
	}
	return err
}

func commandTimeout(spec map[string]any) time.Duration {
	return commandTimeoutWithDefault(spec, 30*time.Second)
}

func commandTimeoutWithDefault(spec map[string]any, def time.Duration) time.Duration {
	timeout := def
	if ts := stringValue(spec, "timeout"); ts != "" {
		d, err := time.ParseDuration(ts)
		if err == nil && d > 0 {
			timeout = d
		}
	}
	return timeout
}

func runTimedCommand(name string, args []string, timeout time.Duration) error {
	return runTimedCommandWithContext(context.Background(), name, args, timeout)
}

func runTimedCommandWithContext(parent context.Context, name string, args []string, timeout time.Duration) error {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return err
}

func runCommandOutputWithContext(parent context.Context, cmdArgs []string, timeout time.Duration) (string, error) {
	if len(cmdArgs) == 0 {
		return "", fmt.Errorf("empty command")
	}

	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, cmdArgs[0], cmdArgs[1:]...)
	output, err := cmd.CombinedOutput()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "", fmt.Errorf("command timed out after %s", timeout)
	}
	if err != nil {
		msg := strings.TrimSpace(string(output))
		if msg != "" {
			return "", fmt.Errorf("command failed: %w: %s", err, msg)
		}
		return "", fmt.Errorf("command failed: %w", err)
	}
	return string(output), nil
}
