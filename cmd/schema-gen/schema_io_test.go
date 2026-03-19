package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/taedi90/deck/internal/workflowcontract"
)

func TestEnsureRegistrySchemaFilesRejectsUnknownSchema(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "unknown.schema.json")
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write schema: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	err = ensureRegistrySchemaFiles(dir, entries)
	if err == nil {
		t.Fatalf("expected unknown schema error")
	}
	if !strings.Contains(err.Error(), "unknown tool schema file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadToolPageInputsIncludesOnlyPublicSteps(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	pages, err := loadToolPageInputs(filepath.Join(root, "schemas", "tools"))
	if err != nil {
		t.Fatalf("loadToolPageInputs: %v", err)
	}
	if len(pages) == 0 {
		t.Fatalf("expected public tool pages")
	}
	for _, page := range pages {
		if len(page.Variants) == 0 {
			t.Fatalf("expected grouped variants for page %s", page.PageSlug)
		}
		for _, variant := range page.Variants {
			def, ok := workflowcontract.StepDefinitionForKind(variant.Kind)
			if !ok {
				t.Fatalf("missing step definition for %s", variant.Kind)
			}
			if def.Visibility != "public" {
				t.Fatalf("non-public step %s should not be rendered", variant.Kind)
			}
		}
	}
	for _, def := range workflowcontract.StepDefinitions() {
		if def.Visibility == "public" {
			continue
		}
		for _, page := range pages {
			for _, variant := range page.Variants {
				if variant.Kind == def.Kind {
					t.Fatalf("did not expect %s in public schema docs", def.Kind)
				}
			}
		}
	}
}
