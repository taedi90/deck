package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/taedi90/deck/internal/schemadoc"
)

type schemaDoc struct {
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Visibility  string         `json:"x-deck-visibility"`
	Required    []string       `json:"required"`
	Properties  map[string]any `json:"properties"`
}

type toolSchemaDoc struct {
	File        string
	Kind        string
	Title       string
	Description string
	Visibility  string
	SpecFields  []string
	Required    []string
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	root, err := repoRoot()
	if err != nil {
		return err
	}
	workflowSchemaPath := filepath.Join(root, "schemas", "deck-workflow.schema.json")
	workflowSchema, err := generateWorkflowSchema()
	if err != nil {
		return err
	}
	if err := writeJSONFile(workflowSchemaPath, workflowSchema); err != nil {
		return err
	}
	componentFragmentSchemaPath := filepath.Join(root, "schemas", "deck-component-fragment.schema.json")
	if err := writeJSONFile(componentFragmentSchemaPath, generateComponentFragmentSchema()); err != nil {
		return err
	}
	if err := writeToolSchemas(root); err != nil {
		return err
	}

	toolDefinitionSchemaPath := filepath.Join(root, "schemas", "deck-tooldefinition.schema.json")
	toolDefinitionSchema, err := generateToolDefinitionSchema()
	if err != nil {
		return err
	}
	if err := writeJSONFile(toolDefinitionSchemaPath, toolDefinitionSchema); err != nil {
		return err
	}

	workflowSchemaMap, err := readSchemaMap(workflowSchemaPath)
	if err != nil {
		return err
	}
	componentFragmentSchemaMap, err := readSchemaMap(componentFragmentSchemaPath)
	if err != nil {
		return err
	}
	toolDefinitionSchemaMap, err := readSchemaMap(toolDefinitionSchemaPath)
	if err != nil {
		return err
	}
	tools, err := loadToolSchemas(filepath.Join(root, "schemas", "tools"))
	if err != nil {
		return err
	}
	toolPages, err := loadToolPageInputs(filepath.Join(root, "schemas", "tools"))
	if err != nil {
		return err
	}

	if err := writeGeneratedSchemaDocs(root, workflowSchemaMap, componentFragmentSchemaMap, toolDefinitionSchemaMap, toolPages); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(root, "schemas", "README.md"), renderSchemasReadme(tools)); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(root, "schemas", "tools", "README.md"), renderToolSchemasReadme(tools)); err != nil {
		return err
	}
	return nil
}

func writeGeneratedSchemaDocs(root string, workflowSchema, componentFragmentSchema, toolDefinitionSchema map[string]any, toolPages []schemadoc.PageInput) error {
	if err := removeGeneratedToolDocs(filepath.Join(root, "docs", "reference", "schema", "tools"), toolPages); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(root, "docs", "reference", "schema", "README.md"), schemadoc.RenderSchemaIndex("schemas/deck-workflow.schema.json", "schemas/deck-tooldefinition.schema.json", toolPages)); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(root, "docs", "reference", "schema", "workflow.md"), schemadoc.RenderWorkflowPage("schemas/deck-workflow.schema.json", workflowSchema, schemadoc.WorkflowMeta())); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(root, "docs", "reference", "schema", "component-fragment.md"), schemadoc.RenderComponentFragmentPage("schemas/deck-component-fragment.schema.json", componentFragmentSchema, schemadoc.ComponentFragmentMeta())); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(root, "docs", "reference", "schema", "tool-definition.md"), schemadoc.RenderToolDefinitionPage("schemas/deck-tooldefinition.schema.json", toolDefinitionSchema, schemadoc.ToolDefinitionMeta())); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(root, "docs", "reference", "schema", "tools", "README.md"), schemadoc.RenderToolsIndex(toolPages)); err != nil {
		return err
	}
	for _, page := range toolPages {
		if err := writeFile(filepath.Join(root, "docs", "reference", "schema", "tools", page.PageSlug+".md"), schemadoc.RenderToolPage(page)); err != nil {
			return err
		}
	}
	return nil
}

func removeGeneratedToolDocs(dir string, toolPages []schemadoc.PageInput) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	keep := map[string]bool{"README.md": true}
	for _, page := range toolPages {
		keep[page.PageSlug+".md"] = true
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" || keep[entry.Name()] {
			continue
		}
		if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func writeToolSchemas(root string) error {
	definitions, err := toolSchemaDefinitions()
	if err != nil {
		return err
	}
	if err := removeStaleGeneratedSchemas(filepath.Join(root, "schemas", "tools"), definitions); err != nil {
		return err
	}
	for name, schema := range definitions {
		if err := writeJSONFile(filepath.Join(root, "schemas", "tools", name), schema); err != nil {
			return err
		}
	}
	return nil
}

func removeStaleGeneratedSchemas(dir string, definitions map[string]map[string]any) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".schema.json") {
			continue
		}
		if _, ok := definitions[entry.Name()]; ok {
			continue
		}
		if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}
