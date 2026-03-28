package schemadoc

import (
	"maps"
	"sort"
	"strings"

	"github.com/Airgap-Castaways/deck/internal/stepmeta"
	"github.com/Airgap-Castaways/deck/internal/stepspec"
	"github.com/Airgap-Castaways/deck/internal/workflowcontract"
)

type FieldDoc struct {
	Description string
	Example     string
}

type ToolMetadata struct {
	Kind      string
	Category  string
	Summary   string
	WhenToUse string
	Example   string
	FieldDocs map[string]FieldDoc
	Notes     []string
}

type PageMetadata struct {
	Title     string
	Summary   string
	Example   string
	FieldDocs map[string]FieldDoc
	Notes     []string
}

// commonFieldDocs describes the structural and execution-control fields shared by every step document.
// These are merged into every tool's FieldDocs so non-action pages and any action-specific references
// to shared fields use the same wording.
var commonFieldDocs = map[string]FieldDoc{
	"apiVersion": {Description: "Optional step API version. When omitted, deck resolves it from the top-level workflow `version`. When set, it must be a supported deck step API version.", Example: "deck/v1alpha1"},
	"id":         {Description: "Unique identifier for the step within the workflow. Used in logs and plan output.", Example: "configure-containerd"},
	"kind":       {Description: "Concrete typed step kind. Together with the effective `apiVersion`, it determines which schema is applied to `spec`.", Example: "WriteFile"},
	"spec":       {Description: "Step-specific configuration payload. Shape depends on the chosen `kind`.", Example: "{...}"},
	"when":       {Description: workflowcontract.WhenDescription(), Example: workflowcontract.WhenExample()},
	"retry":      {Description: "Number of times to retry the step after a failure before marking it as failed.", Example: "3"},
	"timeout":    {Description: "Maximum duration allowed for the step before it is cancelled. Accepts Go duration strings.", Example: "5m"},
	"register":   {Description: workflowcontract.RegisterDescription(), Example: workflowcontract.RegisterExample()},
	"metadata":   {Description: "Optional free-form annotation map attached to the step for tooling or audit purposes.", Example: "{owner: platform-team}"},
}

func ToolMetaForDefinition(def workflowcontract.StepDefinition) ToolMetadata {
	if meta, ok := stepspec.LookupToolDoc(def.Kind); ok {
		return toolMetaFromStepspec(def, meta)
	}
	if entry, ok, err := stepmeta.LookupCatalogEntry(def.Kind); err == nil && ok {
		return toolMetaFromStepMeta(def, entry)
	}
	return ToolMetadata{Kind: def.Kind, Category: def.Category, Summary: def.Summary, WhenToUse: def.WhenToUse, FieldDocs: maps.Clone(commonFieldDocs)}
}

func toolMetaFromStepspec(def workflowcontract.StepDefinition, meta stepspec.ToolDocMetadata) ToolMetadata {
	tool := ToolMetadata{
		Kind:      def.Kind,
		Category:  def.Category,
		Summary:   def.Summary,
		WhenToUse: def.WhenToUse,
		Example:   normalizedGeneratedExample(strings.TrimSpace(meta.Example)),
		Notes:     append([]string(nil), meta.Notes...),
		FieldDocs: make(map[string]FieldDoc, len(commonFieldDocs)+len(meta.FieldDocs)),
	}
	maps.Copy(tool.FieldDocs, commonFieldDocs)
	for path, field := range meta.FieldDocs {
		tool.FieldDocs[path] = FieldDoc{Description: field.Description, Example: field.Example}
	}
	return tool
}

func toolMetaFromStepMeta(def workflowcontract.StepDefinition, entry stepmeta.Entry) ToolMetadata {
	projection := stepmeta.ProjectTool(entry, def.Category)
	meta := ToolMetadata{
		Kind:      projection.Kind,
		Category:  projection.Category,
		Summary:   firstNonEmpty(strings.TrimSpace(projection.Summary), def.Summary),
		WhenToUse: firstNonEmpty(strings.TrimSpace(projection.WhenToUse), def.WhenToUse),
		Example:   normalizedGeneratedExample(strings.TrimSpace(projection.Example)),
		Notes:     append([]string(nil), projection.Notes...),
		FieldDocs: make(map[string]FieldDoc, len(projection.FieldDocs)),
	}
	for path, field := range projection.FieldDocs {
		meta.FieldDocs[path] = FieldDoc{Description: field.Description, Example: field.Example}
	}
	merged := make(map[string]FieldDoc, len(commonFieldDocs)+len(meta.FieldDocs))
	maps.Copy(merged, commonFieldDocs)
	maps.Copy(merged, meta.FieldDocs)
	meta.FieldDocs = merged
	return meta
}

func normalizedGeneratedExample(example string) string {
	trimmed := strings.TrimSpace(example)
	if trimmed == "" {
		return ""
	}
	return trimmed + "\n"
}

func ToolKinds() []string {
	defs := workflowcontract.StepDefinitions()
	kinds := make([]string, 0, len(defs))
	for _, def := range defs {
		kinds = append(kinds, def.Kind)
	}
	sort.Strings(kinds)
	return kinds
}

