package prepare

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/taedi90/deck/internal/fsutil"
)

// loadLegacyPackCacheState reads pre-XDG cache state as a fallback only.
func loadLegacyPackCacheState(path string) ([]byte, bool, error) {
	base := filepath.Base(path)
	workflowSHA := strings.TrimSuffix(base, filepath.Ext(base))
	legacyPath, err := legacyPackCacheStatePath(workflowSHA)
	if err != nil {
		return nil, false, err
	}
	raw, err := fsutil.ReadFile(legacyPath)
	if err == nil {
		return raw, true, nil
	}
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	return nil, false, fmt.Errorf("read legacy pack cache state: %w", err)
}
