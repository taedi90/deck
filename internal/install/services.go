package install

import (
	"errors"
	"os/exec"
	"time"
)

func isServiceEnabled(name string, timeout time.Duration) (bool, error) {
	err := runTimedCommand("systemctl", []string{"is-enabled", name}, timeout)
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return false, nil
	}
	return false, err
}

func isServiceActive(name string, timeout time.Duration) (bool, error) {
	err := runTimedCommand("systemctl", []string{"is-active", "--quiet", name}, timeout)
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return false, nil
	}
	return false, err
}
