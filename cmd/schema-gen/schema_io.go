package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/taedi90/deck/internal/schemadoc"
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
	raw, err := os.ReadFile(path)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("parse schema %s: %w", path, err)
	}
	return out, nil
}

func readSchemaMap(path string) (map[string]any, error) {
	raw, err := os.ReadFile(path)
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
	pages := make([]schemadoc.PageInput, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".schema.json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		doc, err := readSchema(path)
		if err != nil {
			return nil, err
		}
		raw, err := readSchemaMap(path)
		if err != nil {
			return nil, err
		}
		kind := schemaConst(doc.Properties, "kind")
		spec, _ := doc.Properties["spec"].(map[string]any)
		page := schemadoc.PageInput{
			Kind:        kind,
			PageSlug:    strings.TrimSuffix(entry.Name(), ".schema.json"),
			Title:       doc.Title,
			Description: doc.Description,
			Visibility:  firstNonEmpty(doc.Visibility, "public"),
			SchemaPath:  filepath.ToSlash(filepath.Join("schemas", "tools", entry.Name())),
			Schema:      raw,
			Meta:        schemadoc.ToolMeta(kind),
			Actions:     nestedEnum(doc.Properties, "spec", "action"),
			Required:    nestedRequired(doc.Properties, "spec"),
			Spec:        spec,
		}
		pages = append(pages, page)
	}
	sort.Slice(pages, func(i, j int) bool { return pages[i].Kind < pages[j].Kind })
	return pages, nil
}

func loadToolSchemas(dir string) ([]toolSchemaDoc, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	tools := make([]toolSchemaDoc, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".schema.json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		var doc schemaDoc
		if err := json.Unmarshal(raw, &doc); err != nil {
			return nil, fmt.Errorf("parse tool schema %s: %w", entry.Name(), err)
		}
		kind := schemaConst(doc.Properties, "kind")
		specProps := nestedProperties(doc.Properties, "spec")
		tool := toolSchemaDoc{
			File:        entry.Name(),
			Kind:        kind,
			Title:       doc.Title,
			Description: doc.Description,
			Visibility:  firstNonEmpty(doc.Visibility, "public"),
			Actions:     nestedEnum(doc.Properties, "spec", "action"),
			SpecFields:  sortedKeys(specProps),
			Required:    nestedRequired(doc.Properties, "spec"),
		}
		tools = append(tools, tool)
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Kind < tools[j].Kind })
	return tools, nil
}
