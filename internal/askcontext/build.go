package askcontext

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/Airgap-Castaways/deck/internal/schemadoc"
	"github.com/Airgap-Castaways/deck/internal/schemafacts"
	"github.com/Airgap-Castaways/deck/internal/stepmeta"
	"github.com/Airgap-Castaways/deck/internal/validate"
	"github.com/Airgap-Castaways/deck/internal/workflowexec"
	"github.com/Airgap-Castaways/deck/internal/workflowissues"
	"github.com/Airgap-Castaways/deck/internal/workspacepaths"
	deckschemas "github.com/Airgap-Castaways/deck/schemas"
)

var (
	manifestOnce sync.Once
	manifestData Manifest
)

func Current() Manifest {
	manifestOnce.Do(func() {
		manifestData = buildManifest()
	})
	return manifestData
}

func buildManifest() Manifest {
	workflow := schemadoc.WorkflowMeta()
	cli := AskCommandMeta()
	manifest := Manifest{
		CLI: CLIContext{
			Command:             "deck ask",
			PlanSubcommand:      "deck ask plan",
			ConfigSubcommand:    "deck ask config",
			TopLevelDescription: cli.Short,
			ImportantFlags:      append([]CLIFlag(nil), cli.Flags...),
			Examples: []string{
				`deck ask "explain what workflows/scenarios/apply.yaml does"`,
				`deck ask --write "create an air-gapped rhel9 single-node kubeadm workflow"`,
				`deck ask plan "create an air-gapped rhel9 single-node kubeadm workflow"`,
			},
		},
		Topology: WorkspaceTopology{
			WorkflowRoot:      workspacepaths.WorkflowRootDir,
			ScenarioDir:       pathJoin(workspacepaths.WorkflowRootDir, workspacepaths.WorkflowScenariosDir),
			ComponentDir:      pathJoin(workspacepaths.WorkflowRootDir, workspacepaths.WorkflowComponentsDir),
			VarsPath:          pathJoin(workspacepaths.WorkflowRootDir, workspacepaths.WorkflowVarsRel),
			AllowedPaths:      AllowedGeneratedPathPatterns(),
			CanonicalPrepare:  pathJoin(workspacepaths.WorkflowRootDir, workspacepaths.CanonicalPrepareWorkflowRel),
			CanonicalApply:    pathJoin(workspacepaths.WorkflowRootDir, workspacepaths.CanonicalApplyWorkflowRel),
			GeneratedPathNote: "New ask-generated files must stay under workflows/prepare.yaml, workflows/scenarios/, workflows/components/, or workflows/vars.yaml.",
		},
		Workflow: WorkflowRules{
			Summary:          workflow.Summary,
			TopLevelModes:    validate.WorkflowTopLevelModes(),
			SupportedModes:   validate.SupportedWorkflowRoles(),
			SupportedVersion: validate.SupportedWorkflowVersion(),
			ImportRule:       validate.WorkflowImportRule(),
			RequiredFields:   workflowRequiredFields(),
			PhaseRules: []string{
				"Each phase needs a non-empty name.",
				"Each phase must define steps or imports.",
				"Phase objects do not support an id field.",
			},
			StepRules: []string{
				"Each step needs id, kind, and spec.",
				"Step ids belong on steps, not phases.",
				workflowissues.MustSpec(workflowissues.CodeDuplicateStepID).Details,
			},
			PhaseExample: strings.TrimSpace(`version: v1alpha1
phases:
  - name: bootstrap
    steps:
      - id: check-host
        kind: CheckHost
        spec:
          checks: [os, arch, swap]
          failFast: true`),
			StepsExample: strings.TrimSpace(`version: v1alpha1
steps:
  - id: run-command
    kind: Command
    spec:
      command: [echo, hello]`),
			Notes: append([]string(nil), validate.WorkflowInvariantNotes()...),
		},
		Policy: AuthoringPolicy{
			AssumeOfflineByDefault: true,
			PrepareArtifactKinds:   []string{"package", "image", "binary", "archive", "bundle", "repository-mirror"},
			ForbiddenApplyActions: []string{
				"remote package download",
				"remote image pull",
				"remote binary download",
				"remote archive fetch",
				"online repository sync",
			},
			VarsAdvisory: []string{
				"Repeated package lists, image lists, paths, versions, or environment-specific values should move to workflows/vars.yaml.",
				"Missing vars should not block generation on its own.",
				"Detected local host facts belong under runtime.host in when expressions, not in workflows/vars.yaml.",
				"workflows/vars.yaml must remain plain YAML data. Do not place template expressions in vars values, keys, or unquoted scalar positions.",
			},
			ComponentAdvisory: []string{
				"Reusable repeated logic across phases or scenarios should usually move into workflows/components/.",
				"Missing components should not block generation on its own.",
			},
		},
		Modes: []ModeGuidance{
			{
				Mode:        "prepare",
				Summary:     "Prepare collects online inputs and produces offline-ready artifacts.",
				WhenToUse:   "Use prepare when the request needs downloads, mirrored images, package caches, or bundle content created before apply.",
				Prefer:      []string{"download-oriented File, Image, and Package steps", "variables shared by later apply steps", "named phases when collection has multiple stages"},
				Avoid:       []string{"live node reconfiguration that belongs in apply", "service management on the target node"},
				OutputFiles: []string{workspacepaths.CanonicalPrepareWorkflowRel, pathJoin(workspacepaths.WorkflowRootDir, workspacepaths.WorkflowVarsRel)},
			},
			{
				Mode:        "apply",
				Summary:     "Apply changes the local node using prepared inputs and typed host actions.",
				WhenToUse:   "Use apply for package installation, file writes, service changes, runtime config, host convergence steps, and host suitability validation.",
				Prefer:      []string{"typed steps such as File, ConfigureRepository, RefreshRepository, ManageService, WriteContainerdConfig, Package, and CheckHost", "runtime.host.* for detected local host branching", "named phases for multi-step installs", "components for reusable imported logic"},
				Avoid:       []string{"online collection logic that should happen during prepare", "large repeated literals that belong in vars.yaml"},
				OutputFiles: []string{pathJoin(workspacepaths.WorkflowRootDir, workspacepaths.CanonicalApplyWorkflowRel), pathJoin(workspacepaths.WorkflowRootDir, workspacepaths.WorkflowVarsRel)},
			},
		},
		Components: ComponentGuidance{
			Summary:      "Reusable workflow fragments belong in workflows/components/ and are imported into scenario phases.",
			ImportRule:   "Imports are only valid under phases[].imports and resolve from workflows/components/ using component-relative paths.",
			ReuseRule:    "Split repeated or reusable logic into components instead of duplicating steps across scenarios.",
			LocationRule: "Scenario entrypoints live under workflows/scenarios/ while imported fragments live under workflows/components/.",
			FragmentRule: "Component files are fragment documents, not full workflow documents. They should usually contain a top-level `steps:` mapping only and must not add workflow-level fields like version or phases.",
			ImportExample: strings.TrimSpace(`phases:
  - name: preflight
    imports:
      - path: check-host.yaml`),
			FragmentExample: strings.TrimSpace(`steps:
  - id: check-host
    kind: CheckHost
    spec:
      checks: [os, arch, swap]
      failFast: true`),
		},
		Vars: VarsGuidance{
			Path:        pathJoin(workspacepaths.WorkflowRootDir, workspacepaths.WorkflowVarsRel),
			Summary:     "Prefer workflows/vars.yaml for configurable values that would otherwise be repeated inline across steps or files.",
			PreferFor:   []string{"package lists", "repository URLs", "service names", "paths and ports that may vary by environment"},
			AvoidFor:    []string{"runtime-only outputs registered from previous steps", "detected local host facts such as osFamily or arch that already exist under runtime.host", "tiny one-off literals with no reuse value", "typed step fields whose schema expects a native YAML array or object but the template engine would turn into a string", "typed enum fields or constrained scalar fields that must stay literal to satisfy schema validation"},
			ExampleKeys: []string{"dockerRepoURL", "dockerPackages", "containerRuntimeConfigPath"},
		},
		StepKinds: buildStepKinds(),
	}
	return manifest
}

