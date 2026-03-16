package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAskDependencyGuardRestrictsGollmImports(t *testing.T) {
	repoRoot := filepath.Join("..", "..")
	err := filepath.Walk(repoRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == ".git" || info.Name() == "bin" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		//nolint:gosec // Test-only repository scan over local source files.
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if strings.HasSuffix(filepath.ToSlash(path), "/cmd/deck/ask_dependency_guard_test.go") {
			return nil
		}
		if !strings.Contains(string(raw), "github.com/teilomillet/gollm") {
			return nil
		}
		rel, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			return relErr
		}
		if filepath.ToSlash(rel) != "internal/askprovider/gollm/client.go" {
			t.Fatalf("gollm import is only allowed in internal/askprovider/gollm/client.go, found %s", filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk repository: %v", err)
	}
}
