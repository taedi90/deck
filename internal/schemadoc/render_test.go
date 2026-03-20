package schemadoc

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/taedi90/deck/internal/workflowcontract"
	"github.com/taedi90/deck/schemas"
)

func TestRenderToolPageGroupsConcreteKindsByFamily(t *testing.T) {
	page := testFamilyPageInput(t, "file")
	rendered := string(RenderToolPage(page))

	if !strings.Contains(rendered, "## Supported Kinds") {
		t.Fatalf("expected supported kinds section:\n%s", rendered)
	}
	for _, kind := range []string{"DownloadFile", "WriteFile", "CopyFile", "EditFile"} {
		if !strings.Contains(rendered, "`"+kind+"`") {
			t.Fatalf("expected grouped file kind %s:\n%s", kind, rendered)
		}
	}
	if strings.Contains(rendered, "spec.action") {
		t.Fatalf("did not expect legacy spec.action docs:\n%s", rendered)
	}
	if !strings.Contains(rendered, "../../../schemas/tools/file.download.schema.json") {
		t.Fatalf("expected raw schema link for grouped variant:\n%s", rendered)
	}
}

func TestRenderToolPageConcreteKindSectionUsesNormalizedExample(t *testing.T) {
	page := testFamilyPageInput(t, "package")
	rendered := string(RenderToolPage(page))

	if !strings.Contains(rendered, "## `DownloadPackage`") || !strings.Contains(rendered, "## `InstallPackage`") {
		t.Fatalf("expected concrete kind sections:\n%s", rendered)
	}
	if !strings.Contains(rendered, "kind: InstallPackage") {
		t.Fatalf("expected normalized concrete kind example:\n%s", rendered)
	}
	if strings.Contains(rendered, "action:") {
		t.Fatalf("did not expect legacy action selector in rendered page:\n%s", rendered)
	}
	if !strings.Contains(rendered, "outputs: `artifacts`") {
		t.Fatalf("expected outputs section for DownloadPackage:\n%s", rendered)
	}
}

func TestRenderWorkflowPageUsesConcreteKindExample(t *testing.T) {
	raw, err := schemas.WorkflowSchema()
	if err != nil {
		t.Fatalf("WorkflowSchema: %v", err)
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("unmarshal workflow schema: %v", err)
	}
	rendered := string(RenderWorkflowPage("schemas/deck-workflow.schema.json", schema, WorkflowMeta()))

	if !strings.Contains(rendered, "kind: WriteFile") {
		t.Fatalf("expected concrete kind workflow example:\n%s", rendered)
	}
	if strings.Contains(rendered, "action: write") {
		t.Fatalf("did not expect legacy action line in workflow example:\n%s", rendered)
	}
}

func testFamilyPageInput(t *testing.T, family string) PageInput {
	t.Helper()
	defs := workflowcontract.StepDefinitions()
	page := PageInput{Family: family}
	for _, def := range defs {
		if def.Family != family || def.Visibility != "public" {
			continue
		}
		raw, err := schemas.ToolSchema(def.SchemaFile)
		if err != nil {
			t.Fatalf("ToolSchema(%q): %v", def.SchemaFile, err)
		}
		var schema map[string]any
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatalf("unmarshal tool schema %q: %v", def.SchemaFile, err)
		}
		properties, _ := schema["properties"].(map[string]any)
		spec, _ := properties["spec"].(map[string]any)
		if page.PageSlug == "" {
			page.PageSlug = def.DocsPage
			page.Title = def.FamilyTitle
			page.Summary = "Reference for the `" + def.FamilyTitle + "` family of typed workflow steps."
		}
		page.Variants = append(page.Variants, VariantInput{
			Kind:        def.Kind,
			Title:       def.FamilyTitle,
			Description: def.Summary,
			SchemaPath:  filepath.ToSlash(filepath.Join("schemas", "tools", def.SchemaFile)),
			Schema:      schema,
			Meta:        ToolMeta(def.Kind),
			Required:    toRequiredStrings(spec["required"]),
			Spec:        spec,
			Outputs:     append([]string(nil), def.Outputs...),
			DocsOrder:   def.DocsOrder,
		})
	}
	if page.PageSlug == "" {
		t.Fatalf("missing test page for family %s", family)
	}
	return page
}

func toRequiredStrings(value any) []string {
	items, _ := value.([]any)
	out := make([]string, 0, len(items))
	for _, item := range items {
		text, _ := item.(string)
		if text != "" {
			out = append(out, text)
		}
	}
	return out
}
