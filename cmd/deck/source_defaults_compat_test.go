package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSourceDefaultsReadLegacyHomePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	legacyPath := filepath.Join(home, ".deck", "server.json")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
		t.Fatalf("mkdir legacy config dir: %v", err)
	}
	if err := os.WriteFile(legacyPath, []byte("{\n  \"url\": \"http://127.0.0.1:9090\"\n}\n"), 0o600); err != nil {
		t.Fatalf("write legacy server defaults: %v", err)
	}

	resolved, source, err := resolveSourceURL("")
	if err != nil {
		t.Fatalf("resolveSourceURL failed: %v", err)
	}
	if resolved != "http://127.0.0.1:9090" || source != "config" {
		t.Fatalf("unexpected legacy source defaults resolution: resolved=%q source=%q", resolved, source)
	}
}
