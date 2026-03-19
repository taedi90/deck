package main

import (
	"testing"

	"github.com/taedi90/deck/internal/workflowexec"
)

func TestToolSchemaGeneratorsCoverStepDefinitions(t *testing.T) {
	generators := toolSchemaGenerators()
	for _, def := range workflowexec.StepDefinitions() {
		generatorName := def.ToolSchemaGenerator
		if generatorName == "" {
			generatorName = def.Kind
		}
		if _, ok := generators[generatorName]; !ok {
			t.Fatalf("missing generator %q for %s", generatorName, def.Kind)
		}
	}
	usedGenerators := map[string]bool{}
	for _, def := range workflowexec.StepDefinitions() {
		generatorName := def.ToolSchemaGenerator
		if generatorName == "" {
			generatorName = def.Kind
		}
		usedGenerators[generatorName] = true
	}
	for name := range generators {
		if !usedGenerators[name] {
			t.Fatalf("generator registered but unused: %s", name)
		}
	}
}

func TestToolSchemaDefinitionsUseRegistrySchemaFiles(t *testing.T) {
	defs, err := toolSchemaDefinitions()
	if err != nil {
		t.Fatalf("toolSchemaDefinitions: %v", err)
	}
	for _, def := range workflowexec.StepDefinitions() {
		if _, ok := defs[def.SchemaFile]; !ok {
			t.Fatalf("generated schemas missing %s for %s", def.SchemaFile, def.Kind)
		}
	}
	if len(defs) != len(workflowexec.StepDefinitions()) {
		t.Fatalf("expected %d generated tool schemas, got %d", len(workflowexec.StepDefinitions()), len(defs))
	}
}

func TestGeneratedToolSchemasTreatAPIVersionAsOptional(t *testing.T) {
	defs, err := toolSchemaDefinitions()
	if err != nil {
		t.Fatalf("toolSchemaDefinitions: %v", err)
	}
	for _, def := range workflowexec.StepDefinitions() {
		schema, ok := defs[def.SchemaFile]
		if !ok {
			t.Fatalf("missing schema for %s", def.Kind)
		}
		required := toStringSlice(schema["required"])
		if contains(required, "apiVersion") {
			t.Fatalf("tool schema %s should not require apiVersion: %v", def.SchemaFile, required)
		}
		for _, field := range []string{"id", "kind", "spec"} {
			if !contains(required, field) {
				t.Fatalf("tool schema %s missing required field %s: %v", def.SchemaFile, field, required)
			}
		}
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
