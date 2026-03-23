package install

import (
	"fmt"
	"os"
	"strings"

	"github.com/taedi90/deck/internal/config"
)

// resolveLegacyStateReadPath keeps read-only fallback support for pre-XDG apply
// state files while new writes continue to use the canonical state root.
func resolveLegacyStateReadPath(wf *config.Workflow, preferredPath string) (string, bool, error) {
	if wf == nil || strings.TrimSpace(wf.StateKey) == "" {
		return strings.TrimSpace(preferredPath), false, nil
	}
	legacyPath, err := LegacyStatePath(wf)
	if err != nil {
		return "", false, err
	}
	if legacyPath == strings.TrimSpace(preferredPath) {
		return strings.TrimSpace(preferredPath), false, nil
	}
	if _, err := os.Stat(legacyPath); err == nil {
		return legacyPath, true, nil
	} else if err != nil && !os.IsNotExist(err) {
		return "", false, fmt.Errorf("stat legacy state file: %w", err)
	}
	return strings.TrimSpace(preferredPath), false, nil
}
