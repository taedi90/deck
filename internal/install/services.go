package install

import (
	"strings"
	"time"

	"github.com/taedi90/deck/internal/executil"
)

func isServiceEnabled(name string, timeout time.Duration) (bool, error) {
	err := runTimedCommand("systemctl", []string{"is-enabled", name}, timeout)
	if err == nil {
		return true, nil
	}
	if executil.IsExitError(err) {
		return false, nil
	}
	return false, err
}

func isServiceActive(name string, timeout time.Duration) (bool, error) {
	err := runTimedCommand("systemctl", []string{"is-active", "--quiet", name}, timeout)
	if err == nil {
		return true, nil
	}
	if executil.IsExitError(err) {
		return false, nil
	}
	return false, err
}

func serviceUnitExists(name string, timeout time.Duration) (bool, error) {
	err := runTimedCommand("systemctl", []string{"list-unit-files", serviceUnitLookupName(name)}, timeout)
	if err == nil {
		return true, nil
	}
	if executil.IsExitError(err) {
		return false, nil
	}
	return false, err
}

func serviceUnitLookupName(name string) string {
	trimmed := strings.TrimSpace(name)
	if strings.Contains(trimmed, ".") {
		return trimmed
	}
	return trimmed + ".service"
}
