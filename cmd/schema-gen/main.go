package main

import (
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
	Actions     []string
	SpecFields  []string
	Required    []string
}

func main() {
	root, err := repoRoot()
	if err != nil {
		fail(err)
	}
	workflowSchemaPath := filepath.Join(root, "schemas", "deck-workflow.schema.json")
	writeJSONFile(workflowSchemaPath, generateWorkflowSchema())
	componentFragmentSchemaPath := filepath.Join(root, "schemas", "deck-component-fragment.schema.json")
	writeJSONFile(componentFragmentSchemaPath, generateComponentFragmentSchema())
	writeToolSchemas(root)

	toolDefinitionSchemaPath := filepath.Join(root, "schemas", "deck-tooldefinition.schema.json")
	writeJSONFile(toolDefinitionSchemaPath, generateToolDefinitionSchema())

	workflowSchemaMap, err := readSchemaMap(workflowSchemaPath)
	if err != nil {
		fail(err)
	}
	toolDefinitionSchemaMap, err := readSchemaMap(toolDefinitionSchemaPath)
	if err != nil {
		fail(err)
	}
	tools, err := loadToolSchemas(filepath.Join(root, "schemas", "tools"))
	if err != nil {
		fail(err)
	}
	toolPages, err := loadToolPageInputs(filepath.Join(root, "schemas", "tools"))
	if err != nil {
		fail(err)
	}

	writeGeneratedSchemaDocs(root, workflowSchemaMap, toolDefinitionSchemaMap, toolPages)
	writeFile(filepath.Join(root, "schemas", "README.md"), renderSchemasReadme(tools))
	writeFile(filepath.Join(root, "schemas", "tools", "README.md"), renderToolSchemasReadme(tools))
}

func writeGeneratedSchemaDocs(root string, workflowSchema, toolDefinitionSchema map[string]any, toolPages []schemadoc.PageInput) {
	writeFile(filepath.Join(root, "docs", "reference", "schema", "index.md"), schemadoc.RenderSchemaIndex("schemas/deck-workflow.schema.json", "schemas/deck-tooldefinition.schema.json", toolPages))
	writeFile(filepath.Join(root, "docs", "reference", "schema", "workflow.md"), schemadoc.RenderWorkflowPage("schemas/deck-workflow.schema.json", workflowSchema, schemadoc.WorkflowMeta()))
	writeFile(filepath.Join(root, "docs", "reference", "schema", "tool-definition.md"), schemadoc.RenderToolDefinitionPage("schemas/deck-tooldefinition.schema.json", toolDefinitionSchema, schemadoc.ToolDefinitionMeta()))
	writeFile(filepath.Join(root, "docs", "reference", "schema", "tools", "index.md"), schemadoc.RenderToolsIndex(toolPages))
	writeFile(filepath.Join(root, "docs", "reference", "schema", "examples", "index.md"), schemadoc.RenderExamplesIndex(toolPages))
	for _, page := range toolPages {
		name := strings.TrimSuffix(filepath.Base(page.SchemaPath), ".schema.json") + ".md"
		writeFile(filepath.Join(root, "docs", "reference", "schema", "tools", name), schemadoc.RenderToolPage(page))
		writeFile(filepath.Join(root, "docs", "reference", "schema", "examples", name), schemadoc.RenderToolExamplePage(page))
	}
}

func writeToolSchemas(root string) {
	for name, schema := range toolSchemaDefinitions() {
		writeJSONFile(filepath.Join(root, "schemas", "tools", name), schema)
	}
}
