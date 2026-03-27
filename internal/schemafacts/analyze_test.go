package schemafacts

import (
	"encoding/json"
	"testing"

	"github.com/Airgap-Castaways/deck/schemas"
)

func TestAnalyzeWriteFileCapturesRequiredAndExactlyOneRules(t *testing.T) {
	schema := loadToolSchema(t, "file.write.schema.json")
	facts := Analyze(schema)
	fields := map[string]FieldFact{}
	for _, field := range facts.Fields {
		fields[field.Path] = field
	}
	if got := fields["spec.path"].Requirement; got != RequirementRequired {
		t.Fatalf("expected spec.path required, got %q", got)
	}
	if got := fields["spec.content"].Requirement; got != RequirementConditional {
		t.Fatalf("expected spec.content conditional, got %q", got)
	}
	if got := fields["spec.template"].Requirement; got != RequirementConditional {
		t.Fatalf("expected spec.template conditional, got %q", got)
	}
	rules := specRules(t, schema)
	found := false
	for _, rule := range rules {
		if rule == "Exactly one of `spec.content` or `spec.template` must be set." {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected exact-one rule in %#v", rules)
	}
}

func TestAnalyzeDownloadFileCapturesConditionalSourceBranches(t *testing.T) {
	schema := loadToolSchema(t, "file.download.schema.json")
	facts := Analyze(schema)
	fields := map[string]FieldFact{}
	for _, field := range facts.Fields {
		fields[field.Path] = field
	}
	for _, path := range []string{"spec.source", "spec.items", "spec.source.url", "spec.source.path", "spec.source.bundle", "spec.source.bundle.root", "spec.source.bundle.path"} {
		if fields[path].Requirement != RequirementConditional {
			t.Fatalf("expected %s conditional, got %#v", path, fields[path])
		}
	}
	if fields["spec.outputPath"].Requirement != RequirementOptional {
		t.Fatalf("expected outputPath optional, got %#v", fields["spec.outputPath"])
	}
}

func loadToolSchema(t *testing.T, file string) map[string]any {
	t.Helper()
	raw, err := schemas.ToolSchema(file)
	if err != nil {
		t.Fatalf("ToolSchema(%q): %v", file, err)
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("json.Unmarshal(%q): %v", file, err)
	}
	return schema
}

func specRules(t *testing.T, schema map[string]any) []string {
	t.Helper()
	props, _ := schema["properties"].(map[string]any)
	spec, _ := props["spec"].(map[string]any)
	if len(spec) == 0 {
		t.Fatal("expected spec schema")
	}
	return ExtractRules(spec, "spec")
}
