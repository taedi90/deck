package askcontext

import "strings"

func InvariantPromptBlock() PromptBlock {
	manifest := Current()
	b := &strings.Builder{}
	b.WriteString("Workflow invariants:\n")
	b.WriteString("- Supported command modes: ")
	b.WriteString(strings.Join(manifest.Workflow.SupportedModes, ", "))
	b.WriteString("\n")
	b.WriteString("- Supported workflow version: ")
	b.WriteString(manifest.Workflow.SupportedVersion)
	b.WriteString("\n")
	b.WriteString("- Allowed generated paths: ")
	b.WriteString(strings.Join(manifest.Topology.AllowedPaths, ", "))
	b.WriteString("\n")
	b.WriteString("- Top-level workflow modes: ")
	b.WriteString(strings.Join(manifest.Workflow.TopLevelModes, ", "))
	b.WriteString("\n")
	if len(manifest.Workflow.RequiredFields) > 0 {
		b.WriteString("- Required workflow fields: ")
		b.WriteString(strings.Join(manifest.Workflow.RequiredFields, ", "))
		b.WriteString("\n")
	}
	for _, note := range manifest.Workflow.Notes {
		b.WriteString("- ")
		b.WriteString(note)
		b.WriteString("\n")
	}
	for _, rule := range manifest.Workflow.PhaseRules {
		b.WriteString("- ")
		b.WriteString(rule)
		b.WriteString("\n")
	}
	for _, rule := range manifest.Workflow.StepRules {
		b.WriteString("- ")
		b.WriteString(rule)
		b.WriteString("\n")
	}
	if manifest.Workflow.PhaseExample != "" {
		b.WriteString("- Minimal phase-based workflow example:\n")
		for _, line := range strings.Split(manifest.Workflow.PhaseExample, "\n") {
			b.WriteString("  ")
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	if manifest.Workflow.StepsExample != "" {
		b.WriteString("- Minimal top-level steps workflow example:\n")
		for _, line := range strings.Split(manifest.Workflow.StepsExample, "\n") {
			b.WriteString("  ")
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	return PromptBlock{Topic: TopicWorkflowInvariants, Title: "Workflow invariants", Content: strings.TrimSpace(b.String())}
}

func PolicyPromptBlock() PromptBlock {
	b := &strings.Builder{}
	b.WriteString("Workflow authoring policy:\n")
	b.WriteString("- Prefer typed steps over Command whenever a typed step expresses the change clearly.\n")
	b.WriteString("- Prefer workflows/vars.yaml for repeated configurable values instead of scattering literals across steps.\n")
	b.WriteString("- Do not replace schema-typed arrays or objects with string templates. Keep arrays as YAML arrays and objects as YAML objects so schema validation still passes.\n")
	b.WriteString("- Split repeated logic into reusable components and import them under phases[].imports.\n")
	b.WriteString("- Use the `prepare` command for online collection or offline bundle preparation and `apply` for local node changes.\n")
	return PromptBlock{Topic: TopicPolicy, Title: "Workflow authoring policy", Content: strings.TrimSpace(b.String())}
}

func GlobalAuthoringBlock() string {
	b := &strings.Builder{}
	b.WriteString(InvariantPromptBlock().Content)
	b.WriteString("\n")
	b.WriteString(PolicyPromptBlock().Content)
	return strings.TrimSpace(b.String())
}

func WorkspaceTopologyBlock() string {
	manifest := Current()
	b := &strings.Builder{}
	b.WriteString("Workspace topology:\n")
	b.WriteString("- Scenario entrypoints: ")
	b.WriteString(manifest.Topology.ScenarioDir)
	b.WriteString("\n")
	b.WriteString("- Reusable components: ")
	b.WriteString(manifest.Topology.ComponentDir)
	b.WriteString("\n")
	b.WriteString("- Shared variables file: ")
	b.WriteString(manifest.Topology.VarsPath)
	b.WriteString("\n")
	b.WriteString("- Canonical prepare entrypoint: ")
	b.WriteString(manifest.Topology.CanonicalPrepare)
	b.WriteString("\n")
	b.WriteString("- Canonical apply scenario: ")
	b.WriteString(manifest.Topology.CanonicalApply)
	b.WriteString("\n")
	b.WriteString("- Allowed generated paths: ")
	b.WriteString(strings.Join(manifest.Topology.AllowedPaths, ", "))
	b.WriteString("\n")
	b.WriteString("- ")
	b.WriteString(manifest.Topology.GeneratedPathNote)
	return strings.TrimSpace(b.String())
}

func WorkspaceTopologyPromptBlock() PromptBlock {
	return PromptBlock{Topic: TopicWorkspaceTopology, Title: "Workspace topology", Content: WorkspaceTopologyBlock()}
}

func RoleGuidanceBlock() string {
	manifest := Current()
	b := &strings.Builder{}
	b.WriteString("Command-mode guidance:\n")
	for _, mode := range manifest.Modes {
		b.WriteString("- ")
		b.WriteString("`")
		b.WriteString(mode.Mode)
		b.WriteString("` command: ")
		b.WriteString(mode.Summary)
		b.WriteString(" Use when: ")
		b.WriteString(mode.WhenToUse)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func RoleGuidancePromptBlock() PromptBlock {
	return PromptBlock{Topic: TopicPrepareApplyGuidance, Title: "Command-mode guidance", Content: RoleGuidanceBlock()}
}

func ComponentGuidanceBlock() string {
	manifest := Current()
	b := &strings.Builder{}
	b.WriteString("Components and imports:\n")
	b.WriteString("- ")
	b.WriteString(manifest.Components.Summary)
	b.WriteString("\n- ")
	b.WriteString(manifest.Components.ImportRule)
	b.WriteString("\n- ")
	b.WriteString(manifest.Components.ReuseRule)
	b.WriteString("\n- ")
	b.WriteString(manifest.Components.LocationRule)
	if strings.TrimSpace(manifest.Components.FragmentRule) != "" {
		b.WriteString("\n- ")
		b.WriteString(manifest.Components.FragmentRule)
	}
	if strings.TrimSpace(manifest.Components.ImportExample) != "" {
		b.WriteString("\n- Import example:\n")
		for _, line := range strings.Split(manifest.Components.ImportExample, "\n") {
			b.WriteString("  ")
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	if strings.TrimSpace(manifest.Components.FragmentExample) != "" {
		b.WriteString("- Component fragment example:\n")
		for _, line := range strings.Split(manifest.Components.FragmentExample, "\n") {
			b.WriteString("  ")
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	return strings.TrimSpace(b.String())
}

func ComponentGuidancePromptBlock() PromptBlock {
	return PromptBlock{Topic: TopicComponentsImports, Title: "Components and imports", Content: ComponentGuidanceBlock()}
}

func VarsGuidanceBlock() string {
	manifest := Current()
	b := &strings.Builder{}
	b.WriteString("Variables guidance:\n")
	b.WriteString("- ")
	b.WriteString(manifest.Vars.Summary)
	b.WriteString("\n- Prefer vars.yaml for: ")
	b.WriteString(strings.Join(manifest.Vars.PreferFor, ", "))
	b.WriteString("\n- Avoid vars.yaml for: ")
	b.WriteString(strings.Join(manifest.Vars.AvoidFor, ", "))
	b.WriteString("\n- Keep schema-typed arrays/objects inline as real YAML arrays/objects when the step schema requires them.")
	b.WriteString("\n- Example vars keys: ")
	b.WriteString(strings.Join(manifest.Vars.ExampleKeys, ", "))
	return strings.TrimSpace(b.String())
}

func VarsGuidancePromptBlock() PromptBlock {
	return PromptBlock{Topic: TopicVarsGuidance, Title: "Variables guidance", Content: VarsGuidanceBlock()}
}

func CLIHintsBlock() string {
	manifest := Current()
	b := &strings.Builder{}
	b.WriteString("Relevant CLI usage:\n")
	b.WriteString("- ")
	b.WriteString(manifest.CLI.Command)
	b.WriteString(" previews by default; add --write to write files.\n")
	b.WriteString("- ")
	b.WriteString(manifest.CLI.PlanSubcommand)
	b.WriteString(" saves a reusable plan artifact without writing workflow files.\n")
	for _, flag := range manifest.CLI.ImportantFlags {
		b.WriteString("- ")
		b.WriteString(flag.Name)
		b.WriteString(": ")
		b.WriteString(flag.Description)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func CLIHintsPromptBlock() PromptBlock {
	return PromptBlock{Topic: TopicCLIHints, Title: "Relevant CLI usage", Content: CLIHintsBlock()}
}

func RelevantStepKindsPromptBlock(prompt string) PromptBlock {
	return PromptBlock{Topic: TopicTypedSteps, Title: "Relevant typed steps", Content: RelevantStepKindsBlock(prompt)}
}
