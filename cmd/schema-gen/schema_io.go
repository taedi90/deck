package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	defs := workflowexec.StepDefinitions()
	pages := make([]schemadoc.PageInput, 0, len(defs))
	for _, def := range defs {
		path := filepath.Join(dir, def.SchemaFile)
		doc, err := readSchema(path)
		if err != nil {
			return nil, err
		}
		raw, err := readSchemaMap(path)
		if err != nil {
			return nil, err
		}
		kind, actions, err := validateToolSchemaDoc(def, doc)
		if err != nil {
			return nil, err
		}
		spec, _ := doc.Properties["spec"].(map[string]any)
		meta := schemadoc.ToolMeta(kind)
		meta.Category = def.Category
		page := schemadoc.PageInput{
			Kind:        kind,
			PageSlug:    strings.TrimSuffix(def.SchemaFile, ".schema.json"),
			Title:       doc.Title,
			Description: doc.Description,
			Visibility:  def.Visibility,
			SchemaPath:  filepath.ToSlash(filepath.Join("schemas", "tools", def.SchemaFile)),
			Schema:      raw,
			Meta:        meta,
			Actions:     actions,
			Required:    nestedRequired(doc.Properties, "spec"),
			Spec:        spec,
		}
		pages = append(pages, page)
	}
	return pages, nil
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
		kind, actions, err := validateToolSchemaDoc(def, doc)
		if err != nil {
			return nil, err
		}
		specProps := nestedProperties(doc.Properties, "spec")
		tool := toolSchemaDoc{
			File:        def.SchemaFile,
			Kind:        kind,
			Title:       doc.Title,
			Description: doc.Description,
			Visibility:  def.Visibility,
			Actions:     actions,
			SpecFields:  sortedKeys(specProps),
			Required:    nestedRequired(doc.Properties, "spec"),
		}
		tools = append(tools, tool)
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

func validateToolSchemaDoc(def workflowexec.StepDefinition, doc schemaDoc) (string, []string, error) {
	kind := schemaConst(doc.Properties, "kind")
	if kind != def.Kind {
		return "", nil, fmt.Errorf("tool schema %s kind mismatch: expected %s, got %s", def.SchemaFile, def.Kind, kind)
	}
	if visibility := firstNonEmpty(doc.Visibility, "public"); visibility != def.Visibility {
		return "", nil, fmt.Errorf("tool schema %s visibility mismatch: expected %s, got %s", def.SchemaFile, def.Visibility, visibility)
	}
	actions := make([]string, 0, len(def.Actions))
	for _, action := range def.Actions {
		actions = append(actions, action.Name)
	}
	return kind, actions, nil
}
