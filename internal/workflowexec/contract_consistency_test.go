package workflowexec

import (
	"testing"

	"github.com/taedi90/deck/internal/workflowcontract"
)

func TestStepRegistryDelegatesToWorkflowContract(t *testing.T) {
	wantDefs := workflowcontract.StepDefinitions()
	gotDefs := StepDefinitions()
	if len(gotDefs) != len(wantDefs) {
		t.Fatalf("unexpected step definition count: got %d want %d", len(gotDefs), len(wantDefs))
	}
	for _, want := range wantDefs {
		got, ok := StepDefinitionForKind(want.Kind)
		if !ok {
			t.Fatalf("missing step definition for kind %s", want.Kind)
		}
		if got.SchemaFile != want.SchemaFile {
			t.Fatalf("schema file mismatch for %s: got %q want %q", want.Kind, got.SchemaFile, want.SchemaFile)
		}
		if got.Category != want.Category {
			t.Fatalf("category mismatch for %s: got %q want %q", want.Kind, got.Category, want.Category)
		}
		if file, ok := StepSchemaFile(want.Kind); !ok || file != want.SchemaFile {
			t.Fatalf("StepSchemaFile mismatch for %s: got %q ok=%t want %q", want.Kind, file, ok, want.SchemaFile)
		}
	}
}

func TestRegisterableOutputsCoveredByContracts(t *testing.T) {
	for _, def := range StepDefinitions() {
		contract, ok := StepContractForKind(def.Kind)
		if !ok {
			t.Fatalf("missing step contract for %s", def.Kind)
		}
		for _, output := range def.Outputs {
			if !contract.Outputs[output] {
				t.Fatalf("missing top-level output %q for %s", output, def.Kind)
			}
		}
	}
}
