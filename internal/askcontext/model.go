package askcontext

type Manifest struct {
	CLI        CLIContext
	Topology   WorkspaceTopology
	Workflow   WorkflowRules
	Modes      []ModeGuidance
	Components ComponentGuidance
	Vars       VarsGuidance
	StepKinds  []StepKindContext
}

type Topic string

const (
	TopicWorkflowInvariants   Topic = "workflow-invariants"
	TopicPolicy               Topic = "policy"
	TopicWorkspaceTopology    Topic = "workspace-topology"
	TopicPrepareApplyGuidance Topic = "prepare-apply-guidance"
	TopicComponentsImports    Topic = "components-imports"
	TopicVarsGuidance         Topic = "vars-guidance"
	TopicTypedSteps           Topic = "typed-steps"
	TopicCLIHints             Topic = "cli-hints"
	TopicProjectPhilosophy    Topic = "project-philosophy"
)

type PromptBlock struct {
	Topic   Topic
	Title   string
	Content string
}

type CLIContext struct {
	Command             string
	PlanSubcommand      string
	ConfigSubcommand    string
	TopLevelDescription string
	ImportantFlags      []CLIFlag
	Examples            []string
}

type CLIFlag struct {
	Name        string
	Description string
}

type WorkspaceTopology struct {
	WorkflowRoot      string
	ScenarioDir       string
	ComponentDir      string
	VarsPath          string
	AllowedPaths      []string
	CanonicalPrepare  string
	CanonicalApply    string
	GeneratedPathNote string
}

type WorkflowRules struct {
	Summary          string
	TopLevelModes    []string
	SupportedModes   []string
	SupportedVersion string
	ImportRule       string
	Notes            []string
}

type ModeGuidance struct {
	Mode        string
	Summary     string
	WhenToUse   string
	Prefer      []string
	Avoid       []string
	OutputFiles []string
}

type ComponentGuidance struct {
	Summary      string
	ImportRule   string
	ReuseRule    string
	LocationRule string
}

type VarsGuidance struct {
	Path        string
	Summary     string
	PreferFor   []string
	AvoidFor    []string
	ExampleKeys []string
}

type StepKindContext struct {
	Kind         string
	Category     string
	Summary      string
	WhenToUse    string
	SchemaFile   string
	AllowedRoles []string
	Actions      []string
	Outputs      []string
	MinimalShape string
	CuratedShape string
	KeyFields    []StepFieldContext
	ActionGuides []StepActionContext
	Notes        []string
}

type StepFieldContext struct {
	Path        string
	Description string
	Example     string
}

type StepActionContext struct {
	Action  string
	Note    string
	Example string
}
