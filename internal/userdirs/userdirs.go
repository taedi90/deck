package userdirs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func ConfigRoot() (string, error) {
	if raw := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); raw != "" {
		return filepath.Join(raw, "deck"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home directory: %w", err)
	}
	return filepath.Join(home, ".config", "deck"), nil
}

func StateRoot() (string, error) {
	if raw := strings.TrimSpace(os.Getenv("XDG_STATE_HOME")); raw != "" {
		return filepath.Join(raw, "deck"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home directory: %w", err)
	}
	return filepath.Join(home, ".local", "state", "deck"), nil
}

func CacheRoot() (string, error) {
	if raw := strings.TrimSpace(os.Getenv("XDG_CACHE_HOME")); raw != "" {
		return filepath.Join(raw, "deck"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home directory: %w", err)
	}
	return filepath.Join(home, ".cache", "deck"), nil
}

func ConfigFile(name string) (string, error) {
	root, err := ConfigRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, name), nil
}

func StateFile(name string) (string, error) {
	root, err := StateRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "state", name), nil
}

func RunsRoot() (string, error) {
	root, err := StateRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "runs"), nil
}

func LegacyDeckRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home directory: %w", err)
	}
	return filepath.Join(home, ".deck"), nil
}

func LegacyConfigFile(name string) (string, error) {
	root, err := LegacyDeckRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, name), nil
}

func LegacyStateFile(name string) (string, error) {
	root, err := LegacyDeckRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "state", name), nil
}

func LegacyCacheRoot() (string, error) {
	root, err := LegacyDeckRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "cache"), nil
}
