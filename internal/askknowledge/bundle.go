package askknowledge

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/Airgap-Castaways/deck/internal/askcontext"
	"github.com/Airgap-Castaways/deck/internal/workflowcontract"
)

type Bundle struct {
	Workflow    WorkflowKnowledge
	Topology    TopologyKnowledge
	Components  ComponentKnowledge
	Vars        VarsKnowledge
	Policy      PolicyKnowledge
	Steps       []StepKnowledge
	Constraints []ConstraintKnowledge
}

type WorkflowKnowledge struct {
	SupportedRoles   []string
	SupportedVersion string
	TopLevelModes    []string
	RequiredFields   []string
	Notes            []string
	PhaseExample     string
	StepsExample     string
}

type TopologyKnowledge struct {
	WorkflowRoot     string
	ScenarioDir      string
	ComponentDir     string
	VarsPath         string
	CanonicalPrepare string
	CanonicalApply   string
	AllowedPaths     []string
}

type ComponentKnowledge struct {
	ImportRule      string
	FragmentRule    string
	ImportExample   string
	FragmentExample string
	AllowedRootKeys []string
}

type VarsKnowledge struct {
	Path        string
	Summary     string
	PreferFor   []string
	AvoidFor    []string
	ExampleKeys []string
}

type PolicyKnowledge struct {
	AssumeOfflineByDefault bool
	PrepareArtifactKinds   []string
	ForbiddenApplyActions  []string
	VarsAdvisory           []string
	ComponentAdvisory      []string
}

type StepKnowledge struct {
	Kind                     string
	Category                 string
	Summary                  string
	WhenToUse                string
	SchemaFile               string
	AllowedRoles             []string
	Outputs                  []string
	Example                  string
	KeyFields                []askcontext.StepFieldContext
	ConstrainedLiteralFields []askcontext.ConstrainedFieldHint
}

type ConstraintKnowledge struct {
	StepKind      string
	Path          string
	AllowedValues []string
	Guidance      string
	SourceRef     string
}

var (
	bundleOnce sync.Once
	bundleData Bundle
)

func Current() Bundle {
	bundleOnce.Do(func() {
		bundleData = buildBundle()
	})
	return bundleData
}

func buildBundle() Bundle {
	manifest := askcontext.Current()
	bundle := Bundle{
		Workflow: WorkflowKnowledge{
			SupportedRoles:   append([]string(nil), manifest.Workflow.SupportedModes...),
			SupportedVersion: manifest.Workflow.SupportedVersion,
			TopLevelModes:    append([]string(nil), manifest.Workflow.TopLevelModes...),
			RequiredFields:   append([]string(nil), manifest.Workflow.RequiredFields...),
			Notes:            append([]string(nil), manifest.Workflow.Notes...),
			PhaseExample:     strings.TrimSpace(manifest.Workflow.PhaseExample),
			StepsExample:     strings.TrimSpace(manifest.Workflow.StepsExample),
		},
		Topology: TopologyKnowledge{
			WorkflowRoot:     manifest.Topology.WorkflowRoot,
			ScenarioDir:      manifest.Topology.ScenarioDir,
			ComponentDir:     manifest.Topology.ComponentDir,
			VarsPath:         manifest.Topology.VarsPath,
			CanonicalPrepare: manifest.Topology.CanonicalPrepare,
			CanonicalApply:   manifest.Topology.CanonicalApply,
			AllowedPaths:     append([]string(nil), manifest.Topology.AllowedPaths...),
		},
		Components: ComponentKnowledge{
			ImportRule:      manifest.Components.ImportRule,
			FragmentRule:    manifest.Components.FragmentRule,
			ImportExample:   strings.TrimSpace(manifest.Components.ImportExample),
			FragmentExample: strings.TrimSpace(manifest.Components.FragmentExample),
			AllowedRootKeys: []string{"steps"},
		},
		Vars: VarsKnowledge{
			Path:        manifest.Vars.Path,
			Summary:     manifest.Vars.Summary,
			PreferFor:   append([]string(nil), manifest.Vars.PreferFor...),
			AvoidFor:    append([]string(nil), manifest.Vars.AvoidFor...),
			ExampleKeys: append([]string(nil), manifest.Vars.ExampleKeys...),
		},
		Policy: PolicyKnowledge{
			AssumeOfflineByDefault: manifest.Policy.AssumeOfflineByDefault,
			PrepareArtifactKinds:   append([]string(nil), manifest.Policy.PrepareArtifactKinds...),
			ForbiddenApplyActions:  append([]string(nil), manifest.Policy.ForbiddenApplyActions...),
			VarsAdvisory:           append([]string(nil), manifest.Policy.VarsAdvisory...),
			ComponentAdvisory:      append([]string(nil), manifest.Policy.ComponentAdvisory...),
		},
	}
	defs := workflowcontract.StepDefinitions()
	index := make(map[string]workflowcontract.StepDefinition, len(defs))
	for _, def := range defs {
		index[def.Kind] = def
	}
	for _, step := range manifest.StepKinds {
		bundle.Steps = append(bundle.Steps, StepKnowledge{
			Kind:                     step.Kind,
			Category:                 step.Category,
			Summary:                  step.Summary,
			WhenToUse:                step.WhenToUse,
			SchemaFile:               step.SchemaFile,
			AllowedRoles:             append([]string(nil), step.AllowedRoles...),
			Outputs:                  append([]string(nil), step.Outputs...),
			Example:                  strings.TrimSpace(step.MinimalShape),
			KeyFields:                append([]askcontext.StepFieldContext(nil), step.KeyFields...),
			ConstrainedLiteralFields: append([]askcontext.ConstrainedFieldHint(nil), step.ConstrainedLiteralFields...),
		})
		for _, field := range step.ConstrainedLiteralFields {
			sourceRef := step.SchemaFile
			if def, ok := index[step.Kind]; ok && strings.TrimSpace(def.SchemaFile) != "" {
				sourceRef = def.SchemaFile
			}
			bundle.Constraints = append(bundle.Constraints, ConstraintKnowledge{
				StepKind:      step.Kind,
				Path:          field.Path,
				AllowedValues: append([]string(nil), field.AllowedValues...),
				Guidance:      field.Guidance,
				SourceRef:     sourceRef,
			})
		}
	}
	sort.Slice(bundle.Steps, func(i, j int) bool { return bundle.Steps[i].Kind < bundle.Steps[j].Kind })
	sort.Slice(bundle.Constraints, func(i, j int) bool {
		if bundle.Constraints[i].StepKind == bundle.Constraints[j].StepKind {
			return bundle.Constraints[i].Path < bundle.Constraints[j].Path
		}
		return bundle.Constraints[i].StepKind < bundle.Constraints[j].StepKind
	})
	return bundle
}