func workflowRequiredFields() []string {
	raw, err := deckschemas.WorkflowSchema()
	if err != nil {
		return []string{"version"}
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		return []string{"version"}
	}
	facts := schemafacts.Analyze(schema)
	fields := schemafacts.FilterDirectChildFields(facts.Fields, "")
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if field.Requirement == schemafacts.RequirementRequired {
			out = append(out, field.Path)
		}
	}
	sort.Strings(out)
	if len(out) == 0 {
		return []string{"version"}
	}
	return out
}

func AllowedGeneratedPathPatterns() []string {
	return []string{"workflows/prepare.yaml", "workflows/scenarios/*.yaml", "workflows/components/*.yaml", "workflows/vars.yaml"}
}

func AllowedGeneratedPath(path string) bool {
	clean := filepath.ToSlash(strings.TrimSpace(path))
	if clean == "" || strings.Contains(clean, "..") {
		return false
	}
	return clean == "workflows/prepare.yaml" || strings.HasPrefix(clean, "workflows/scenarios/") || strings.HasPrefix(clean, "workflows/components/") || clean == "workflows/vars.yaml"
}

func buildStepKinds() []StepKindContext {
	defs := workflowexec.BuiltInTypeDefinitions()
	out := make([]StepKindContext, 0, len(defs))
	for _, def := range defs {
		meta := schemadoc.ToolMetaForDefinition(def.Step)
		facts := schemaFactsForKind(def.Step.Kind)
		ctx := StepKindContext{
			Kind:                def.Step.Kind,
			Category:            def.Step.Category,
			Summary:             meta.Summary,
			WhenToUse:           meta.WhenToUse,
			SchemaFile:          def.Step.SchemaFile,
			AllowedRoles:        append([]string(nil), def.Step.Roles...),
			Outputs:             append([]string(nil), def.Step.Outputs...),
			MinimalShape:        strings.TrimSpace(meta.Example),
			CuratedShape:        strings.TrimSpace(meta.Example),
			KeyFields:           buildStepKeyFields(def.Step.Kind, meta, facts),
			SchemaRuleSummaries: append([]string(nil), facts.RuleSummaries...),
			Notes:               append([]string(nil), meta.Notes...),
		}
		ctx.PromptExamples = promptExamplesFromShape(ctx.CuratedShape)
		applyCuratedStepMetadata(&ctx)
		ctx.Outputs = dedupe(ctx.Outputs)
		out = append(out, ctx)
	}
	return out
}

