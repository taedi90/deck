package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/taedi90/deck/internal/filemode"
	"github.com/taedi90/deck/internal/fsutil"
)

type serverDefaults struct {
	URL       string `json:"url"`
	AuthToken string `json:"authToken,omitempty"`
}

func serverDefaultsPath() (string, error) {
	if raw := strings.TrimSpace(os.Getenv("DECK_SERVER_CONFIG_PATH")); raw != "" {
		return raw, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home directory: %w", err)
	}
	return filepath.Join(home, ".deck", "server.json"), nil
}

func resolveServerURL(explicit string) (string, string, error) {
	if trimmed := strings.TrimRight(strings.TrimSpace(explicit), "/"); trimmed != "" {
		if err := validateServerURL(trimmed); err != nil {
			return "", "", err
		}
		return trimmed, "flag", nil
	}
	if raw := strings.TrimRight(strings.TrimSpace(os.Getenv("DECK_SERVER")), "/"); raw != "" {
		if err := validateServerURL(raw); err != nil {
			return "", "", fmt.Errorf("invalid DECK_SERVER: %w", err)
		}
		return raw, "env", nil
	}
	defaults, err := loadServerDefaults()
	if err != nil {
		return "", "", err
	}
	if strings.TrimSpace(defaults.URL) == "" {
		return "", "", nil
	}
	if err := validateServerURL(defaults.URL); err != nil {
		return "", "", fmt.Errorf("invalid saved default server: %w", err)
	}
	return defaults.URL, "config", nil
}

func resolveServerAuthToken(explicit string) (string, string, error) {
	if trimmed := strings.TrimSpace(explicit); trimmed != "" {
		return trimmed, "flag", nil
	}
	if raw := strings.TrimSpace(os.Getenv("DECK_API_TOKEN")); raw != "" {
		return raw, "env", nil
	}
	defaults, err := loadServerDefaults()
	if err != nil {
		return "", "", err
	}
	if strings.TrimSpace(defaults.AuthToken) == "" {
		return "", "", nil
	}
	return defaults.AuthToken, "config", nil
}

func resolveRequiredServerURL(explicit string) (string, string, error) {
	resolved, source, err := resolveServerURL(explicit)
	if err != nil {
		return "", "", err
	}
	if resolved == "" {
		return "", "", errors.New("--server is required or set a default with \"deck server set <url>\"")
	}
	return resolved, source, nil
}

func loadServerDefaults() (serverDefaults, error) {
	path, err := serverDefaultsPath()
	if err != nil {
		return serverDefaults{}, err
	}
	raw, err := fsutil.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return serverDefaults{}, nil
		}
		return serverDefaults{}, fmt.Errorf("read server defaults: %w", err)
	}
	disk := map[string]string{}
	if err := json.Unmarshal(raw, &disk); err != nil {
		return serverDefaults{}, fmt.Errorf("decode server defaults: %w", err)
	}
	defaults := serverDefaults{URL: disk["url"], AuthToken: disk["authToken"]}
	if defaults.AuthToken == "" {
		defaults.AuthToken = disk["apiToken"]
	}
	defaults.URL = strings.TrimRight(strings.TrimSpace(defaults.URL), "/")
	defaults.AuthToken = strings.TrimSpace(defaults.AuthToken)
	return defaults, nil
}

func saveServerDefaults(defaults serverDefaults) error {
	path, err := serverDefaultsPath()
	if err != nil {
		return err
	}
	defaults.URL = strings.TrimRight(strings.TrimSpace(defaults.URL), "/")
	defaults.AuthToken = strings.TrimSpace(defaults.AuthToken)
	payload := map[string]string{"url": defaults.URL}
	if defaults.AuthToken != "" {
		payload["authToken"] = defaults.AuthToken
	}
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("encode server defaults: %w", err)
	}
	if err := filemode.WritePrivateFile(path, raw); err != nil {
		return fmt.Errorf("write server defaults: %w", err)
	}
	return nil
}

func clearServerDefaults() error {
	path, err := serverDefaultsPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove server defaults: %w", err)
	}
	return nil
}

func validateServerURL(raw string) error {
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	if trimmed == "" {
		return errors.New("server URL must not be empty")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return fmt.Errorf("parse server URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("server URL must use http or https")
	}
	if strings.TrimSpace(parsed.Host) == "" {
		return errors.New("server URL host must not be empty")
	}
	return nil
}