func (b Bundle) WorkflowPromptBlock() string {
	lines := []string{
		"Workflow source-of-truth:",
		fmt.Sprintf("- supported roles: %s", strings.Join(b.Workflow.SupportedRoles, ", ")),
		fmt.Sprintf("- supported workflow version: %s", b.Workflow.SupportedVersion),
		fmt.Sprintf("- top-level workflow modes: %s", strings.Join(b.Workflow.TopLevelModes, ", ")),
		fmt.Sprintf("- allowed generated paths: %s", strings.Join(b.Topology.AllowedPaths, ", ")),
	}
	if len(b.Workflow.RequiredFields) > 0 {
		lines = append(lines, fmt.Sprintf("- required workflow fields: %s", strings.Join(b.Workflow.RequiredFields, ", ")))
	}
	for _, note := range b.Workflow.Notes {
		lines = append(lines, "- "+strings.TrimSpace(note))
	}
	return strings.Join(lines, "\n")
}

func (b Bundle) PolicyPromptBlock() string {
	lines := []string{"Authoring policy from deck metadata:"}
	if b.Policy.AssumeOfflineByDefault {
		lines = append(lines, "- assume offline unless the request explicitly says online")
	}
	lines = append(lines,
		"- use prepare only when packages, images, binaries, archives, bundles, or repository mirrors must be staged",
		"- keep typed schema fields as native YAML values instead of stringifying arrays or objects",
		"- keep vars and components advisory unless the request or plan requires them",
	)
	for _, rule := range b.Policy.ForbiddenApplyActions {
		lines = append(lines, "- apply should avoid: "+strings.TrimSpace(rule))
	}
	return strings.Join(lines, "\n")
}

func (b Bundle) ComponentPromptBlock() string {
	lines := []string{
		"Component and import source-of-truth:",
		"- " + strings.TrimSpace(b.Components.ImportRule),
		"- " + strings.TrimSpace(b.Components.FragmentRule),
		fmt.Sprintf("- component fragment root keys: %s", strings.Join(b.Components.AllowedRootKeys, ", ")),
	}
	if b.Components.ImportExample != "" {
		lines = append(lines, "- import example:")
		for _, line := range strings.Split(b.Components.ImportExample, "\n") {
			lines = append(lines, "  "+line)
		}
	}
	if b.Components.FragmentExample != "" {
		lines = append(lines, "- component fragment example:")
		for _, line := range strings.Split(b.Components.FragmentExample, "\n") {
			lines = append(lines, "  "+line)
		}
	}
	return strings.Join(lines, "\n")
}

func (b Bundle) VarsPromptBlock() string {
	lines := []string{
		"Vars source-of-truth:",
		"- path: " + b.Vars.Path,
		"- " + strings.TrimSpace(b.Vars.Summary),
		"- prefer vars for: " + strings.Join(b.Vars.PreferFor, ", "),
		"- avoid vars for: " + strings.Join(b.Vars.AvoidFor, ", "),
	}
	if len(b.Vars.ExampleKeys) > 0 {
		lines = append(lines, "- example vars keys: "+strings.Join(b.Vars.ExampleKeys, ", "))
	}
	return strings.Join(lines, "\n")
}

func (b Bundle) ConstraintPromptBlock(stepKinds []string) string {
	allowed := map[string]bool{}
	for _, kind := range stepKinds {
		allowed[strings.TrimSpace(kind)] = true
	}
	lines := []string{"Schema-constrained literal fields:"}
	count := 0
	for _, item := range b.Constraints {
		if len(allowed) > 0 && !allowed[item.StepKind] {
			continue
		}
		line := fmt.Sprintf("- %s %s", item.StepKind, item.Path)
		if len(item.AllowedValues) > 0 {
			line += fmt.Sprintf(" allowed=%s", strings.Join(item.AllowedValues, ", "))
		}
		if strings.TrimSpace(item.Guidance) != "" {
			line += " guidance=" + strings.TrimSpace(item.Guidance)
		}
		lines = append(lines, line)
		count++
	}
	if count == 0 {
		return ""
	}
	return strings.Join(lines, "\n")
}