func applyCuratedStepMetadata(ctx *StepKindContext) {
	// Derived fields come from workflow contracts and schema doc metadata assembled in
	// buildStepKinds. This helper adds a narrow curated layer only for recurring ask
	// quality issues such as common schema mistakes, repair hints, and prompt-ready
	// examples that are awkward to infer directly from validator output.
	ctx.MatchSignals = append([]string(nil), defaultMatchSignals(ctx.Kind)...)
	ctx.CommonMistakes = append([]string(nil), defaultCommonMistakes(ctx.Kind)...)
	ctx.RepairHints = append([]string(nil), defaultRepairHints(ctx.Kind)...)
	ctx.ValidationHints = append([]ValidationHint(nil), defaultValidationHints(ctx.Kind)...)
	ctx.ConstrainedLiteralFields = append([]ConstrainedFieldHint(nil), defaultConstrainedLiteralFields(ctx.Kind)...)
	ctx.QualityRules = append([]QualityRule(nil), defaultQualityRules(ctx.Kind)...)
	if ask, ok := stepmetaAsk(ctx.Kind); ok && len(ask.AntiSignals) > 0 {
		ctx.AntiSignals = append([]string(nil), ask.AntiSignals...)
	}
}

func stepmetaAsk(kind string) (stepmeta.AskMetadata, bool) {
	entry, ok, err := stepmeta.LookupCatalogEntry(kind)
	if err != nil || !ok {
		return stepmeta.AskMetadata{}, false
	}
	return stepmeta.ProjectAsk(entry), true
}

func defaultMatchSignals(kind string) []string {
	if ask, ok := stepmetaAsk(kind); ok && len(ask.MatchSignals) > 0 {
		return append([]string(nil), ask.MatchSignals...)
	}
	return nil
}

func promptExamplesFromShape(shape string) []StepExampleContext {
	if strings.TrimSpace(shape) == "" {
		return nil
	}
	return []StepExampleContext{{Purpose: "compact shape", YAML: strings.TrimSpace(shape)}}
}

func defaultCommonMistakes(kind string) []string {
	if ask, ok := stepmetaAsk(kind); ok && len(ask.CommonMistakes) > 0 {
		return append([]string(nil), ask.CommonMistakes...)
	}
	return nil
}

func defaultRepairHints(kind string) []string {
	if ask, ok := stepmetaAsk(kind); ok && len(ask.RepairHints) > 0 {
		return append([]string(nil), ask.RepairHints...)
	}
	return nil
}

