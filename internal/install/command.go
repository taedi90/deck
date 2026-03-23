package install

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/taedi90/deck/internal/errcode"
	"github.com/taedi90/deck/internal/executil"
	"github.com/taedi90/deck/internal/stepspec"
	"github.com/taedi90/deck/internal/workflowexec"
)

var ErrStepCommandTimeout = errors.New("step command timeout")

func runCommand(ctx context.Context, spec map[string]any) error {
	decoded, err := workflowexec.DecodeSpec[stepspec.Command](spec)
	if err != nil {
		return fmt.Errorf("decode Command spec: %w", err)
	}
	return runCommandDecoded(ctx, decoded)
}

func runCommandDecoded(ctx context.Context, decoded stepspec.Command) error {
	cmdArgs := decoded.Command
	if len(cmdArgs) == 0 {
		return errcode.Newf(errCodeInstallCommandMissing, "Command requires command")
	}
	timeout := parseStepTimeout(decoded.Timeout, 30*time.Second)

	err := runTimedCommandSpecWithContext(ctx, cmdArgs, decoded.Env, decoded.Sudo, timeout, os.Stdout, os.Stderr)
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrStepCommandTimeout) {
		return errcode.Newf(errCodeInstallCommandTimeout, "command timed out after %s", timeout)
	}
	if executil.IsExitError(err) {
		return errcode.New(errCodeInstallCommandFailed, fmt.Errorf("command exited non-zero: %w", err))
	}
	return err
}

func commandTimeout(spec map[string]any) time.Duration {
	return commandTimeoutWithDefault(spec, 30*time.Second)
}

func commandTimeoutWithDefault(spec map[string]any, def time.Duration) time.Duration {
	return parseStepTimeout(stringValue(spec, "timeout"), def)
}

func parseStepTimeout(raw string, def time.Duration) time.Duration {
	timeout := def
	if raw != "" {
		d, err := time.ParseDuration(raw)
		if err == nil && d > 0 {
			timeout = d
		}
	}
	return timeout
}

func runTimedCommandWithContext(parent context.Context, name string, args []string, timeout time.Duration) error {
	return runTimedCommandSpecWithContext(parent, append([]string{name}, args...), nil, false, timeout, os.Stdout, os.Stderr)
}

func runTimedCommandSpecWithContext(parent context.Context, cmdArgs []string, env map[string]string, sudo bool, timeout time.Duration, stdout, stderr io.Writer) error {
	if len(cmdArgs) == 0 {
		return fmt.Errorf("empty command")
	}
	if parent == nil {
		return fmt.Errorf("context is nil")
	}
	if err := parent.Err(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	commandArgs := append([]string{}, cmdArgs...)
	if sudo {
		commandArgs = append([]string{"sudo"}, commandArgs...)
	}
	// #nosec G204 -- Command intentionally executes the workflow-provided command vector.
	command := exec.CommandContext(ctx, commandArgs[0], commandArgs[1:]...)
	command.Stdout = stdout
	command.Stderr = stderr
	if len(env) > 0 {
		command.Env = os.Environ()
		for key, value := range env {
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
			command.Env = append(command.Env, key+"="+value)
		}
	}
	err := command.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		if parent.Err() != nil {
			return parent.Err()
		}
		return ErrStepCommandTimeout
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		if parent.Err() != nil {
			return parent.Err()
		}
		return context.Canceled
	}
	return err
}

func runCommandOutputWithContext(parent context.Context, cmdArgs []string, timeout time.Duration) (string, error) {
	if len(cmdArgs) == 0 {
		return "", fmt.Errorf("empty command")
	}

	if parent == nil {
		return "", fmt.Errorf("context is nil")
	}
	if err := parent.Err(); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	output, err := executil.CombinedOutputWorkflowCommand(ctx, cmdArgs[0], cmdArgs[1:]...)
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		if parent.Err() != nil {
			return "", parent.Err()
		}
		return "", ErrStepCommandTimeout
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		if parent.Err() != nil {
			return "", parent.Err()
		}
		return "", context.Canceled
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