// WorkflowMeta intentionally stays centralized because it documents the top-level
// workflow document contract rather than a typed step registered through stepmeta.
func WorkflowMeta() PageMetadata {
	return PageMetadata{
		Title:   "Workflow Schema",
		Summary: "Top-level workflow authoring reference for deck workflows.",
		Example: "version: v1alpha1\nsteps:\n  - id: write-config\n    apiVersion: deck/v1alpha1\n    kind: WriteFile\n    spec:\n      path: /etc/example.conf\n      content: hello\n",
		FieldDocs: map[string]FieldDoc{
			"phases":                  {Description: "Ordered execution phases. Each phase can contain imports, steps, or both.", Example: "[{name:install,steps:[...]}]"},
			"steps":                   {Description: "Flat step list for workflows that do not need named phases. Execution normalizes these steps into an implicit `default` phase.", Example: "[{id:configure-runtime,kind:WriteContainerdConfig,spec:{...}}]"},
			"steps[].kind":            {Description: "Typed step kind selected from the shipped public step inventory. Deck resolves step identity from effective `apiVersion` plus `kind`.", Example: "WriteFile"},
			"steps[].spec":            {Description: "Step payload validated against the schema for the resolved step identity.", Example: "{path:/etc/example.conf,content:hello}"},
			"steps[].when":            {Description: workflowcontract.WhenDescription(), Example: `vars.skipSetup != "true"`},
			"steps[].parallelGroup":   {Description: "Optional batch label. Consecutive steps with the same value in a phase may run in parallel and publish register outputs only after the whole batch succeeds.", Example: "downloads"},
			"steps[].retry":           {Description: "Number of times to retry the step after a failure before marking it as failed.", Example: "3"},
			"steps[].timeout":         {Description: "Maximum duration for the step. Accepts Go duration strings.", Example: "5m"},
			"steps[].register":        {Description: workflowcontract.RegisterDescription(), Example: workflowcontract.RegisterExample()},
			"phases[].name":           {Description: "Stable phase name used for ordering, selective execution, and persisted resume boundaries.", Example: "install"},
			"phases[].maxParallelism": {Description: "Optional cap for concurrently running steps inside a parallel batch in this phase.", Example: "2"},
			"phases[].imports":        {Description: "Component fragment imports that expand into this phase before step execution.", Example: "[{path:k8s/containerd-kubelet.yaml}]"},
		},
		Notes: []string{
			"A workflow must define at least one of `phases` or `steps`.",
			"A workflow cannot define both top-level `phases` and top-level `steps` at the same time.",
			"Top-level `steps` execute as an implicit phase named `default`.",
			"Imports are only supported under `phases[].imports` and resolve from `workflows/components/`.",
			"When a step omits `apiVersion`, deck resolves it from the top-level workflow `version` before schema and role checks run.",
			"Workflow mode is determined by command context or file location, not by an in-file `role` field.",
			"Each step still validates against its own kind-specific schema after the top-level workflow schema passes.",
		},
	}
}

// ToolDefinitionMeta intentionally stays centralized because it documents the
// tool-definition manifest format, not a workflow step kind.
func ToolDefinitionMeta() PageMetadata {
	return PageMetadata{
		Title:   "Tool Definition Schema",
		Summary: "Reference for tool definition manifests used to describe typed workflow tools.",
		Example: "apiVersion: deck/v1\nkind: ToolDefinition\nmetadata:\n  name: File\n  description: Manage files on the local node\nspec:\n  version: v1\n  summary: Manage files on the local node\n  category: shared\n  offlineAllowed: true\n  requires:\n    root: true\n  inputSchema: {}\n  idempotency:\n    mode: by-output\n    keys: [path]\n",
		FieldDocs: map[string]FieldDoc{
			"metadata.name":                  {Description: "Public tool kind name described by this manifest.", Example: "WriteFile"},
			"spec.version":                   {Description: "Manifest version for the tool definition contract.", Example: "v1"},
			"spec.summary":                   {Description: "Short product-facing summary of the tool's purpose.", Example: "Manage files on the local node."},
			"spec.category":                  {Description: "High-level grouping used for docs and discovery.", Example: "shared"},
			"spec.inputSchema":               {Description: "Schema fragment describing the tool's input contract.", Example: "{}"},
			"spec.outputContract":            {Description: "Declared outputs or state keys produced by the tool.", Example: "{artifacts:[joinFile],stateKeys:[nodeReady]}"},
			"spec.idempotency.mode":          {Description: "How callers should reason about repeated runs of the tool.", Example: "by-output"},
			"spec.requires.containerRuntime": {Description: "Whether the tool expects a container runtime on the node.", Example: "required"},
		},
		Notes: []string{
			"`ToolDefinition` describes a typed tool contract rather than a workflow step instance.",
			"`inputSchema` stays open-ended because it embeds another schema fragment rather than a fixed workflow payload.",
		},
	}
}

// ComponentFragmentMeta intentionally stays centralized because component
// fragments are top-level authoring documents, not typed step kinds.
func ComponentFragmentMeta() PageMetadata {
	return PageMetadata{
		Title:   "Component Fragment Schema",
		Summary: "Reference for reusable workflow component fragments located under `workflows/components/`.",
		Example: "steps:\n  - id: write-config\n    kind: WriteFile\n    spec:\n      path: /etc/example.conf\n      content: hello\n  - id: restart-service\n    kind: ManageService\n    spec:\n      name: example\n      state: restarted\n",
		FieldDocs: map[string]FieldDoc{
			"steps": {Description: "Ordered list of workflow steps contained in this fragment.", Example: "[{id:example,kind:Command,spec:{...}}]"},
		},
		Notes: []string{
			"Component fragments are stored in the `workflows/components/` directory of your workspace.",
			"They contain only a `steps:` list and follow a restricted schema compared to full scenarios.",
			"Fragments are imported into a scenario phase using `phases[].imports`.",
			"For more details on where fragments fit in the project, see the [Workspace Layout](../../workspace-layout.md).",
		},
	}
}
