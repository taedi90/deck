package main

import (
	"fmt"
	"os"

	"github.com/taedi90/deck/internal/userdirs"
)

// resolveLegacyDeckCacheRoot surfaces the pre-XDG cache root for inspection and
// cleanup commands without writing new state there.
func resolveLegacyDeckCacheRoot() (string, bool, error) {
	legacyRoot, err := userdirs.LegacyCacheRoot()
	if err != nil {
		return "", false, err
	}
	if _, err := os.Stat(legacyRoot); err == nil {
		return legacyRoot, true, nil
	} else if err != nil && !os.IsNotExist(err) {
		return "", false, fmt.Errorf("stat legacy cache root: %w", err)
	}
	return "", false, nil
}
