package workflowexec

import (
	"testing"

	"github.com/taedi90/deck/internal/workflowcontract"
)

func TestStepRegistryDelegatesToWorkflowContract(t *testing.T) {
	wantDefs := workflowcontract.StepDefinitions()
	gotDefs := StepDefinitions()
	builtIns := BuiltInTypeDefinitions()
	if len(gotDefs) != len(wantDefs) {
		t.Fatalf("unexpected step definition count: got %d want %d", len(gotDefs), len(wantDefs))
	}
	if len(builtIns) != len(wantDefs) {
		t.Fatalf("unexpected built-in type definition count: got %d want %d", len(builtIns), len(wantDefs))
	}
	for _, want := range wantDefs {
		got, ok := StepDefinitionForKey(StepTypeKey{APIVersion: want.APIVersion, Kind: want.Kind})
		if !ok {
			t.Fatalf("missing step definition for key %s/%s", want.APIVersion, want.Kind)
		}
		if got.SchemaFile != want.SchemaFile {
			t.Fatalf("schema file mismatch for %s: got %q want %q", want.Kind, got.SchemaFile, want.SchemaFile)
		}
		if got.Category != want.Category {
			t.Fatalf("category mismatch for %s: got %q want %q", want.Kind, got.Category, want.Category)
		}
		if file, ok := StepSchemaFileForKey(StepTypeKey{APIVersion: want.APIVersion, Kind: want.Kind}); !ok || file != want.SchemaFile {
			t.Fatalf("StepSchemaFileForKey mismatch for %s: got %q ok=%t want %q", want.Kind, file, ok, want.SchemaFile)
		}
		builtIn, ok := BuiltInTypeDefinitionForKey(StepTypeKey{APIVersion: want.APIVersion, Kind: want.Kind})
		if !ok {
			t.Fatalf("missing built-in type definition for %s", want.Kind)
		}
		if builtIn.Step.SchemaFile != want.SchemaFile {
			t.Fatalf("built-in type definition schema mismatch for %s: got %q want %q", want.Kind, builtIn.Step.SchemaFile, want.SchemaFile)
		}
		if builtIn.Docs.Kind != want.Kind {
			t.Fatalf("built-in docs kind mismatch for %s: got %q", want.Kind, builtIn.Docs.Kind)
		}
		if builtIn.Docs.Category != want.Category {
			t.Fatalf("built-in docs category mismatch for %s: got %q want %q", want.Kind, builtIn.Docs.Category, want.Category)
		}
		if builtIn.Schema.GeneratorName == "" {
			t.Fatalf("missing schema generator name for %s", want.Kind)
		}
	}
}

func TestRegisterableOutputsCoveredByContracts(t *testing.T) {
	for _, def := range StepDefinitions() {
		builtIn, ok := BuiltInTypeDefinitionForKey(StepTypeKey{APIVersion: def.APIVersion, Kind: def.Kind})
		if !ok {
			t.Fatalf("missing built-in type definition for %s", def.Kind)
		}
		for _, output := range def.Outputs {
			if !containsString(builtIn.Step.Outputs, output) {
				t.Fatalf("missing keyed top-level output %q for %s", output, def.Kind)
			}
		}
	}
}
