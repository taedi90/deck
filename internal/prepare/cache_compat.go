package prepare

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/taedi90/deck/internal/fsutil"
	"github.com/taedi90/deck/internal/userdirs"
)

func resolveLegacyPackageCacheRoot(cacheKey string) (string, bool, error) {
	legacyRoot, err := userdirs.LegacyCacheRoot()
	if err != nil {
		return "", false, err
	}
	legacyPath := filepath.Join(legacyRoot, "packages", strings.TrimSpace(cacheKey))
	if _, err := os.Stat(legacyPath); err == nil {
		return legacyPath, true, nil
	} else if err != nil && !os.IsNotExist(err) {
		return "", false, fmt.Errorf("stat legacy package cache root: %w", err)
	}
	return "", false, nil
}

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
