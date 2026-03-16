package executil

import (
	"context"
	"errors"
	"os/exec"
)

func IsExitError(err error) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr)
}

func IsExecutableNotFound(err error) bool {
	var execErr *exec.Error
	return errors.As(err, &execErr)
}

func LookPathAptGet() (string, error) {
	return exec.LookPath("apt-get")
}

func LookPathDnf() (string, error) {
	return exec.LookPath("dnf")
}

func LookPathSystemctl() (string, error) {
	return exec.LookPath("systemctl")
}

func LookPathSystemdRun() (string, error) {
	return exec.LookPath("systemd-run")
}

func RunSystemctl(ctx context.Context, args ...string) error {
	return exec.CommandContext(ctx, "systemctl", args...).Run()
}

func CombinedOutputSystemctl(ctx context.Context, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, "systemctl", args...).CombinedOutput()
}

func CombinedOutputSystemdRun(ctx context.Context, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, "systemd-run", args...).CombinedOutput()
}

func CombinedOutputJournalctl(ctx context.Context, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, "journalctl", args...).CombinedOutput()
}
