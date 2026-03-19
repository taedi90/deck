package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/taedi90/deck/internal/filemode"
	"github.com/taedi90/deck/internal/fsutil"
	"github.com/taedi90/deck/internal/userdirs"
)

type sourceDefaults struct {
	URL string `json:"url"`
}

func sourceDefaultsPath() (string, error) {
	if raw := strings.TrimSpace(os.Getenv("DECK_SERVER_CONFIG_PATH")); raw != "" {
		return raw, nil
	}
	return userdirs.ConfigFile("source.json")
}

func resolveSourceURL(explicit string) (string, string, error) {
	if trimmed := strings.TrimRight(strings.TrimSpace(explicit), "/"); trimmed != "" {
		if err := validateSourceURL(trimmed); err != nil {
			return "", "", err
		}
		return trimmed, "flag", nil
	}
	if raw := strings.TrimRight(strings.TrimSpace(os.Getenv("DECK_SERVER")), "/"); raw != "" {
		if err := validateSourceURL(raw); err != nil {
			return "", "", fmt.Errorf("invalid DECK_SERVER: %w", err)
		}
		return raw, "env", nil
	}
	defaults, err := loadSourceDefaults()
	if err != nil {
		return "", "", err
	}
	if strings.TrimSpace(defaults.URL) == "" {
		return "", "", nil
	}
	if err := validateSourceURL(defaults.URL); err != nil {
		return "", "", fmt.Errorf("invalid saved remote server URL: %w", err)
	}
	return defaults.URL, "config", nil
}

func resolveRequiredSourceURL(explicit string) (string, string, error) {
	resolved, source, err := resolveSourceURL(explicit)
	if err != nil {
		return "", "", err
	}
	if resolved == "" {
		return "", "", errors.New("--server is required or set a default with \"deck server remote set <url>\"")
	}
	return resolved, source, nil
}

func loadSourceDefaults() (sourceDefaults, error) {
	path, err := sourceDefaultsPath()
	if err != nil {
		return sourceDefaults{}, err
	}
	raw, err := fsutil.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			if strings.TrimSpace(os.Getenv("DECK_SERVER_CONFIG_PATH")) != "" {
				return sourceDefaults{}, nil
			}
			legacyDefaults, found, legacyErr := loadLegacySourceDefaults()
			if legacyErr != nil {
				return sourceDefaults{}, legacyErr
			}
			if !found {
				return sourceDefaults{}, nil
			}
			return legacyDefaults, nil
		} else {
			return sourceDefaults{}, fmt.Errorf("read source defaults: %w", err)
		}
	}
	disk := map[string]string{}
	if err := json.Unmarshal(raw, &disk); err != nil {
		return sourceDefaults{}, fmt.Errorf("decode source defaults: %w", err)
	}
	return sourceDefaults{URL: strings.TrimRight(strings.TrimSpace(disk["url"]), "/")}, nil
}

func saveSourceDefaults(defaults sourceDefaults) error {
	path, err := sourceDefaultsPath()
	if err != nil {
		return err
	}
	defaults.URL = strings.TrimRight(strings.TrimSpace(defaults.URL), "/")
	payload := map[string]string{"url": defaults.URL}
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("encode source defaults: %w", err)
	}
	if err := filemode.WritePrivateFile(path, raw); err != nil {
		return fmt.Errorf("write source defaults: %w", err)
	}
	return nil
}

func clearSourceDefaults() error {
	path, err := sourceDefaultsPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove source defaults: %w", err)
	}
	return nil
}

func validateSourceURL(raw string) error {
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	if trimmed == "" {
		return errors.New("source URL must not be empty")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return fmt.Errorf("parse source URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("source URL must use http or https")
	}
	if strings.TrimSpace(parsed.Host) == "" {
		return errors.New("source URL host must not be empty")
	}
	return nil
}
