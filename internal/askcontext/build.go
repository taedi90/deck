package askcontext

import (
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/Airgap-Castaways/deck/internal/schemadoc"
	"github.com/Airgap-Castaways/deck/internal/validate"
	"github.com/Airgap-Castaways/deck/internal/workflowexec"
	"github.com/Airgap-Castaways/deck/internal/workspacepaths"
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
	defs := workflowexec.BuiltInTypeDefinitions()
	out := make([]StepKindContext, 0, len(defs))
	for _, def := range defs {
		meta := def.Docs
		ctx := StepKindContext{
			Kind:         def.Step.Kind,
			Category:     def.Step.Category,
			Summary:      meta.Summary,
			WhenToUse:    meta.WhenToUse,
			SchemaFile:   def.Step.SchemaFile,
			AllowedRoles: append([]string(nil), def.Step.Roles...),
			Outputs:      append([]string(nil), def.Step.Outputs...),
			MinimalShape: strings.TrimSpace(meta.Example),
			CuratedShape: strings.TrimSpace(meta.Example),
			KeyFields:    buildStepKeyFields(def.Step.Kind, meta),
			Notes:        append([]string(nil), meta.Notes...),
		}
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
	ctx.PromptExamples = append([]StepExampleContext(nil), defaultPromptExamples(ctx)...)
	ctx.CommonMistakes = append([]string(nil), defaultCommonMistakes(ctx.Kind)...)
	ctx.RepairHints = append([]string(nil), defaultRepairHints(ctx.Kind)...)
	ctx.ValidationHints = append([]ValidationHint(nil), defaultValidationHints(ctx.Kind)...)
	ctx.QualityRules = append([]QualityRule(nil), defaultQualityRules(ctx.Kind)...)
	if ctx.Kind == "Command" {
		ctx.AntiSignals = []string{"typed", "typed steps", "where possible"}
	}
}

func defaultMatchSignals(kind string) []string {
	switch kind {
	case "CheckHost":
		return []string{"host", "preflight", "rhel", "rocky", "ubuntu", "air-gapped", "single-node"}
	case "LoadImage":
		return []string{"air-gapped", "image", "images", "archive", "containerd", "docker", "offline"}
	case "CheckCluster":
		return []string{"kubernetes", "kubeadm", "cluster", "verify", "health", "ready"}
	case "InstallPackage":
		return []string{"install", "package", "packages", "rpm", "dnf", "apt"}
	case "DownloadPackage":
		return []string{"download", "package", "packages", "rpm", "dnf", "air-gapped", "offline"}
	case "ConfigureRepository":
		return []string{"repo", "repository", "mirror", "yum", "dnf", "apt"}
	case "RefreshRepository":
		return []string{"repo", "repository", "metadata", "refresh", "cache", "dnf", "apt"}
	case "ManageService":
		return []string{"service", "enable", "restart", "reload", "systemctl"}
	case "KernelModule":
		return []string{"kernel", "module", "br_netfilter", "overlay", "kubernetes"}
	case "WriteFile":
		return []string{"write", "file", "config", "motd", "content"}
	case "EditYAML":
		return []string{"yaml", "edit", "patch", "config"}
	case "Command":
		return []string{"shell", "command", "script", "escape hatch"}
	default:
		return nil
	}
}

func defaultPromptExamples(ctx *StepKindContext) []StepExampleContext {
	if strings.TrimSpace(ctx.CuratedShape) == "" {
		return nil
	}
	return []StepExampleContext{{Purpose: "compact shape", YAML: strings.TrimSpace(ctx.CuratedShape)}}
}

func defaultCommonMistakes(kind string) []string {
	switch kind {
	case "CheckHost":
		return []string{
			"Use spec.checks as a YAML string array such as [os, arch, swap].",
			"Do not invent nested objects like spec.os or object items under spec.checks.",
		}
	case "LoadImage":
		return []string{
			"Keep spec.images in the schema-supported shape from the step example.",
			"Do not replace the whole images collection with a single quoted template scalar.",
		}
	case "CheckCluster":
		return []string{
			"Follow the documented checks shape from the example instead of inventing custom polling fields.",
		}
	case "InstallPackage", "DownloadPackage":
		return []string{
			"spec.packages must stay a real YAML array, not a quoted template string.",
			"Do not set spec.packages to `{{ .vars.* }}` or any other whole-value template expression; inline package items instead.",
		}
	case "ConfigureRepository":
		return []string{
			"spec.repositories must stay a real YAML array of repository objects, not a scalar shortcut.",
			"Do not set spec.repositories to `{{ .vars.* }}` or any other whole-value template expression; inline repository objects instead.",
		}
	default:
		return nil
	}
}

func defaultRepairHints(kind string) []string {
	switch kind {
	case "CheckHost":
		return []string{
			"For CheckHost, use spec.checks as a string array like [os, arch, swap].",
			"If binary presence matters, keep names under spec.binaries and include binaries in spec.checks.",
		}
	case "LoadImage":
		return []string{"Return a schema-valid LoadImage spec using the documented image archive shape from ask metadata."}
	case "CheckCluster":
		return []string{"Return a schema-valid CheckCluster spec using documented checks instead of ad hoc readiness fields."}
	case "InstallPackage", "DownloadPackage":
		return []string{"Inline concrete YAML arrays for spec.packages rather than using a whole-value template expression."}
	case "ConfigureRepository":
		return []string{"Inline repository objects under spec.repositories rather than using a scalar or whole-value template."}
	default:
		return nil
	}
}

func defaultValidationHints(kind string) []ValidationHint {
	switch kind {
	case "CheckHost":
		return []ValidationHint{
			{ErrorContains: "checkhost", Fix: "For CheckHost, use spec.checks as a YAML string array like [os, arch, swap]."},
			{ErrorContains: "checks is required", Fix: "CheckHost requires spec.checks. Example: spec: {checks: [os, arch, swap]}."},
			{ErrorContains: "additional property os is not allowed", Fix: "Do not use spec.os for CheckHost; put named checks under spec.checks instead."},
			{ErrorContains: "spec.checks.0: invalid type", Fix: "Each CheckHost spec.checks item must be a plain string such as os or arch, not an object."},
		}
	case "InstallPackage", "DownloadPackage":
		return []ValidationHint{{ErrorContains: "invalid map key", Fix: "Do not use whole-value template expressions for package arrays; inline YAML list items under spec.packages."}}
	case "ConfigureRepository":
		return []ValidationHint{{ErrorContains: "invalid map key", Fix: "Do not use whole-value template expressions for spec.repositories; inline YAML repository objects instead."}}
	default:
		return nil
	}
}

func defaultQualityRules(kind string) []QualityRule {
	switch kind {
	case "Command":
		return []QualityRule{{Trigger: "typed-preferred", Message: "Prefer a typed step when one clearly matches the requested host action instead of using Command only.", Level: "advisory"}}
	default:
		return nil
	}
}

func buildStepKeyFields(kind string, meta workflowexec.ToolMetadata) []StepFieldContext {
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
