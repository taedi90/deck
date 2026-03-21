package askcontext

import (
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/taedi90/deck/internal/schemadoc"
	"github.com/taedi90/deck/internal/validate"
	"github.com/taedi90/deck/internal/workflowexec"
	"github.com/taedi90/deck/internal/workspacepaths"
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
				`deck ask --write "create an air-gapped rhel9 kubeadm cluster workflow"`,
				`deck ask plan "create an air-gapped rhel9 kubeadm cluster workflow"`,
			},
		},
		Topology: WorkspaceTopology{
			WorkflowRoot:      workspacepaths.WorkflowRootDir,
			ScenarioDir:       pathJoin(workspacepaths.WorkflowRootDir, workspacepaths.WorkflowScenariosDir),
			ComponentDir:      pathJoin(workspacepaths.WorkflowRootDir, workspacepaths.WorkflowComponentsDir),
			VarsPath:          pathJoin(workspacepaths.WorkflowRootDir, workspacepaths.WorkflowVarsRel),
			AllowedPaths:      AllowedGeneratedPathPatterns(),
			CanonicalPrepare:  workspacepaths.CanonicalPrepareWorkflowRel,
			CanonicalApply:    pathJoin(workspacepaths.WorkflowRootDir, workspacepaths.CanonicalApplyWorkflowRel),
			GeneratedPathNote: "New ask-generated files must stay under workflows/prepare.yaml, workflows/scenarios/, workflows/components/, or workflows/vars.yaml.",
		},
		Workflow: WorkflowRules{
			Summary:          workflow.Summary,
			TopLevelModes:    validate.WorkflowTopLevelModes(),
			SupportedModes:   validate.SupportedWorkflowRoles(),
			SupportedVersion: validate.SupportedWorkflowVersion(),
			ImportRule:       validate.WorkflowImportRule(),
			Notes:            append([]string(nil), validate.WorkflowInvariantNotes()...),
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
				WhenToUse:   "Use apply for package installation, file writes, service changes, runtime config, and host convergence steps.",
				Prefer:      []string{"typed steps such as File, ConfigureRepository, RefreshRepository, ManageService, WriteContainerdConfig, and Package", "named phases for multi-step installs", "components for reusable imported logic"},
				Avoid:       []string{"online collection logic that should happen during prepare", "large repeated literals that belong in vars.yaml"},
				OutputFiles: []string{pathJoin(workspacepaths.WorkflowRootDir, workspacepaths.CanonicalApplyWorkflowRel), pathJoin(workspacepaths.WorkflowRootDir, workspacepaths.WorkflowVarsRel)},
			},
		},
		Components: ComponentGuidance{
			Summary:      "Reusable workflow fragments belong in workflows/components/ and are imported into scenario phases.",
			ImportRule:   "Imports are only valid under phases[].imports and resolve from workflows/components/ using component-relative paths.",
			ReuseRule:    "Split repeated or reusable logic into components instead of duplicating steps across scenarios.",
			LocationRule: "Scenario entrypoints live under workflows/scenarios/ while imported fragments live under workflows/components/.",
		},
		Vars: VarsGuidance{
			Path:        pathJoin(workspacepaths.WorkflowRootDir, workspacepaths.WorkflowVarsRel),
			Summary:     "Prefer workflows/vars.yaml for configurable values that would otherwise be repeated inline across steps or files.",
			PreferFor:   []string{"package lists", "repository URLs", "service names", "paths and ports that may vary by environment"},
			AvoidFor:    []string{"runtime-only outputs registered from previous steps", "tiny one-off literals with no reuse value", "typed step fields whose schema expects a native YAML array or object but the template engine would turn into a string"},
			ExampleKeys: []string{"dockerRepoURL", "dockerPackages", "containerRuntimeConfigPath"},
		},
		StepKinds: buildStepKinds(),
	}
	return manifest
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
	defs := workflowexec.StepDefinitions()
	out := make([]StepKindContext, 0, len(defs))
	for _, def := range defs {
		meta := schemadoc.ToolMeta(def.Kind)
		contract, _ := workflowexec.StepContractForKind(def.Kind)
		ctx := StepKindContext{
			Kind:         def.Kind,
			Category:     def.Category,
			Summary:      meta.Summary,
			WhenToUse:    meta.WhenToUse,
			SchemaFile:   def.SchemaFile,
			AllowedRoles: sortedKeys(contract.Roles),
			Outputs:      sortedKeys(contract.Outputs),
			MinimalShape: strings.TrimSpace(meta.Example),
			CuratedShape: strings.TrimSpace(meta.Example),
			KeyFields:    buildStepKeyFields(def.Kind, meta),
			Notes:        append([]string(nil), meta.Notes...),
		}
		ctx.Outputs = dedupe(ctx.Outputs)
		out = append(out, ctx)
	}
	return out
}

func buildStepKeyFields(kind string, meta schemadoc.ToolMetadata) []StepFieldContext {
	preferred := map[string][]string{
		"DownloadPackage":     {"spec.packages", "spec.distro", "spec.repo", "spec.backend", "spec.outputDir"},
		"InstallPackage":      {"spec.packages", "spec.source", "spec.restrictToRepos", "spec.excludeRepos"},
		"ConfigureRepository": {"spec.format", "spec.path", "spec.repositories", "spec.replaceExisting", "spec.cleanupPaths"},
		"RefreshRepository":   {"spec.manager", "spec.clean", "spec.update", "spec.restrictToRepos", "spec.excludeRepos"},
		"ManageService":       {"spec.name", "spec.names", "spec.state", "spec.enabled"},
		"DownloadFile":        {"spec.source", "spec.fetch", "spec.outputPath", "spec.mode"},
		"WriteFile":           {"spec.path", "spec.content", "spec.template", "spec.mode"},
		"CopyFile":            {"spec.source", "spec.path", "spec.mode"},
		"EditFile":            {"spec.path", "spec.edits", "spec.backup", "spec.mode"},
	}
	keys := preferred[kind]
	if len(keys) == 0 {
		keys = []string{"spec.path", "spec.source", "spec.content"}
	}
	out := make([]StepFieldContext, 0, len(keys))
	for _, key := range keys {
		field, ok := meta.FieldDocs[key]
		if !ok {
			continue
		}
		out = append(out, StepFieldContext{Path: key, Description: field.Description, Example: field.Example})
	}
	return out
}

func sortedKeys(values map[string]bool) []string {
	out := make([]string, 0, len(values))
	for value, ok := range values {
		if ok {
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
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
