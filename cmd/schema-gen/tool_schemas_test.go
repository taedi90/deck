package main

import (
	"testing"

	"github.com/Airgap-Castaways/deck/internal/workflowexec"
)

func TestToolSchemaGeneratorsCoverStepDefinitions(t *testing.T) {
	for _, def := range workflowexec.BuiltInTypeDefinitionsWith(nil, schemaMetadataForDefinition) {
		generatorName := def.Schema.GeneratorName
		if generatorName == "" {
			generatorName = def.Step.Kind
		}
		if generatorName == "" {
			t.Fatalf("missing generator name for %s", def.Step.Kind)
		}
		if def.Schema.SpecType == nil {
			t.Fatalf("missing schema spec type for %s", def.Step.Kind)
		}
		if def.Schema.Patch == nil {
			t.Fatalf("missing schema patch hook for %s", def.Step.Kind)
		}
	}
}

func TestToolSchemaDefinitionsUseRegistrySchemaFiles(t *testing.T) {
	defs, err := toolSchemaDefinitions()
	if err != nil {
		t.Fatalf("toolSchemaDefinitions: %v", err)
	}
	registryDefs := workflowexec.BuiltInTypeDefinitionsWith(nil, schemaMetadataForDefinition)
	for _, def := range registryDefs {
		if _, ok := defs[def.Step.SchemaFile]; !ok {
			t.Fatalf("generated schemas missing %s for %s", def.Step.SchemaFile, def.Step.Kind)
		}
	}
	if len(defs) != len(registryDefs) {
		t.Fatalf("expected %d generated tool schemas, got %d", len(registryDefs), len(defs))
	}
}

func TestGeneratedToolSchemasTreatAPIVersionAsOptional(t *testing.T) {
	defs, err := toolSchemaDefinitions()
	if err != nil {
		t.Fatalf("toolSchemaDefinitions: %v", err)
	}
	for _, def := range workflowexec.BuiltInTypeDefinitionsWith(nil, schemaMetadataForDefinition) {
		schema, ok := defs[def.Step.SchemaFile]
		if !ok {
			t.Fatalf("missing schema for %s", def.Step.Kind)
		}
		required := toStringSlice(schema["required"])
		if contains(required, "apiVersion") {
			t.Fatalf("tool schema %s should not require apiVersion: %v", def.Step.SchemaFile, required)
		}
		for _, field := range []string{"id", "kind", "spec"} {
			if !contains(required, field) {
				t.Fatalf("tool schema %s missing required field %s: %v", def.Step.SchemaFile, field, required)
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
