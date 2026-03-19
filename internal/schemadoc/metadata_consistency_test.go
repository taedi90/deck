package schemadoc

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/taedi90/deck/internal/workflowcontract"
	"github.com/taedi90/deck/internal/workflowexec"
	"github.com/taedi90/deck/schemas"
)

func TestToolMetadataCoversStepKinds(t *testing.T) {
	known := map[string]bool{}
	for _, kind := range workflowexec.StepKinds() {
		known[kind] = true
		if _, ok := toolMetadata[kind]; !ok {
			t.Fatalf("missing tool metadata for kind %s", kind)
		}
	}
	for kind := range toolMetadata {
		if !known[kind] {
			t.Fatalf("unexpected tool metadata for unknown kind %s", kind)
		}
	}
}

func TestSharedRegisterExamplesUseGenericOutputs(t *testing.T) {
	for name, example := range map[string]string{
		"common register":   commonFieldDocs["register"].Example,
		"workflow register": WorkflowMeta().FieldDocs["steps[].register"].Example,
	} {
		if strings.Contains(example, "joinCommand") || strings.Contains(example, "joinCmd") {
			t.Fatalf("%s example should not reference kubeadm-specific outputs: %q", name, example)
		}
	}
}

func TestSharedContractTextUsesWorkflowContract(t *testing.T) {
	if commonFieldDocs["when"].Description != workflowcontract.WhenDescription() {
		t.Fatalf("unexpected common when description: %q", commonFieldDocs["when"].Description)
	}
	if commonFieldDocs["when"].Example != workflowcontract.WhenExample() {
		t.Fatalf("unexpected common when example: %q", commonFieldDocs["when"].Example)
	}
	if commonFieldDocs["register"].Description != workflowcontract.RegisterDescription() {
		t.Fatalf("unexpected common register description: %q", commonFieldDocs["register"].Description)
	}
	if commonFieldDocs["register"].Example != workflowcontract.RegisterExample() {
		t.Fatalf("unexpected common register example: %q", commonFieldDocs["register"].Example)
	}

	workflowMeta := WorkflowMeta()
	if workflowMeta.FieldDocs["steps[].when"].Description != workflowcontract.WhenDescription() {
		t.Fatalf("unexpected workflow when description: %q", workflowMeta.FieldDocs["steps[].when"].Description)
	}
	if workflowMeta.FieldDocs["steps[].register"].Description != workflowcontract.RegisterDescription() {
		t.Fatalf("unexpected workflow register description: %q", workflowMeta.FieldDocs["steps[].register"].Description)
	}
}

func TestRemovedFieldsStayOutOfPublicMetadata(t *testing.T) {
	checks := []struct {
		kind  string
		field string
	}{
		{kind: "File", field: "spec.owner"},
		{kind: "File", field: "spec.group"},
		{kind: "Wait", field: "spec.state"},
	}
	for _, tc := range checks {
		meta, ok := toolMetadata[tc.kind]
		if !ok {
			t.Fatalf("missing tool metadata for kind %s", tc.kind)
		}
		if _, exists := meta.FieldDocs[tc.field]; exists {
			t.Fatalf("field %s should not appear in %s metadata", tc.field, tc.kind)
		}
	}
}

func TestActionMetadataCoversActionContracts(t *testing.T) {
	for _, kind := range workflowexec.StepKinds() {
		contract, ok := workflowexec.StepContractForKind(kind)
		if !ok || len(contract.Actions) == 0 {
			continue
		}
		meta := toolMetadata[kind]
		knownActions := map[string]bool{}
		for action := range contract.Actions {
			knownActions[action] = true
			if _, ok := meta.ActionNotes[action]; !ok {
				t.Fatalf("missing action note for %s.%s", kind, action)
			}
			if _, ok := meta.ActionExamples[action]; !ok {
				t.Fatalf("missing action example for %s.%s", kind, action)
			}
		}
		for action := range meta.ActionNotes {
			if !knownActions[action] {
				t.Fatalf("unexpected action note for %s.%s", kind, action)
			}
		}
		for action := range meta.ActionExamples {
			if !knownActions[action] {
				t.Fatalf("unexpected action example for %s.%s", kind, action)
			}
		}
	}
}

func TestToolMetadataCategoryMatchesRegistry(t *testing.T) {
	for _, def := range workflowexec.StepDefinitions() {
		meta := ToolMeta(def.Kind)
		if meta.Category != def.Category {
			t.Fatalf("category mismatch for %s: metadata=%q registry=%q", def.Kind, meta.Category, def.Category)
		}
		if meta.Summary != def.Summary {
			t.Fatalf("summary mismatch for %s: metadata=%q registry=%q", def.Kind, meta.Summary, def.Summary)
		}
		if meta.WhenToUse != def.WhenToUse {
			t.Fatalf("whenToUse mismatch for %s: metadata=%q registry=%q", def.Kind, meta.WhenToUse, def.WhenToUse)
		}
	}
}

func TestToolMetadataDoesNotDuplicateStructuredIdentityFacts(t *testing.T) {
	for kind, meta := range toolMetadata {
		if meta.Kind != "" {
			t.Fatalf("tool metadata should not set Kind for %s", kind)
		}
		if meta.Category != "" {
			t.Fatalf("tool metadata should not set Category for %s", kind)
		}
		if meta.Summary != "" {
			t.Fatalf("tool metadata should not set Summary for %s", kind)
		}
		if meta.WhenToUse != "" {
			t.Fatalf("tool metadata should not set WhenToUse for %s", kind)
		}
	}
}

func TestToolMetadataFieldDocsExistInSchemas(t *testing.T) {
	for _, def := range workflowexec.StepDefinitions() {
		meta, ok := toolMetadata[def.Kind]
		if !ok {
			t.Fatalf("missing tool metadata for kind %s", def.Kind)
		}
		raw, err := schemas.ToolSchema(def.SchemaFile)
		if err != nil {
			t.Fatalf("ToolSchema(%q): %v", def.SchemaFile, err)
		}
		var schema map[string]any
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatalf("unmarshal schema %q: %v", def.SchemaFile, err)
		}
		fieldSet := map[string]bool{}
		for _, field := range CollectFields(schema) {
			fieldSet[field.Path] = true
		}
		for path := range meta.FieldDocs {
			if !fieldSet[path] {
				t.Fatalf("field doc %s missing from schema for %s", path, def.Kind)
			}
		}
	}
}
