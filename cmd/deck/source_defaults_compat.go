package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/taedi90/deck/internal/fsutil"
	"github.com/taedi90/deck/internal/userdirs"
)

func legacySourceDefaultsPath() (string, error) {
	return userdirs.LegacyConfigFile("server.json")
}

func loadLegacySourceDefaults() (sourceDefaults, bool, error) {
	legacyPath, err := legacySourceDefaultsPath()
	if err != nil {
		return sourceDefaults{}, false, err
	}
	raw, err := fsutil.ReadFile(legacyPath)
	if err != nil {
		if os.IsNotExist(err) {
			return sourceDefaults{}, false, nil
		}
		return sourceDefaults{}, false, fmt.Errorf("read legacy source defaults: %w", err)
	}
	disk := map[string]string{}
	if err := json.Unmarshal(raw, &disk); err != nil {
		return sourceDefaults{}, false, fmt.Errorf("decode legacy source defaults: %w", err)
	}
	return sourceDefaults{URL: strings.TrimRight(strings.TrimSpace(disk["url"]), "/")}, true, nil
}
