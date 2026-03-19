package schemas

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/taedi90/deck/internal/workflowcontract"
	"github.com/taedi90/deck/internal/workflowexec"
)

func TestWorkflowSchemaAvailable(t *testing.T) {
	raw, err := WorkflowSchema()
	if err != nil {
		t.Fatalf("WorkflowSchema: %v", err)
	}
	if len(raw) == 0 {
		t.Fatalf("expected workflow schema content")
	}
}

func TestGeneratedToolPagesExist(t *testing.T) {
	seenPages := map[string]bool{}
	for _, def := range workflowcontract.StepDefinitions() {
		if def.Visibility != "public" {
			continue
		}
		if seenPages[def.DocsPage] {
			continue
		}
		seenPages[def.DocsPage] = true
		page := filepath.Join("..", "docs", "reference", "schema", "tools", def.DocsPage+".md")
		if _, err := os.Stat(page); err != nil {
			t.Fatalf("tool page missing for %s: %v", def.Kind, err)
		}
	}
}

func TestToolSchemasCoverStepContracts(t *testing.T) {
	for _, kind := range workflowexec.StepKinds() {
		file, ok := workflowexec.StepSchemaFile(kind)
		if !ok {
			t.Fatalf("missing schema file for kind %s", kind)
		}
		raw, err := ToolSchema(file)
		if err != nil {
			t.Fatalf("ToolSchema(%q): %v", file, err)
		}
		if len(raw) == 0 {
			t.Fatalf("expected schema content for kind %s", kind)
		}
	}
}

func TestWorkflowSchemaCoversStepKinds(t *testing.T) {
	raw, err := WorkflowSchema()
	if err != nil {
		t.Fatalf("WorkflowSchema: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal workflow schema: %v", err)
	}
	properties, _ := doc["properties"].(map[string]any)
	steps, _ := properties["steps"].(map[string]any)
	items, _ := steps["items"].(map[string]any)
	itemProps, _ := items["properties"].(map[string]any)
	kindField, _ := itemProps["kind"].(map[string]any)
	enum, _ := kindField["enum"].([]any)
	seen := map[string]bool{}
	for _, rawValue := range enum {
		value, _ := rawValue.(string)
		seen[value] = true
	}
	for _, kind := range workflowexec.StepKinds() {
		if !seen[kind] {
			t.Fatalf("workflow schema kind enum missing %s", kind)
		}
	}
}