func defaultValidationHints(kind string) []ValidationHint {
	if ask, ok := stepmetaAsk(kind); ok && len(ask.ValidationHints) > 0 {
		out := make([]ValidationHint, 0, len(ask.ValidationHints))
		for _, hint := range ask.ValidationHints {
			out = append(out, ValidationHint{ErrorContains: hint.ErrorContains, Fix: hint.Fix})
		}
		return out
	}
	return nil
}

func defaultQualityRules(kind string) []QualityRule {
	if ask, ok := stepmetaAsk(kind); ok && len(ask.QualityRules) > 0 {
		out := make([]QualityRule, 0, len(ask.QualityRules))
		for _, rule := range ask.QualityRules {
			out = append(out, QualityRule{Trigger: rule.Trigger, Message: rule.Message, Level: rule.Level})
		}
		return out
	}
	return nil
}

func defaultConstrainedLiteralFields(kind string) []ConstrainedFieldHint {
	if ask, ok := stepmetaAsk(kind); ok && len(ask.ConstrainedLiteralFields) > 0 {
		out := make([]ConstrainedFieldHint, 0, len(ask.ConstrainedLiteralFields))
		for _, field := range ask.ConstrainedLiteralFields {
			out = append(out, ConstrainedFieldHint{Path: field.Path, AllowedValues: append([]string(nil), field.AllowedValues...), Guidance: field.Guidance})
		}
		return out
	}
	return nil
}

func buildStepKeyFields(kind string, meta schemadoc.ToolMetadata, facts schemafacts.DocumentFacts) []StepFieldContext {
	fieldRequirements := map[string]string{}
	for _, field := range facts.Fields {
		if strings.HasPrefix(field.Path, "spec") {
			fieldRequirements[field.Path] = string(field.Requirement)
		}
	}
	keys := stepmetaKeyFields(kind)
	if len(keys) == 0 {
		keys = deriveKeyFields(meta, fieldRequirements)
	}
	out := make([]StepFieldContext, 0, len(keys))
	for _, key := range keys {
		field, ok := meta.FieldDocs[key]
		if !ok {
			continue
		}
		requirement := fieldRequirements[key]
		if requirement == "" {
			requirement = "optional"
		}
		out = append(out, StepFieldContext{Path: key, Description: field.Description, Example: field.Example, Requirement: requirement})
	}
	return out
}

func deriveKeyFields(meta schemadoc.ToolMetadata, fieldRequirements map[string]string) []string {
	type candidate struct {
		path        string
		requirement string
	}
	candidates := make([]candidate, 0, len(meta.FieldDocs))
	for path := range meta.FieldDocs {
		if !strings.HasPrefix(path, "spec.") {
			continue
		}
		remainder := strings.TrimPrefix(path, "spec.")
		if strings.Contains(remainder, "[]") || strings.Count(remainder, ".") > 0 {
			continue
		}
		requirement := fieldRequirements[path]
		if requirement == "" {
			requirement = "optional"
		}
		candidates = append(candidates, candidate{path: path, requirement: requirement})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].requirement != candidates[j].requirement {
			return candidates[i].requirement == "required"
		}
		return candidates[i].path < candidates[j].path
	})
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, candidate.path)
		if len(out) >= 5 {
			break
		}
	}
	return out
}

func stepmetaKeyFields(kind string) []string {
	if ask, ok := stepmetaAsk(kind); ok && len(ask.KeyFields) > 0 {
		return append([]string(nil), ask.KeyFields...)
	}
	return nil
}

func schemaFactsForKind(kind string) schemafacts.DocumentFacts {
	var schemaFile string
	for _, def := range workflowexec.StepDefinitions() {
		if def.Kind == strings.TrimSpace(kind) {
			schemaFile = strings.TrimSpace(def.SchemaFile)
			break
		}
	}
	if schemaFile == "" {
		return schemafacts.DocumentFacts{}
	}
	raw, err := deckschemas.ToolSchema(schemaFile)
	if err != nil {
		return schemafacts.DocumentFacts{}
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		return schemafacts.DocumentFacts{}
	}
	facts := schemafacts.Analyze(schema)
	if props, _ := schema["properties"].(map[string]any); len(props) > 0 {
		if spec, _ := props["spec"].(map[string]any); len(spec) > 0 {
			facts.RuleSummaries = schemafacts.ExtractRules(spec, "spec")
		}
	}
	return facts
}

func dedupe(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func pathJoin(parts ...string) string {
	return strings.Join(parts, "/")
}
