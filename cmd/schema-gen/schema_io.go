package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/taedi90/deck/internal/fsutil"
	"github.com/taedi90/deck/internal/schemadoc"
	"github.com/taedi90/deck/internal/workflowexec"
)

func repoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	if filepath.Base(wd) == "schemas" {
		return filepath.Dir(wd), nil
	}
	if _, err := os.Stat(filepath.Join(wd, "go.mod")); err == nil {
		return wd, nil
	}
	return "", fmt.Errorf("could not determine repo root from %s", wd)
}

func readSchema(path string) (schemaDoc, error) {
	var out schemaDoc
	raw, err := fsutil.ReadFile(path)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("parse schema %s: %w", path, err)
	}
	return out, nil
}

func readSchemaMap(path string) (map[string]any, error) {
	raw, err := fsutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("parse schema map %s: %w", path, err)
	}
	return out, nil
}

func loadToolPageInputs(dir string) ([]schemadoc.PageInput, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	if err := ensureRegistrySchemaFiles(dir, entries); err != nil {
		return nil, err
	}
	pagesBySlug := map[string]*schemadoc.PageInput{}
	for _, def := range workflowexec.StepDefinitions() {
		if def.Visibility != "public" {
			continue
		}
		path := filepath.Join(dir, def.SchemaFile)
		doc, err := readSchema(path)
		if err != nil {
			return nil, err
		}
		raw, err := readSchemaMap(path)
		if err != nil {
			return nil, err
		}
		kind, err := validateToolSchemaDoc(def, doc)
		if err != nil {
			return nil, err
		}
		page := pagesBySlug[def.DocsPage]
		if page == nil {
			page = &schemadoc.PageInput{
				Family:      def.Family,
				PageSlug:    def.DocsPage,
				Title:       def.FamilyTitle,
				Summary:     familyPageSummary(def),
				Description: familyPageSummary(def),
			}
			pagesBySlug[def.DocsPage] = page
		}
		spec, _ := doc.Properties["spec"].(map[string]any)
		page.Variants = append(page.Variants, schemadoc.VariantInput{
			Kind:        kind,
			Title:       doc.Title,
			Description: doc.Description,
			SchemaPath:  filepath.ToSlash(filepath.Join("schemas", "tools", def.SchemaFile)),
			Schema:      raw,
			Meta:        schemadoc.ToolMeta(kind),
			Required:    nestedRequired(doc.Properties, "spec"),
			Spec:        spec,
			Outputs:     append([]string(nil), def.Outputs...),
			DocsOrder:   def.DocsOrder,
		})
	}
	pages := make([]schemadoc.PageInput, 0, len(pagesBySlug))
	for _, page := range pagesBySlug {
		pages = append(pages, *page)
	}
	sort.Slice(pages, func(i, j int) bool { return pages[i].PageSlug < pages[j].PageSlug })
	return pages, nil
}

func familyPageSummary(def workflowexec.StepDefinition) string {
	if def.Kind == def.Family {
		return def.Summary
	}
	return fmt.Sprintf("Reference for the `%s` family of typed workflow steps.", def.FamilyTitle)
}

func loadToolSchemas(dir string) ([]toolSchemaDoc, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	if err := ensureRegistrySchemaFiles(dir, entries); err != nil {
		return nil, err
	}
	defs := workflowexec.StepDefinitions()
	tools := make([]toolSchemaDoc, 0, len(defs))
	for _, def := range defs {
		raw, err := fsutil.ReadFile(filepath.Join(dir, def.SchemaFile))
		if err != nil {
			return nil, err
		}
		var doc schemaDoc
		if err := json.Unmarshal(raw, &doc); err != nil {
			return nil, fmt.Errorf("parse tool schema %s: %w", def.SchemaFile, err)
		}
		kind, err := validateToolSchemaDoc(def, doc)
		if err != nil {
			return nil, err
		}
		specProps := nestedProperties(doc.Properties, "spec")
		tools = append(tools, toolSchemaDoc{
			File:        def.SchemaFile,
			Kind:        kind,
			Title:       doc.Title,
			Description: doc.Description,
			Visibility:  def.Visibility,
			SpecFields:  sortedKeys(specProps),
			Required:    nestedRequired(doc.Properties, "spec"),
		})
	}
	return tools, nil
}

func ensureRegistrySchemaFiles(dir string, entries []os.DirEntry) error {
	known := map[string]bool{}
	for _, def := range workflowexec.StepDefinitions() {
		known[def.SchemaFile] = true
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".schema.json") {
			continue
		}
		if !known[entry.Name()] {
			return fmt.Errorf("unknown tool schema file %s in %s", entry.Name(), dir)
		}
	}
	return nil
}

func validateToolSchemaDoc(def workflowexec.StepDefinition, doc schemaDoc) (string, error) {
	kind := schemaConst(doc.Properties, "kind")
	if kind != def.Kind {
		return "", fmt.Errorf("tool schema %s kind mismatch: expected %s, got %s", def.SchemaFile, def.Kind, kind)
	}
	if visibility := firstNonEmpty(doc.Visibility, "public"); visibility != def.Visibility {
		return "", fmt.Errorf("tool schema %s visibility mismatch: expected %s, got %s", def.SchemaFile, def.Visibility, visibility)
	}
	return kind, nil
}
