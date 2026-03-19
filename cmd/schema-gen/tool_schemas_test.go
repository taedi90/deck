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

func TestActionScopedDefinitionsDeclareFieldOwnership(t *testing.T) {
	for _, def := range workflowexec.StepDefinitions() {
		if len(def.Actions) == 0 {
			continue
		}
		for _, action := range def.Actions {
			if len(action.Fields) == 0 {
				t.Fatalf("missing field ownership for %s.%s", def.Kind, action.Name)
			}
		}
	}
}
