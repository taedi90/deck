package askknowledge

import (
	"fmt"
	"sort"
	"strings"
)

func RenderReferenceDocs(bundle Bundle) map[string]string {
	return map[string]string{
		"docs/reference/ask-workflow-structure.md":     renderWorkflowStructureDoc(bundle),
		"docs/reference/ask-step-catalog.md":           renderStepCatalogDoc(bundle),
		"docs/reference/ask-component-shape.md":        renderComponentShapeDoc(bundle),
		"docs/reference/ask-vars-guidance.md":          renderVarsDoc(bundle),
		"docs/reference/ask-validation-diagnostics.md": renderDiagnosticsDoc(bundle),
		"docs/reference/ask-pipeline.md":               renderPipelineDoc(),
	}
}

func renderWorkflowStructureDoc(bundle Bundle) string {
	b := &strings.Builder{}
	b.WriteString("# Ask workflow structure\n\n")
	b.WriteString("Generated from deck ask source-of-truth metadata.\n\n")
	b.WriteString("## Invariants\n")
	_, _ = fmt.Fprintf(b, "- Supported roles: %s\n", strings.Join(bundle.Workflow.SupportedRoles, ", "))
	_, _ = fmt.Fprintf(b, "- Supported workflow version: `%s`\n", bundle.Workflow.SupportedVersion)
	_, _ = fmt.Fprintf(b, "- Top-level workflow modes: %s\n", strings.Join(bundle.Workflow.TopLevelModes, ", "))
	_, _ = fmt.Fprintf(b, "- Required top-level fields: %s\n", strings.Join(bundle.Workflow.RequiredFields, ", "))
	b.WriteString("\n## File topology\n")
	_, _ = fmt.Fprintf(b, "- Scenario entrypoints live under `%s`\n", bundle.Topology.ScenarioDir)
	_, _ = fmt.Fprintf(b, "- Reusable fragments live under `%s`\n", bundle.Topology.ComponentDir)
	_, _ = fmt.Fprintf(b, "- Shared variables live at `%s`\n", bundle.Topology.VarsPath)
	_, _ = fmt.Fprintf(b, "- Canonical prepare entrypoint: `%s`\n", bundle.Topology.CanonicalPrepare)
	_, _ = fmt.Fprintf(b, "- Canonical apply entrypoint: `%s`\n", bundle.Topology.CanonicalApply)
	b.WriteString("\n## Examples\n\n```yaml\n")
	b.WriteString(bundle.Workflow.PhaseExample)
	b.WriteString("\n```\n\n```yaml\n")
	b.WriteString(bundle.Workflow.StepsExample)
	b.WriteString("\n```\n")
	return b.String()
}

func renderStepCatalogDoc(bundle Bundle) string {
	b := &strings.Builder{}
	b.WriteString("# Ask step catalog\n\n")
	b.WriteString("Generated from workflow contracts and schema docs.\n")
	for _, step := range bundle.Steps {
		b.WriteString("\n## ")
		b.WriteString(step.Kind)
		b.WriteString("\n")
		b.WriteString("- Category: `")
		b.WriteString(step.Category)
		b.WriteString("`\n")
		b.WriteString("- Allowed roles: ")
		b.WriteString(strings.Join(step.AllowedRoles, ", "))
		b.WriteString("\n")
		b.WriteString("- Summary: ")
		b.WriteString(step.Summary)
		b.WriteString("\n")
		if step.WhenToUse != "" {
			b.WriteString("- When to use: ")
			b.WriteString(step.WhenToUse)
			b.WriteString("\n")
		}
		if step.SchemaFile != "" {
			b.WriteString("- Schema: `schemas/")
			b.WriteString(step.SchemaFile)
			b.WriteString("`\n")
		}
		if len(step.KeyFields) > 0 {
			b.WriteString("- Key fields:\n")
			for _, field := range step.KeyFields {
				b.WriteString("  - `")
				b.WriteString(field.Path)
				b.WriteString("`: ")
				b.WriteString(field.Description)
				b.WriteString("\n")
			}
		}
	}
	return b.String()
}

func renderComponentShapeDoc(bundle Bundle) string {
	b := &strings.Builder{}
	b.WriteString("# Ask component shape\n\n")
	b.WriteString("Generated from workflow topology and component fragment guidance.\n\n")
	b.WriteString("- ")
	b.WriteString(bundle.Components.ImportRule)
	b.WriteString("\n- ")
	b.WriteString(bundle.Components.FragmentRule)
	b.WriteString("\n- Allowed root keys: ")
	b.WriteString(strings.Join(bundle.Components.AllowedRootKeys, ", "))
	b.WriteString("\n\n## Import example\n\n```yaml\n")
	b.WriteString(bundle.Components.ImportExample)
	b.WriteString("\n```\n\n## Component example\n\n```yaml\n")
	b.WriteString(bundle.Components.FragmentExample)
	b.WriteString("\n```\n")
	return b.String()
}

func renderVarsDoc(bundle Bundle) string {
	b := &strings.Builder{}
	b.WriteString("# Ask vars guidance\n\n")
	b.WriteString("Generated from vars guidance metadata.\n\n")
	b.WriteString("- Path: `")
	b.WriteString(bundle.Vars.Path)
	b.WriteString("`\n")
	b.WriteString("- ")
	b.WriteString(bundle.Vars.Summary)
	b.WriteString("\n- Prefer vars for: ")
	b.WriteString(strings.Join(bundle.Vars.PreferFor, ", "))
	b.WriteString("\n- Avoid vars for: ")
	b.WriteString(strings.Join(bundle.Vars.AvoidFor, ", "))
	b.WriteString("\n- Example keys: ")
	b.WriteString(strings.Join(bundle.Vars.ExampleKeys, ", "))
	b.WriteString("\n")
	return b.String()
}

func renderDiagnosticsDoc(bundle Bundle) string {
	b := &strings.Builder{}
	b.WriteString("# Ask validation diagnostics\n\n")
	b.WriteString("Structured diagnostics classify repair work by code, file, path, expected value, actual value, and source reference.\n\n")
	b.WriteString("## Common categories\n")
	b.WriteString("- `schema_invalid`: schema or YAML shape mismatch\n")
	b.WriteString("- `component_fragment_shape`: component fragment was not a `steps:` object\n")
	b.WriteString("- `import_shape`: phase import item was not an object with `path`\n")
	b.WriteString("- `constrained_literal_template`: constrained field used a vars template instead of a literal\n")
	b.WriteString("- `role_support`: step kind used in an unsupported role\n")
	b.WriteString("\n## Constrained field references\n")
	constraints := append([]ConstraintKnowledge(nil), bundle.Constraints...)
	sort.Slice(constraints, func(i, j int) bool { return constraints[i].Path < constraints[j].Path })
	for _, item := range constraints {
		b.WriteString("- `")
		b.WriteString(item.StepKind)
		b.WriteString(" ` `")
		b.WriteString(item.Path)
		b.WriteString("`\n")
	}
	return b.String()
}

func renderPipelineDoc() string {
	return "# Ask pipeline\n\n- Request intake normalizes the prompt and workspace root.\n- Classification chooses interaction mode only.\n- Retrieval gathers workspace context, typed evidence, and deck knowledge slices.\n- Requirements derivation decides offline assumptions, required files, and acceptance level.\n- Scaffold selection chooses a validated starter shape.\n- Generation fills scaffold slots rather than inventing file topology from scratch.\n- Validation emits structured diagnostics.\n- Repair consumes diagnostic JSON plus source references.\n"
}
