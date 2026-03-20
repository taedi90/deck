package schemadoc

import (
	"strings"
	"testing"

	"github.com/taedi90/deck/internal/workflowcontract"
	"github.com/taedi90/deck/internal/workflowexec"
)

func TestToolMetadataCoversStepKinds(t *testing.T) {
	for _, kind := range workflowexec.StepKinds() {
		meta := ToolMeta(kind)
		if meta.Kind != kind {
			t.Fatalf("unexpected normalized kind for %s: %q", kind, meta.Kind)
		}
		if strings.TrimSpace(meta.Summary) == "" {
			t.Fatalf("missing tool metadata summary for kind %s", kind)
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
		{kind: "DownloadFile", field: "spec.owner"},
		{kind: "DownloadFile", field: "spec.group"},
		{kind: "WaitForService", field: "spec.state"},
	}
	for _, tc := range checks {
		meta := ToolMeta(tc.kind)
		if _, exists := meta.FieldDocs[tc.field]; exists {
			t.Fatalf("field %s should not appear in %s metadata", tc.field, tc.kind)
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

func TestToolMetadataRemovesLegacyActionFieldDocs(t *testing.T) {
	for _, def := range workflowexec.StepDefinitions() {
		meta := ToolMeta(def.Kind)
		if _, ok := meta.FieldDocs["spec.action"]; ok {
			t.Fatalf("legacy spec.action field doc should not be exposed for %s", def.Kind)
		}
	}
}
